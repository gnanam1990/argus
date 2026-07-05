package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/gnanam1990/argus/internal/computeruse/actor"
	"github.com/gnanam1990/argus/internal/computeruse/approval"
	"github.com/gnanam1990/argus/internal/computeruse/permissions"
	"github.com/gnanam1990/argus/internal/computeruse/state"
)

// ---- fakes ----

// fakeStateProvider is a hermetic state.StateProvider.
type fakeStateProvider struct {
	mu       sync.Mutex
	states   map[string]state.AppState
	apps     []state.AppInfo
	stateErr error
	appsErr  error
	calls    []string
}

func newFakeStateProvider() *fakeStateProvider {
	return &fakeStateProvider{states: map[string]state.AppState{}}
}

func (f *fakeStateProvider) GetAppState(_ context.Context, bundleID string) (state.AppState, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, "GetAppState:"+bundleID)
	if f.stateErr != nil {
		return state.AppState{}, f.stateErr
	}
	return f.states[bundleID], nil
}

func (f *fakeStateProvider) ListApps(_ context.Context) ([]state.AppInfo, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, "ListApps")
	if f.appsErr != nil {
		return nil, f.appsErr
	}
	return f.apps, nil
}

// fakeActor is a hermetic actor.Actor that records every call.
type fakeActor struct {
	mu    sync.Mutex
	calls []any
	err   error
}

func (f *fakeActor) record(req any) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, req)
	return f.err
}

func (f *fakeActor) last() any {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.calls) == 0 {
		return nil
	}
	return f.calls[len(f.calls)-1]
}

func (f *fakeActor) Click(_ context.Context, req actor.ClickRequest) error { return f.record(req) }
func (f *fakeActor) TypeText(_ context.Context, req actor.TypeRequest) error {
	return f.record(req)
}
func (f *fakeActor) PressKey(_ context.Context, req actor.KeyRequest) error { return f.record(req) }
func (f *fakeActor) Scroll(_ context.Context, req actor.ScrollRequest) error {
	return f.record(req)
}
func (f *fakeActor) Drag(_ context.Context, req actor.DragRequest) error { return f.record(req) }
func (f *fakeActor) PerformSecondaryAction(_ context.Context, req actor.SecondaryActionRequest) error {
	return f.record(req)
}

var _ actor.Actor = (*fakeActor)(nil)

// fakeOrchestrator is a hermetic permissions.Orchestrator.
type fakeOrchestrator struct {
	ensureErr error
	locked    bool
	lockedErr error
}

func (f *fakeOrchestrator) Ensure(context.Context) error { return f.ensureErr }
func (f *fakeOrchestrator) IsLocked(context.Context) (bool, error) {
	return f.locked, f.lockedErr
}

var _ permissions.Orchestrator = (*fakeOrchestrator)(nil)

// memStore is a hermetic, in-memory approval.Store.
type memStore struct {
	mu      sync.Mutex
	records map[string]approval.Decision
}

func newMemStore() *memStore { return &memStore{records: map[string]approval.Decision{}} }

func (m *memStore) Get(_ context.Context, bundleID string) (approval.Decision, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if d, ok := m.records[bundleID]; ok {
		return d, nil
	}
	return approval.Pending, nil
}

func (m *memStore) Set(_ context.Context, bundleID string, d approval.Decision) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.records[bundleID] = d
	return nil
}

func (m *memStore) Remove(_ context.Context, bundleID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.records, bundleID)
	return nil
}

func (m *memStore) List(context.Context) ([]approval.Record, error) { return nil, nil }

var _ approval.Store = (*memStore)(nil)

// ---- test helpers ----

const testBundle = "com.example.app"

