package anthropic_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/anthropics/anthropic-sdk-go/option"

	anthropicprov "github.com/gnanam1990/argus/internal/provider/anthropic"
	"github.com/gnanam1990/argus/pkg/action"
	"github.com/gnanam1990/argus/pkg/model"
)

const toolUseResponse = `{
  "id": "msg_1", "type": "message", "role": "assistant", "model": "claude-opus-4-8",
  "content": [
    {"type": "text", "text": "clicking the submit button"},
    {"type": "tool_use", "id": "toolu_1", "name": "computer", "input": {"action": "left_click", "coordinate": [10, 20]}}
  ],
  "stop_reason": "tool_use",
  "usage": {"input_tokens": 100, "output_tokens": 20}
}`

const endTurnResponse = `{
  "id": "msg_2", "type": "message", "role": "assistant", "model": "claude-opus-4-8",
  "content": [{"type": "text", "text": "the task is complete"}],
  "stop_reason": "end_turn",
  "usage": {"input_tokens": 50, "output_tokens": 8}
}`

// captureServer returns responses in sequence and records each request body +
// beta header.
func captureServer(t *testing.T, responses ...string) (*httptest.Server, *[]string, *[]string) {
	t.Helper()
	var bodies, betas []string
	i := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		bodies = append(bodies, string(b))
		betas = append(betas, r.Header.Get("anthropic-beta"))
		w.Header().Set("Content-Type", "application/json")
		resp := responses[len(responses)-1]
		if i < len(responses) {
			resp = responses[i]
		}
		i++
		_, _ = io.WriteString(w, resp)
	}))
	t.Cleanup(srv.Close)
	return srv, &bodies, &betas
}

func newProvider(t *testing.T, srv *httptest.Server) *anthropicprov.Provider {
	t.Helper()
	return anthropicprov.New(
		anthropicprov.WithClientOptions(option.WithBaseURL(srv.URL), option.WithAPIKey("test-key")),
		anthropicprov.WithDisplaySize(1024, 768),
	)
}

func TestStepDecodesToolUse(t *testing.T) {
	t.Parallel()
	srv, bodies, betas := captureServer(t, toolUseResponse)
	p := newProvider(t, srv)

	conv := &model.Conversation{System: "you are a test agent"}
	conv.AddUser(model.Text("submit the form"))
	conv.AddUser(model.ImageContent(action.Image{MIME: action.MIMEPNG, Data: []byte{1, 2, 3}}))

	turn, err := p.Step(context.Background(), conv)
	if err != nil {
		t.Fatalf("Step: %v", err)
	}

	if turn.Stop != model.StopAction {
		t.Errorf("Stop = %s, want action", turn.Stop)
	}
	if turn.Text() != "clicking the submit button" {
		t.Errorf("Text = %q", turn.Text())
	}
	uses := turn.ActionUses()
	if len(uses) != 1 {
		t.Fatalf("ActionUses = %d, want 1", len(uses))
	}
	if uses[0].CallID != "toolu_1" {
		t.Errorf("CallID = %q, want toolu_1", uses[0].CallID)
	}
	a := uses[0].Action
	if a.Type != action.Click || a.Point != (action.Point{X: 10, Y: 20}) {
		t.Errorf("action = %+v, want click at (10,20)", a)
	}
	if turn.Usage.InputTokens != 100 || turn.Usage.OutputTokens != 20 {
		t.Errorf("usage = %+v", turn.Usage)
	}

	// Request carried the beta header, the computer tool, the system prompt,
	// and the base64 image.
	if !strings.Contains((*betas)[0], "computer-use-2025-11-24") {
		t.Errorf("beta header = %q", (*betas)[0])
	}
	body := (*bodies)[0]
	for _, want := range []string{"computer_20251124", "you are a test agent", "base64", "AQID"} {
		if !strings.Contains(body, want) {
			t.Errorf("request body missing %q\n%s", want, body)
		}
	}
}

