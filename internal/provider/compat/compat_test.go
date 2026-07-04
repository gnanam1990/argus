package compat_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gnanam1990/argus/internal/provider/compat"
	"github.com/gnanam1990/argus/pkg/action"
	"github.com/gnanam1990/argus/pkg/model"
)

const toolCallResponse = `{
  "choices": [{
    "message": {
      "role": "assistant",
      "content": "clicking submit",
      "tool_calls": [{"id": "call_1", "type": "function", "function": {"name": "computer", "arguments": "{\"action\":\"click\",\"x\":10,\"y\":20}"}}]
    },
    "finish_reason": "tool_calls"
  }],
  "usage": {"prompt_tokens": 100, "completion_tokens": 20}
}`

const stopResponse = `{
  "choices": [{"message": {"role": "assistant", "content": "done"}, "finish_reason": "stop"}],
  "usage": {"prompt_tokens": 50, "completion_tokens": 5}
}`

func server(t *testing.T, responses ...string) (*httptest.Server, *[]string) {
	t.Helper()
	var bodies []string
	i := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		bodies = append(bodies, string(b))
		resp := responses[len(responses)-1]
		if i < len(responses) {
			resp = responses[i]
		}
		i++
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, resp)
	}))
	t.Cleanup(srv.Close)
	return srv, &bodies
}

func TestStepDecodesToolCall(t *testing.T) {
	t.Parallel()
	srv, bodies := server(t, toolCallResponse)
	p := compat.New(compat.WithBaseURL(srv.URL), compat.WithAPIKey("k"), compat.WithModel("gpt-4o"))

	conv := &model.Conversation{System: "be helpful"}
	conv.AddUser(model.Text("submit"))
	conv.AddUser(model.ImageContent(action.Image{MIME: action.MIMEPNG, Data: []byte{1, 2}}))

	turn, err := p.Step(context.Background(), conv)
	if err != nil {
		t.Fatalf("Step: %v", err)
	}
	if turn.Stop != model.StopAction {
		t.Errorf("Stop = %s, want action", turn.Stop)
	}
	if turn.Text() != "clicking submit" {
		t.Errorf("Text = %q", turn.Text())
	}
	uses := turn.ActionUses()
	if len(uses) != 1 || uses[0].CallID != "call_1" {
		t.Fatalf("uses = %+v", uses)
	}
	if uses[0].Action.Type != action.Click || uses[0].Action.Point != (action.Point{X: 10, Y: 20}) {
		t.Errorf("action = %+v", uses[0].Action)
	}
	if turn.Usage.InputTokens != 100 || turn.Usage.OutputTokens != 20 {
		t.Errorf("usage = %+v", turn.Usage)
	}

	body := (*bodies)[0]
	for _, want := range []string{`"name":"computer"`, "be helpful", "image_url", "data:image/png;base64"} {
		if !strings.Contains(body, want) {
			t.Errorf("request missing %q\n%s", want, body)
		}
	}
}

func TestStepMultiTurn(t *testing.T) {
	t.Parallel()
	srv, bodies := server(t, toolCallResponse, stopResponse)
	p := compat.New(compat.WithBaseURL(srv.URL))

	conv := &model.Conversation{}
	conv.AddUser(model.Text("task"))
	turn1, err := p.Step(context.Background(), conv)
	if err != nil {
		t.Fatal(err)
	}
	conv.Add(turn1.Message)
	conv.AddTool(model.ActionResult(turn1.ActionUses()[0].CallID, action.Result{Output: "ok"}))
	conv.AddUser(model.ImageContent(action.Image{MIME: action.MIMEPNG, Data: []byte{9}}))

	turn2, err := p.Step(context.Background(), conv)
	if err != nil {
		t.Fatal(err)
	}
	if turn2.Stop != model.StopEnd || turn2.HasActions() {
		t.Errorf("turn2 = %+v", turn2)
	}
	// The tool result references the real call id, and the assistant tool_call
	// appears once (native, not re-encoded).
	body := (*bodies)[1]
	if !strings.Contains(body, `"tool_call_id":"call_1"`) {
		t.Errorf("missing tool result for call_1:\n%s", body)
	}
	if n := strings.Count(body, `"role":"assistant"`); n != 1 {
		t.Errorf("assistant message count = %d, want 1", n)
	}
}

func TestStepAPIError(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = io.WriteString(w, `{"error":{"message":"rate limited"}}`)
	}))
	t.Cleanup(srv.Close)
	p := compat.New(compat.WithBaseURL(srv.URL))
	conv := &model.Conversation{}
	conv.AddUser(model.Text("x"))
	if _, err := p.Step(context.Background(), conv); err == nil {
		t.Error("expected API error")
	}
}

func TestCapabilitiesEmulated(t *testing.T) {
	t.Parallel()
	caps := compat.New().Capabilities()
	if caps.NativeComputerUse {
		t.Error("compat must report emulated (non-native) computer use so grounding engages")
	}
	if !caps.Vision {
		t.Error("compat supports vision")
	}
}
