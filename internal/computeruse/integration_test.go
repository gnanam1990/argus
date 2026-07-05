// This file exercises the real capture, approval, grounding, instructions,
// actor, and mcp packages wired together, with fakes only at the seams that
// touch the OS: permission/lock detection, bringing an app to the
// foreground, and screenshot/accessibility capture. It is the same wiring
// internal/app.BuildComputerUse assembles for production; see that file for
// the authoritative shape this test proves end to end.
package computeruse_test

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/gnanam1990/argus/internal/computeruse/actor"
	"github.com/gnanam1990/argus/internal/computeruse/approval"
	"github.com/gnanam1990/argus/internal/computeruse/capture"
	"github.com/gnanam1990/argus/internal/computeruse/grounding"
	"github.com/gnanam1990/argus/internal/computeruse/instructions"
	"github.com/gnanam1990/argus/internal/computeruse/mcp"
	"github.com/gnanam1990/argus/internal/computeruse/permissions"
	"github.com/gnanam1990/argus/internal/computeruse/state"
	"github.com/gnanam1990/argus/pkg/action"
	computerfake "github.com/gnanam1990/argus/pkg/computer/fake"
	"github.com/gnanam1990/argus/pkg/grounder"
)

const (
	approvedBundle = "com.example.approved"
	deniedBundle   = "com.example.denied"

	// interactableIndex is the stable index the grounding layer assigns to
	// the one interactable element the fake accessibility tree reports (see
	// fakeTreeSource): grounding.DefaultProvider.FrontmostTree numbers
	// surviving elements 1, 2, ... in report order, so the button that comes
	// first gets index 1 (internal/computeruse/grounding/grounding.go).
	interactableIndex = 1

	// wantCenterX/Y is the center of that element's Box, (100,200)-(200,240),
	// which resolveXY (internal/computeruse/mcp/tools.go) must resolve
	// element_index 1 to before issuing the click.
	wantCenterX = 150
	wantCenterY = 220

	// errNotFresh must match the exact ordering-error string
	// internal/computeruse/mcp/server.go's requireReady returns when no
	// fresh get_app_state observation is cached for the app.
	errNotFresh = "You must call get_app_state to get the latest state before doing other Computer Use actions."
)

// fakePermOrch is a hermetic permissions.Orchestrator: the screen is always
// unlocked and Ensure always succeeds, so approval is the only thing left to
// gate a capture or action on.
type fakePermOrch struct{}

func (fakePermOrch) Ensure(context.Context) error           { return nil }
func (fakePermOrch) IsLocked(context.Context) (bool, error) { return false, nil }

var _ permissions.Orchestrator = fakePermOrch{}

// fakeFocuser records every bundle it is asked to bring to the foreground,
// without touching any real window server.
type fakeFocuser struct {
	mu      sync.Mutex
	focused []string
}

func (f *fakeFocuser) Focus(_ context.Context, bundleID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.focused = append(f.focused, bundleID)
	return nil
}

func (f *fakeFocuser) Focused() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, len(f.focused))
	copy(out, f.focused)
	return out
}

var _ capture.Focuser = (*fakeFocuser)(nil)

// fakeAppLister is a hermetic capture.AppLister; this test never calls
// list_apps, so it has nothing to report.
type fakeAppLister struct{}

func (fakeAppLister) ListApps(context.Context) ([]state.AppInfo, error) { return nil, nil }

var _ capture.AppLister = fakeAppLister{}

// fakeScreenshotter is a hermetic capture.Screenshotter returning a canned
// image.
type fakeScreenshotter struct{ img action.Image }

func (f fakeScreenshotter) Screenshot(context.Context) (action.Image, error) { return f.img, nil }

var _ capture.Screenshotter = fakeScreenshotter{}

// tinyPNG encodes a real, decodable 4x4 PNG so anything that decodes the
// image sees valid data rather than a hand-rolled byte fixture.
func tinyPNG(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 4, 4))); err != nil {
		t.Fatalf("encode tiny PNG: %v", err)
	}
	return buf.Bytes()
}

// fakeTreeSource is an ax.TreeSource reporting two elements: an interactable
// button with a known Box (so the test can assert the exact coordinates
// get_app_state -> click resolves element_index 1 to) and a non-interactable
// label. It ignores the image it's handed, the same as any tree source that
// doesn't need to relate its own coordinate space to screenshot pixels.
func fakeTreeSource(context.Context, action.Image) ([]grounder.Element, error) {
	return []grounder.Element{
		{
			ID:           1,
			Box:          action.Rect{Min: action.Point{X: 100, Y: 200}, Max: action.Point{X: 200, Y: 240}},
			Label:        "OK",
			Interactable: true,
		},
		{
			ID:    2,
			Box:   action.Rect{Min: action.Point{X: 0, Y: 0}, Max: action.Point{X: 50, Y: 20}},
			Label: "Static text",
		},
	}, nil
}