func TestStepMultiTurnHistory(t *testing.T) {
	t.Parallel()
	srv, bodies, _ := captureServer(t, toolUseResponse, endTurnResponse)
	p := newProvider(t, srv)

	conv := &model.Conversation{}
	conv.AddUser(model.Text("task"))

	// Turn 1 → tool_use.
	turn1, err := p.Step(context.Background(), conv)
	if err != nil {
		t.Fatalf("Step 1: %v", err)
	}
	// Simulate the loop: append the assistant turn + a tool result + a new
	// observation, then step again.
	conv.Add(turn1.Message)
	conv.AddTool(model.ActionResult(turn1.ActionUses()[0].CallID, action.Result{Output: "done"}))
	conv.AddUser(model.ImageContent(action.Image{MIME: action.MIMEPNG, Data: []byte{9}}))

	turn2, err := p.Step(context.Background(), conv)
	if err != nil {
		t.Fatalf("Step 2: %v", err)
	}
	if turn2.Stop != model.StopEnd {
		t.Errorf("turn2 Stop = %s, want end", turn2.Stop)
	}
	if turn2.HasActions() {
		t.Error("end turn should have no actions")
	}

	// The second request must include the tool_result referencing toolu_1 and
	// must not duplicate the assistant turn (encoded once, natively).
	body := (*bodies)[1]
	if !strings.Contains(body, "toolu_1") {
		t.Errorf("second request missing tool_result for toolu_1:\n%s", body)
	}
	// Exactly one assistant tool_use block (not re-encoded from the neutral
	// conversation); the substring guards against matching "tool_use_id".
	if n := strings.Count(body, `"type":"tool_use"`); n != 1 {
		t.Errorf("assistant tool_use should appear exactly once, got %d\n%s", n, body)
	}
}

func TestStepAPIError(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, `{"type":"error","error":{"type":"invalid_request_error","message":"bad"}}`)
	}))
	t.Cleanup(srv.Close)
	p := newProvider(t, srv)

	conv := &model.Conversation{}
	conv.AddUser(model.Text("task"))
	if _, err := p.Step(context.Background(), conv); err == nil {
		t.Error("expected an API error")
	} else if !strings.Contains(err.Error(), "anthropic") {
		t.Errorf("error not wrapped: %v", err)
	}
}

func TestCapabilities(t *testing.T) {
	t.Parallel()
	caps := anthropicprov.New().Capabilities()
	if !caps.NativeComputerUse || !caps.Vision {
		t.Errorf("caps = %+v, want native computer use + vision", caps)
	}
}

// toolUseResp builds a tool_use response with a distinct id, so a multi-step
// script can verify tool_use/tool_result pairing across turns.
func toolUseResp(id string) string {
	return `{
	  "id": "msg_` + id + `", "type": "message", "role": "assistant", "model": "claude-opus-4-8",
	  "content": [{"type": "tool_use", "id": "` + id + `", "name": "computer", "input": {"action": "left_click", "coordinate": [1, 1]}}],
	  "stop_reason": "tool_use",
	  "usage": {"input_tokens": 10, "output_tokens": 5}
	}`
}

// stepScreenshotScript drives 4 Steps, each adding one screenshot observation,
// wiring each turn's tool_use through a matching tool_result before the next
// step — mirroring how the agent loop actually drives a provider.
func stepScreenshotScript(t *testing.T, p *anthropicprov.Provider) {
	t.Helper()
	conv := &model.Conversation{}
	conv.AddUser(model.Text("start"))
	conv.AddUser(model.ImageContent(action.Image{MIME: action.MIMEPNG, Data: []byte{1}}))

	for i := 0; i < 4; i++ {
		turn, err := p.Step(context.Background(), conv)
		if err != nil {
			t.Fatalf("Step %d: %v", i+1, err)
		}
		conv.Add(turn.Message)
		conv.AddTool(model.ActionResult(turn.ActionUses()[0].CallID, action.Result{Output: "done"}))
		if i < 3 {
			conv.AddUser(model.ImageContent(action.Image{MIME: action.MIMEPNG, Data: []byte{byte(i + 2)}}))
		}
	}
}

// anthropicReqBlock is the subset of a decoded content block this file's
// retention tests need: enough to count live/pruned image parts and to
// verify tool_use/tool_result pairing, without needing the full SDK shape.
type anthropicReqBlock struct {
	Type      string `json:"type"`
	Text      string `json:"text,omitempty"`
	ID        string `json:"id,omitempty"`
	ToolUseID string `json:"tool_use_id,omitempty"`
}

type anthropicReqMessage struct {
	Role    string              `json:"role"`
	Content []anthropicReqBlock `json:"content"`
}

type anthropicReqBody struct {
	Messages []anthropicReqMessage `json:"messages"`
}