// newTestServer builds a Server wired to fresh fakes, with the given app
// pre-approved (unless approved is false, leaving it Pending) and the
// orchestrator reporting no preconditions problems.
func newTestServer(t *testing.T, approved bool) (*Server, *fakeStateProvider, *fakeActor, *fakeOrchestrator, *memStore) {
	t.Helper()
	sp := newFakeStateProvider()
	sp.states[testBundle] = state.AppState{
		BundleIdentifier: testBundle,
		WindowTitle:      "Test Window",
		Elements: []state.Element{
			{Index: 0, Role: "button", Label: "OK", Frame: state.Rect{X: 10, Y: 20, Width: 30, Height: 10}},
			{Index: 1, Role: "field", Label: "Name", Frame: state.Rect{X: 0, Y: 0, Width: 100, Height: 20}},
		},
	}
	act := &fakeActor{}
	orch := &fakeOrchestrator{}
	store := newMemStore()
	if approved {
		_ = store.Set(context.Background(), testBundle, approval.Approved)
	}
	s := New(sp, act, orch, store, WithInfo("argus-cu", "1.0"))
	return s, sp, act, orch, store
}

func req(t *testing.T, id, method string, params any) rpcRequest {
	t.Helper()
	r := rpcRequest{JSONRPC: "2.0", Method: method}
	if id != "" {
		r.ID = json.RawMessage(id)
	}
	if params != nil {
		b, err := json.Marshal(params)
		if err != nil {
			t.Fatal(err)
		}
		r.Params = b
	}
	return r
}

func call(name string, args map[string]any) map[string]any {
	return map[string]any{"name": name, "arguments": args}
}

func resultOf(t *testing.T, resp rpcResponse) map[string]any {
	t.Helper()
	if resp.Error != nil {
		t.Fatalf("unexpected protocol error: %+v", resp.Error)
	}
	m, ok := resp.Result.(map[string]any)
	if !ok {
		t.Fatalf("result is not a map: %#v", resp.Result)
	}
	return m
}

func contentText(t *testing.T, res map[string]any) string {
	t.Helper()
	content, ok := res["content"].([]map[string]any)
	if !ok || len(content) == 0 {
		t.Fatalf("no content in result: %#v", res)
	}
	text, _ := content[0]["text"].(string)
	return text
}

// ---- tests ----

func TestInitialize(t *testing.T) {
	t.Parallel()
	s, _, _, _, _ := newTestServer(t, true)
	resp := s.Handle(context.Background(), req(t, "1", "initialize", nil))
	res := resultOf(t, resp)
	if res["protocolVersion"] != protocolVersion {
		t.Errorf("protocolVersion = %v", res["protocolVersion"])
	}
	info := res["serverInfo"].(map[string]any)
	if info["name"] != "argus-cu" || info["version"] != "1.0" {
		t.Errorf("serverInfo = %v", info)
	}
}

func TestToolsListReturnsAllEight(t *testing.T) {
	t.Parallel()
	s, _, _, _, _ := newTestServer(t, true)
	resp := s.Handle(context.Background(), req(t, "1", "tools/list", nil))
	res := resultOf(t, resp)
	tools := res["tools"].([]map[string]any)
	if len(tools) != 8 {
		t.Fatalf("got %d tools, want 8", len(tools))
	}
	names := map[string]bool{}
	for _, tl := range tools {
		names[tl["name"].(string)] = true
	}
	want := []string{
		"get_app_state", "list_apps", "click", "type_text",
		"press_key", "scroll", "drag", "perform_secondary_action",
	}
	for _, w := range want {
		if !names[w] {
			t.Errorf("missing tool %q", w)
		}
	}
}

func TestGetAppStateReturnsTreeAndMarksFresh(t *testing.T) {
	t.Parallel()
	s, sp, _, _, _ := newTestServer(t, true)
	resp := s.Handle(context.Background(), req(t, "1", "tools/call", call("get_app_state", map[string]any{"bundle_identifier": testBundle})))
	res := resultOf(t, resp)
	if res["isError"] != false {
		t.Fatalf("isError = %v", res["isError"])
	}
	text := contentText(t, res)
	if !strings.Contains(text, "Test Window") || !strings.Contains(text, `"role":"button"`) {
		t.Errorf("get_app_state text missing expected fields: %s", text)
	}
	if len(sp.calls) != 1 || sp.calls[0] != "GetAppState:"+testBundle {
		t.Errorf("calls = %v", sp.calls)
	}
	if _, fresh := s.getFresh(testBundle); !fresh {
		t.Error("expected state to be marked fresh after get_app_state")
	}
}