// send drives a single JSON-RPC request through srv.Serve and returns the
// decoded response. Each call is its own Serve invocation over a one-line
// reader, mirroring one turn of a real stdio MCP conversation; the server's
// session cache (used to enforce the get_app_state -> action freshness
// ordering) lives on srv itself and persists across calls exactly as it
// would across turns of a real conversation.
func send(t *testing.T, srv *mcp.Server, id int, method string, params any) map[string]any {
	t.Helper()
	reqObj := map[string]any{"jsonrpc": "2.0", "id": id, "method": method}
	if params != nil {
		reqObj["params"] = params
	}
	line, err := json.Marshal(reqObj)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}

	in := bytes.NewBuffer(append(line, '\n'))
	var out bytes.Buffer
	if err := srv.Serve(context.Background(), in, &out); err != nil {
		t.Fatalf("Serve(%s): %v", method, err)
	}

	var resp map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(out.Bytes()), &resp); err != nil {
		t.Fatalf("decode response for %s: %v (raw: %s)", method, err, out.String())
	}
	return resp
}

// toolCall builds the params object for a tools/call request.
func toolCall(name string, args map[string]any) map[string]any {
	return map[string]any{"name": name, "arguments": args}
}

// toolResult extracts (isError, text) from a tools/call response, failing
// the test if the response carries a JSON-RPC protocol error instead of an
// in-band tool result.
func toolResult(t *testing.T, resp map[string]any) (bool, string) {
	t.Helper()
	if e, ok := resp["error"]; ok && e != nil {
		t.Fatalf("unexpected protocol error: %v", e)
	}
	result, ok := resp["result"].(map[string]any)
	if !ok {
		t.Fatalf("result is not an object: %#v", resp["result"])
	}
	isError, _ := result["isError"].(bool)
	content, ok := result["content"].([]any)
	if !ok || len(content) == 0 {
		t.Fatalf("no content in result: %#v", result)
	}
	first, ok := content[0].(map[string]any)
	if !ok {
		t.Fatalf("content[0] is not an object: %#v", content[0])
	}
	text, _ := first["text"].(string)
	return isError, text
}

