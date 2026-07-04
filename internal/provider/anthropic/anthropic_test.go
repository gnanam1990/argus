package anthropic_test

import (
	"context"
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