func TestGetAppStateError(t *testing.T) {
	t.Parallel()
	s, sp, _, _, _ := newTestServer(t, true)
	sp.stateErr = errors.New("boom")
	resp := s.Handle(context.Background(), req(t, "1", "tools/call", call("get_app_state", map[string]any{"bundle_identifier": testBundle})))
	res := resultOf(t, resp)
	if res["isError"] != true {
		t.Fatalf("isError = %v, want true", res["isError"])
	}
	if !strings.Contains(contentText(t, res), "boom") {
		t.Errorf("expected error text to contain %q, got %q", "boom", contentText(t, res))
	}
}

func TestListApps(t *testing.T) {
	t.Parallel()
	s, sp, _, _, _ := newTestServer(t, true)
	sp.apps = []state.AppInfo{{BundleIdentifier: testBundle, Name: "Test App", IsRunning: true}}
	resp := s.Handle(context.Background(), req(t, "1", "tools/call", call("list_apps", nil)))
	res := resultOf(t, resp)
	if res["isError"] != false {
		t.Fatalf("isError = %v", res["isError"])
	}
	if !strings.Contains(contentText(t, res), "Test App") {
		t.Errorf("expected app name in result, got %s", contentText(t, res))
	}
}

func TestActionBeforeGetAppStateIsOrderingError(t *testing.T) {
	t.Parallel()
	s, _, act, _, _ := newTestServer(t, true)
	resp := s.Handle(context.Background(), req(t, "1", "tools/call", call("click", map[string]any{
		"bundle_identifier": testBundle, "x": 1, "y": 2,
	})))
	res := resultOf(t, resp)
	if res["isError"] != true {
		t.Fatalf("isError = %v, want true", res["isError"])
	}
	if contentText(t, res) != errNotFresh {
		t.Errorf("text = %q, want %q", contentText(t, res), errNotFresh)
	}
	if len(act.calls) != 0 {
		t.Errorf("actor should not have been called, calls = %v", act.calls)
	}
}

func TestActionOnUnapprovedAppReturnsApprovalError(t *testing.T) {
	t.Parallel()
	s, _, act, _, _ := newTestServer(t, false) // not approved
	// Observe first so freshness isn't the blocker under test.
	s.Handle(context.Background(), req(t, "1", "tools/call", call("get_app_state", map[string]any{"bundle_identifier": testBundle})))

	resp := s.Handle(context.Background(), req(t, "2", "tools/call", call("click", map[string]any{
		"bundle_identifier": testBundle, "x": 1, "y": 2,
	})))
	res := resultOf(t, resp)
	if res["isError"] != true {
		t.Fatalf("isError = %v, want true", res["isError"])
	}
	want := errNotApproved(testBundle)
	if contentText(t, res) != want {
		t.Errorf("text = %q, want %q", contentText(t, res), want)
	}
	if len(act.calls) != 0 {
		t.Errorf("actor should not have been called, calls = %v", act.calls)
	}
}

func TestActionWhileScreenLocked(t *testing.T) {
	t.Parallel()
	s, _, act, orch, _ := newTestServer(t, true)
	s.Handle(context.Background(), req(t, "1", "tools/call", call("get_app_state", map[string]any{"bundle_identifier": testBundle})))
	orch.ensureErr = permissions.ErrScreenLocked

	resp := s.Handle(context.Background(), req(t, "2", "tools/call", call("click", map[string]any{
		"bundle_identifier": testBundle, "x": 1, "y": 2,
	})))
	res := resultOf(t, resp)
	if res["isError"] != true {
		t.Fatalf("isError = %v, want true", res["isError"])
	}
	if contentText(t, res) != errLocked {
		t.Errorf("text = %q, want %q", contentText(t, res), errLocked)
	}
	if len(act.calls) != 0 {
		t.Errorf("actor should not have been called, calls = %v", act.calls)
	}
}