// TestEndToEndAppAwareComputerUse wires the real capture, approval,
// grounding, instructions, actor, and mcp packages together (fakes only at
// the OS-touching seams) and drives a scripted MCP conversation through the
// real Server.Serve. It proves the whole app-aware pipeline end to end: an
// approved app's accessibility tree is observed and indexed, a click on an
// element index resolves to that element's box center and reaches the real
// driver, an action without a fresh observation is rejected, and an
// unapproved app is refused before any OS side effect happens.
func TestEndToEndAppAwareComputerUse(t *testing.T) {
	t.Parallel()

	// Real approval store, one app pre-approved; everything else stays
	// Pending (deny-by-default — see internal/computeruse/approval's
	// package doc).
	storePath := filepath.Join(t.TempDir(), "cu-approvals.json")
	store := approval.NewFileStore(storePath)
	if err := store.Set(context.Background(), approvedBundle, approval.Approved); err != nil {
		t.Fatalf("pre-approve %s: %v", approvedBundle, err)
	}

	// Real capture.DefaultWorker over fakes at the OS seams.
	orch := fakePermOrch{}
	focuser := &fakeFocuser{}
	provider := grounding.New(fakeTreeSource, func(context.Context) (action.Image, error) {
		return action.Image{MIME: action.MIMEPNG, Data: tinyPNG(t)}, nil
	})
	loader := instructions.NewChainLoader(os.ReadFile, os.UserConfigDir)
	shot := fakeScreenshotter{img: action.Image{MIME: action.MIMEPNG, Data: tinyPNG(t)}}

	// The frontmost-app guard is unit-tested in the capture package; keep this
	// end-to-end wiring test independent of the real desktop's frontmost app.
	worker := capture.NewDefaultWorker(orch, store, focuser, provider, loader, shot,
		capture.WithFrontmostFunc(func() string { return "" }))
	sp := capture.NewProvider(worker, fakeAppLister{})

	// Real actor over a recording fake driver.
	driver := computerfake.New()
	act := actor.New(driver)

	// Real MCP server over all of the above.
	srv := mcp.New(sp, act, orch, store)

	id := 0
	nextID := func() int { id++; return id }

	t.Run("initialize", func(t *testing.T) {
		resp := send(t, srv, nextID(), "initialize", nil)
		result, ok := resp["result"].(map[string]any)
		if !ok {
			t.Fatalf("result is not an object: %#v", resp["result"])
		}
		if result["protocolVersion"] == nil {
			t.Errorf("initialize result missing protocolVersion: %#v", result)
		}
	})

	t.Run("tools_list_has_all_eight", func(t *testing.T) {
		resp := send(t, srv, nextID(), "tools/list", nil)
		result, ok := resp["result"].(map[string]any)
		if !ok {
			t.Fatalf("result is not an object: %#v", resp["result"])
		}
		tools, ok := result["tools"].([]any)
		if !ok {
			t.Fatalf("tools is not an array: %#v", result["tools"])
		}
		want := []string{
			"get_app_state", "list_apps", "click", "type_text",
			"press_key", "scroll", "drag", "perform_secondary_action",
		}
		got := map[string]bool{}
		for _, tl := range tools {
			m, ok := tl.(map[string]any)
			if !ok {
				t.Fatalf("tool entry is not an object: %#v", tl)
			}
			name, _ := m["name"].(string)
			got[name] = true
		}
		if len(tools) != len(want) {
			t.Errorf("got %d tools, want %d: %v", len(tools), len(want), got)
		}
		for _, w := range want {
			if !got[w] {
				t.Errorf("missing tool %q", w)
			}
		}
	})

	t.Run("get_app_state_returns_indexed_elements", func(t *testing.T) {
		resp := send(t, srv, nextID(), "tools/call", toolCall("get_app_state", map[string]any{
			"bundle_identifier": approvedBundle,
		}))
		isError, text := toolResult(t, resp)
		if isError {
			t.Fatalf("get_app_state failed: %s", text)
		}
		if !strings.Contains(text, `"index":1`) || !strings.Contains(text, `"label":"OK"`) {
			t.Errorf("get_app_state body missing the interactable element: %s", text)
		}
		if !strings.Contains(text, `"index":2`) {
			t.Errorf("get_app_state body missing the second element: %s", text)
		}
		if !strings.Contains(text, `"bundle_identifier":"`+approvedBundle+`"`) {
			t.Errorf("get_app_state body missing bundle identifier: %s", text)
		}
	})

	t.Run("click_resolves_element_index_to_box_center", func(t *testing.T) {
		resp := send(t, srv, nextID(), "tools/call", toolCall("click", map[string]any{
			"bundle_identifier": approvedBundle,
			"element_index":     interactableIndex,
		}))
		isError, text := toolResult(t, resp)
		if isError {
			t.Fatalf("click failed: %s", text)
		}
		calls := driver.Calls()
		if len(calls) != 1 {
			t.Fatalf("driver calls = %+v, want exactly 1", calls)
		}
		got := calls[0]
		if got.Method != "Click" {
			t.Fatalf("call = %+v, want a Click", got)
		}
		if got.X != wantCenterX || got.Y != wantCenterY {
			t.Errorf("click coords = (%d,%d), want (%d,%d) — the element's box center", got.X, got.Y, wantCenterX, wantCenterY)
		}
		if got.Button != action.Left {
			t.Errorf("button = %v, want left (default)", got.Button)
		}
		if focused := focuser.Focused(); len(focused) != 1 || focused[0] != approvedBundle {
			t.Errorf("focuser calls = %v, want exactly one Focus(%q)", focused, approvedBundle)
		}
	})

	t.Run("second_click_without_fresh_state_is_ordering_error", func(t *testing.T) {
		resp := send(t, srv, nextID(), "tools/call", toolCall("click", map[string]any{
			"bundle_identifier": approvedBundle,
			"element_index":     interactableIndex,
		}))
		isError, text := toolResult(t, resp)
		if !isError {
			t.Fatalf("expected an ordering error, got success: %s", text)
		}
		if text != errNotFresh {
			t.Errorf("text = %q, want %q", text, errNotFresh)
		}
		if len(driver.Calls()) != 1 {
			t.Errorf("driver should not have been called again, calls = %+v", driver.Calls())
		}
	})

	t.Run("get_app_state_on_unapproved_app_is_denied", func(t *testing.T) {
		resp := send(t, srv, nextID(), "tools/call", toolCall("get_app_state", map[string]any{
			"bundle_identifier": deniedBundle,
		}))
		isError, text := toolResult(t, resp)
		if !isError {
			t.Fatalf("expected get_app_state on an unapproved app to fail, got success: %s", text)
		}
		if !strings.Contains(text, "not allowed to use the app") {
			t.Errorf("text = %q, want it to mention the app is not allowed", text)
		}
		for _, b := range focuser.Focused() {
			if b == deniedBundle {
				t.Errorf("the unapproved app was focused; approval must be checked before any OS side effect")
			}
		}
	})

	t.Run("click_on_unapproved_app_is_rejected", func(t *testing.T) {
		resp := send(t, srv, nextID(), "tools/call", toolCall("click", map[string]any{
			"bundle_identifier": deniedBundle,
		}))
		isError, text := toolResult(t, resp)
		if !isError {
			t.Fatalf("expected click on an unapproved app to fail, got success: %s", text)
		}
		// get_app_state above never cached a fresh observation for this
		// bundle (it failed the approval check before producing one), so
		// the ordering guard rejects the click; the net effect is the same
		// as the "not allowed" case — the app was never usable.
		if text != errNotFresh {
			t.Errorf("text = %q, want %q", text, errNotFresh)
		}
		if len(driver.Calls()) != 1 {
			t.Errorf("driver should never have been called for the unapproved app, calls = %+v", driver.Calls())
		}
	})
}