func decodeAnthropicRequest(t *testing.T, body string) anthropicReqBody {
	t.Helper()
	var req anthropicReqBody
	if err := json.Unmarshal([]byte(body), &req); err != nil {
		t.Fatalf("decode request body: %v\n%s", err, body)
	}
	return req
}

// countImageBlocks returns the number of live "image" blocks and the number
// of text blocks carrying the pruned-screenshot placeholder.
func countImageBlocks(req anthropicReqBody) (live, pruned int) {
	for _, m := range req.Messages {
		for _, c := range m.Content {
			switch {
			case c.Type == "image":
				live++
			case c.Type == "text" && c.Text == wantPrunedText:
				pruned++
			}
		}
	}
	return live, pruned
}

// wantPrunedText mirrors the unexported placeholder text the production code
// emits. It is duplicated here (rather than exported) because this is an
// external (_test) package, and the exact wire text is itself part of the
// adapter's contract under test.
const wantPrunedText = "[screenshot pruned]"

func TestImageRetentionPrunesOldScreenshots(t *testing.T) {
	t.Parallel()
	srv, bodies, _ := captureServer(t, toolUseResp("toolu_1"), toolUseResp("toolu_2"), toolUseResp("toolu_3"), toolUseResp("toolu_4"))
	p := anthropicprov.New(
		anthropicprov.WithClientOptions(option.WithBaseURL(srv.URL), option.WithAPIKey("test-key")),
		anthropicprov.WithImageRetention(2),
	)
	stepScreenshotScript(t, p)

	if len(*bodies) != 4 {
		t.Fatalf("requests = %d, want 4", len(*bodies))
	}
	req := decodeAnthropicRequest(t, (*bodies)[3])

	live, pruned := countImageBlocks(req)
	if live != 2 {
		t.Errorf("live images in 4th request = %d, want 2", live)
	}
	if pruned != 2 {
		t.Errorf("pruned placeholders in 4th request = %d, want 2", pruned)
	}
}

// TestImageRetentionPairingIntact re-runs the same 4-step/retention=2 script
// and decodes the 4th request's JSON structure (not substrings) to confirm
// pruning never disturbed tool_use/tool_result id pairing or dropped a block:
// every tool_use in the request has a matching tool_result and vice versa.
func TestImageRetentionPairingIntact(t *testing.T) {
	t.Parallel()
	srv, bodies, _ := captureServer(t, toolUseResp("toolu_1"), toolUseResp("toolu_2"), toolUseResp("toolu_3"), toolUseResp("toolu_4"))
	p := anthropicprov.New(
		anthropicprov.WithClientOptions(option.WithBaseURL(srv.URL), option.WithAPIKey("test-key")),
		anthropicprov.WithImageRetention(2),
	)
	stepScreenshotScript(t, p)

	req := decodeAnthropicRequest(t, (*bodies)[3])

	toolUse := map[string]bool{}
	toolResult := map[string]bool{}
	for _, m := range req.Messages {
		for _, c := range m.Content {
			switch c.Type {
			case "tool_use":
				toolUse[c.ID] = true
			case "tool_result":
				toolResult[c.ToolUseID] = true
			}
		}
	}
	if len(toolUse) == 0 {
		t.Fatal("expected at least one tool_use block in the 4th request")
	}
	for id := range toolUse {
		if !toolResult[id] {
			t.Errorf("tool_use %q has no matching tool_result in the decoded request", id)
		}
	}
	for id := range toolResult {
		if !toolUse[id] {
			t.Errorf("tool_result references unknown tool_use %q", id)
		}
	}
}

func TestImageRetentionZeroKeepsAll(t *testing.T) {
	t.Parallel()
	srv, bodies, _ := captureServer(t, toolUseResp("toolu_1"), toolUseResp("toolu_2"), toolUseResp("toolu_3"), toolUseResp("toolu_4"))
	p := anthropicprov.New(
		anthropicprov.WithClientOptions(option.WithBaseURL(srv.URL), option.WithAPIKey("test-key")),
		anthropicprov.WithImageRetention(0),
	)
	stepScreenshotScript(t, p)

	req := decodeAnthropicRequest(t, (*bodies)[3])
	live, pruned := countImageBlocks(req)
	if live != 4 {
		t.Errorf("live images in 4th request = %d, want 4 (retention disabled)", live)
	}
	if pruned != 0 {
		t.Errorf("pruned placeholders in 4th request = %d, want 0", pruned)
	}
}