func TestActionWhilePermissionsPending(t *testing.T) {
	t.Parallel()
	s, _, act, orch, _ := newTestServer(t, true)
	s.Handle(context.Background(), req(t, "1", "tools/call", call("get_app_state", map[string]any{"bundle_identifier": testBundle})))
	orch.ensureErr = permissions.ErrPending

	resp := s.Handle(context.Background(), req(t, "2", "tools/call", call("click", map[string]any{
		"bundle_identifier": testBundle, "x": 1, "y": 2,
	})))
	res := resultOf(t, resp)
	if res["isError"] != true {
		t.Fatalf("isError = %v, want true", res["isError"])
	}
	if contentText(t, res) != errPending {
		t.Errorf("text = %q, want %q", contentText(t, res), errPending)
	}
	if len(act.calls) != 0 {
		t.Errorf("actor should not have been called, calls = %v", act.calls)
	}
}

func TestActionWhilePermissionsMissingReturnsRemediation(t *testing.T) {
	t.Parallel()
	s, _, _, orch, _ := newTestServer(t, true)
	s.Handle(context.Background(), req(t, "1", "tools/call", call("get_app_state", map[string]any{"bundle_identifier": testBundle})))
	remediation := errors.New("permissions: required macOS permission missing: Accessibility (System Settings > Privacy & Security > Accessibility)")
	orch.ensureErr = remediation

	resp := s.Handle(context.Background(), req(t, "2", "tools/call", call("click", map[string]any{
		"bundle_identifier": testBundle, "x": 1, "y": 2,
	})))
	res := resultOf(t, resp)
	if res["isError"] != true {
		t.Fatalf("isError = %v, want true", res["isError"])
	}
	if contentText(t, res) != remediation.Error() {
		t.Errorf("text = %q, want %q", contentText(t, res), remediation.Error())
	}
}

func TestClickResolvesElementIndexToCenter(t *testing.T) {
	t.Parallel()
	s, _, act, _, _ := newTestServer(t, true)
	s.Handle(context.Background(), req(t, "1", "tools/call", call("get_app_state", map[string]any{"bundle_identifier": testBundle})))

	resp := s.Handle(context.Background(), req(t, "2", "tools/call", call("click", map[string]any{
		"bundle_identifier": testBundle, "element_index": 0, "button": "right",
	})))
	res := resultOf(t, resp)
	if res["isError"] != false {
		t.Fatalf("isError = %v, resp content = %v", res["isError"], contentText(t, res))
	}
	last, ok := act.last().(actor.ClickRequest)
	if !ok {
		t.Fatalf("last call = %#v, want ClickRequest", act.last())
	}
	// Element 0's frame is {10,20,30,10} -> center (25, 25).
	if last.X != 25 || last.Y != 25 {
		t.Errorf("resolved coords = (%d,%d), want (25,25)", last.X, last.Y)
	}
	if last.Button != "right" {
		t.Errorf("button = %q, want right", last.Button)
	}
	if last.BundleIdentifier != testBundle {
		t.Errorf("bundle = %q", last.BundleIdentifier)
	}
}

func TestClickUsesXYWhenNoElementIndex(t *testing.T) {
	t.Parallel()
	s, _, act, _, _ := newTestServer(t, true)
	s.Handle(context.Background(), req(t, "1", "tools/call", call("get_app_state", map[string]any{"bundle_identifier": testBundle})))

	resp := s.Handle(context.Background(), req(t, "2", "tools/call", call("click", map[string]any{
		"bundle_identifier": testBundle, "x": 77, "y": 88,
	})))
	res := resultOf(t, resp)
	if res["isError"] != false {
		t.Fatalf("isError = %v", res["isError"])
	}
	last := act.last().(actor.ClickRequest)
	if last.X != 77 || last.Y != 88 {
		t.Errorf("coords = (%d,%d), want (77,88)", last.X, last.Y)
	}
}

func TestClickUnknownElementIndex(t *testing.T) {
	t.Parallel()
	s, _, act, _, _ := newTestServer(t, true)
	s.Handle(context.Background(), req(t, "1", "tools/call", call("get_app_state", map[string]any{"bundle_identifier": testBundle})))

	resp := s.Handle(context.Background(), req(t, "2", "tools/call", call("click", map[string]any{
		"bundle_identifier": testBundle, "element_index": 99,
	})))
	res := resultOf(t, resp)
	if res["isError"] != true {
		t.Fatalf("isError = %v, want true", res["isError"])
	}
	if len(act.calls) != 0 {
		t.Errorf("actor should not have been called, calls = %v", act.calls)
	}
}

func TestActionMarksStateStaleRequiringReObserve(t *testing.T) {
	t.Parallel()
	s, _, act, _, _ := newTestServer(t, true)
	s.Handle(context.Background(), req(t, "1", "tools/call", call("get_app_state", map[string]any{"bundle_identifier": testBundle})))

	resp1 := s.Handle(context.Background(), req(t, "2", "tools/call", call("click", map[string]any{
		"bundle_identifier": testBundle, "x": 1, "y": 2,
	})))
	if resultOf(t, resp1)["isError"] != false {
		t.Fatalf("first click should succeed: %v", resultOf(t, resp1))
	}
	if len(act.calls) != 1 {
		t.Fatalf("expected 1 actor call, got %d", len(act.calls))
	}

	// Second action without a fresh get_app_state must be rejected.
	resp2 := s.Handle(context.Background(), req(t, "3", "tools/call", call("click", map[string]any{
		"bundle_identifier": testBundle, "x": 3, "y": 4,
	})))
	res2 := resultOf(t, resp2)
	if res2["isError"] != true {
		t.Fatalf("isError = %v, want true", res2["isError"])
	}
	if contentText(t, res2) != errNotFresh {
		t.Errorf("text = %q, want %q", contentText(t, res2), errNotFresh)
	}
	if len(act.calls) != 1 {
		t.Errorf("actor should not have been called again, calls = %v", act.calls)
	}
}

func TestOtherActionToolsDispatchAfterGetAppState(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		tool string
		args map[string]any
	}{
		{"type_text", "type_text", map[string]any{"bundle_identifier": testBundle, "text": "hello"}},
		{"press_key", "press_key", map[string]any{"bundle_identifier": testBundle, "keys": []string{"cmd", "a"}}},
		{"scroll", "scroll", map[string]any{"bundle_identifier": testBundle, "direction": "down", "pages": 1}},
		{"drag", "drag", map[string]any{"bundle_identifier": testBundle, "from_x": 1, "from_y": 2, "to_x": 3, "to_y": 4}},
		{"perform_secondary_action", "perform_secondary_action", map[string]any{"bundle_identifier": testBundle, "x": 5, "y": 6}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			s, _, act, _, _ := newTestServer(t, true)
			s.Handle(context.Background(), req(t, "1", "tools/call", call("get_app_state", map[string]any{"bundle_identifier": testBundle})))
			resp := s.Handle(context.Background(), req(t, "2", "tools/call", call(c.tool, c.args)))
			res := resultOf(t, resp)
			if res["isError"] != false {
				t.Fatalf("%s: isError = %v, content = %s", c.tool, res["isError"], contentText(t, res))
			}
			if len(act.calls) != 1 {
				t.Fatalf("%s: expected 1 actor call, got %d", c.tool, len(act.calls))
			}
		})
	}
}

func TestActorErrorIsInBand(t *testing.T) {
	t.Parallel()
	s, _, act, _, _ := newTestServer(t, true)
	s.Handle(context.Background(), req(t, "1", "tools/call", call("get_app_state", map[string]any{"bundle_identifier": testBundle})))
	act.err = errors.New("driver failure")

	resp := s.Handle(context.Background(), req(t, "2", "tools/call", call("click", map[string]any{
		"bundle_identifier": testBundle, "x": 1, "y": 2,
	})))
	res := resultOf(t, resp)
	if res["isError"] != true {
		t.Fatalf("isError = %v, want true", res["isError"])
	}
	if !strings.Contains(contentText(t, res), "driver failure") {
		t.Errorf("text = %q, want it to contain driver failure", contentText(t, res))
	}
}

func TestUnknownTool(t *testing.T) {
	t.Parallel()
	s, _, _, _, _ := newTestServer(t, true)
	resp := s.Handle(context.Background(), req(t, "1", "tools/call", call("teleport", nil)))
	if resp.Error == nil || resp.Error.Code != codeInvalidParams {
		t.Errorf("expected invalid-params error, got %+v", resp.Error)
	}
}

func TestUnknownMethod(t *testing.T) {
	t.Parallel()
	s, _, _, _, _ := newTestServer(t, true)
	resp := s.Handle(context.Background(), req(t, "1", "no/such", nil))
	if resp.Error == nil || resp.Error.Code != codeMethodNotFound {
		t.Errorf("expected method-not-found, got %+v", resp.Error)
	}
}

func TestBadToolArguments(t *testing.T) {
	t.Parallel()
	s, _, _, _, _ := newTestServer(t, true)
	bad := rpcRequest{JSONRPC: "2.0", ID: json.RawMessage("1"), Method: "tools/call",
		Params: json.RawMessage(`{"name":"click","arguments":"not-an-object"}`)}
	resp := s.Handle(context.Background(), bad)
	if resp.Error == nil || resp.Error.Code != codeInvalidParams {
		t.Errorf("expected invalid-params, got %+v", resp.Error)
	}
}

// ---- end-to-end Serve tests over real JSON-RPC lines ----

func TestServeEndToEnd(t *testing.T) {
	t.Parallel()
	in := `{"jsonrpc":"2.0","id":1,"method":"initialize"}` + "\n" +
		`{"jsonrpc":"2.0","method":"notifications/initialized"}` + "\n" +
		`{"jsonrpc":"2.0","id":2,"method":"tools/list"}` + "\n"
	var out bytes.Buffer
	s, _, _, _, _ := newTestServer(t, true)
	if err := s.Serve(context.Background(), strings.NewReader(in), &out); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("got %d responses, want 2:\n%s", len(lines), out.String())
	}
	if !strings.Contains(lines[0], "protocolVersion") || !strings.Contains(lines[1], "tools") {
		t.Errorf("responses = %v", lines)
	}
}

func TestServeDrivesFullActionFlow(t *testing.T) {
	t.Parallel()
	getState := fmt.Sprintf(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"get_app_state","arguments":{"bundle_identifier":%q}}}`, testBundle)
	click := fmt.Sprintf(`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"click","arguments":{"bundle_identifier":%q,"element_index":0}}}`, testBundle)
	in := getState + "\n" + click + "\n"

	var out bytes.Buffer
	s, _, act, _, _ := newTestServer(t, true)
	if err := s.Serve(context.Background(), strings.NewReader(in), &out); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("got %d responses, want 2:\n%s", len(lines), out.String())
	}
	var resp2 map[string]any
	if err := json.Unmarshal([]byte(lines[1]), &resp2); err != nil {
		t.Fatal(err)
	}
	result := resp2["result"].(map[string]any)
	if result["isError"] != false {
		t.Fatalf("click over the wire failed: %v", result)
	}
	if len(act.calls) != 1 {
		t.Fatalf("expected 1 actor call, got %d", len(act.calls))
	}
	last := act.calls[0].(actor.ClickRequest)
	if last.X != 25 || last.Y != 25 {
		t.Errorf("resolved coords over the wire = (%d,%d), want (25,25)", last.X, last.Y)
	}
}

func TestServeParseError(t *testing.T) {
	t.Parallel()
	var out bytes.Buffer
	s, _, _, _, _ := newTestServer(t, true)
	if err := s.Serve(context.Background(), strings.NewReader("not json\n"), &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "parse error") {
		t.Errorf("expected parse error response, got %s", out.String())
	}
}

func TestServeCapsLineLength(t *testing.T) {
	t.Parallel()
	huge := bytes.Repeat([]byte("a"), maxLineBytes+1) // no newline: one oversized "line"
	var out bytes.Buffer
	s, _, _, _, _ := newTestServer(t, true)
	err := s.Serve(context.Background(), bytes.NewReader(huge), &out)
	if err == nil {
		t.Fatal("expected an error for a line exceeding the byte limit")
	}
	if !strings.Contains(err.Error(), "exceeds") {
		t.Errorf("error = %v, want a size-limit message", err)
	}
}
