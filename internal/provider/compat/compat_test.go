package compat_test

import (
	"context"
	"encoding/json"
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

// toolCallResp builds a tool_calls response with a distinct id, so a
// multi-turn script can be scripted deterministically.
func toolCallResp(id string) string {
	return `{
	  "choices": [{
	    "message": {
	      "role": "assistant",
	      "content": "clicking",
	      "tool_calls": [{"id": "` + id + `", "type": "function", "function": {"name": "computer", "arguments": "{\"action\":\"click\",\"x\":10,\"y\":20}"}}]
	    },
	    "finish_reason": "tool_calls"
	  }],
	  "usage": {"prompt_tokens": 10, "completion_tokens": 5}
	}`
}

// stepScreenshotScript drives 4 Steps, each adding one screenshot observation,
// wiring each turn's tool_call through a matching tool result before the next
// step — mirroring how the agent loop actually drives a provider.
func stepScreenshotScript(t *testing.T, p *compat.Provider) {
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

// compatReqMessage is the subset of a decoded chat message this file's
// retention tests need. Content is left raw since it is either a string
// (system/tool/assistant) or a []contentPart array (user).
type compatReqMessage struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
}

type compatReqBody struct {
	Messages []compatReqMessage `json:"messages"`
}

type compatReqPart struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

func decodeCompatRequest(t *testing.T, body string) compatReqBody {
	t.Helper()
	var req compatReqBody
	if err := json.Unmarshal([]byte(body), &req); err != nil {
		t.Fatalf("decode request body: %v\n%s", err, body)
	}
	return req
}

// countCompatImages returns the number of live "image_url" content parts and
// the number of "text" parts carrying the pruned-screenshot placeholder.
// Messages whose content is a plain string (system/tool/assistant) fail the
// []contentPart decode and are skipped, since only user messages carry parts.
func countCompatImages(req compatReqBody) (live, pruned int) {
	for _, m := range req.Messages {
		var parts []compatReqPart
		if err := json.Unmarshal(m.Content, &parts); err != nil {
			continue
		}
		for _, part := range parts {
			switch {
			case part.Type == "image_url":
				live++
			case part.Type == "text" && part.Text == wantPrunedText:
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
	srv, bodies := server(t, toolCallResp("call_1"), toolCallResp("call_2"), toolCallResp("call_3"), toolCallResp("call_4"))
	p := compat.New(compat.WithBaseURL(srv.URL), compat.WithImageRetention(2))
	stepScreenshotScript(t, p)

	if len(*bodies) != 4 {
		t.Fatalf("requests = %d, want 4", len(*bodies))
	}
	req := decodeCompatRequest(t, (*bodies)[3])

	live, pruned := countCompatImages(req)
	if live != 2 {
		t.Errorf("live images in 4th request = %d, want 2", live)
	}
	if pruned != 2 {
		t.Errorf("pruned placeholders in 4th request = %d, want 2", pruned)
	}
}

func TestImageRetentionZeroKeepsAll(t *testing.T) {
	t.Parallel()
	srv, bodies := server(t, toolCallResp("call_1"), toolCallResp("call_2"), toolCallResp("call_3"), toolCallResp("call_4"))
	p := compat.New(compat.WithBaseURL(srv.URL), compat.WithImageRetention(0))
	stepScreenshotScript(t, p)

	req := decodeCompatRequest(t, (*bodies)[3])
	live, pruned := countCompatImages(req)
	if live != 4 {
		t.Errorf("live images in 4th request = %d, want 4 (retention disabled)", live)
	}
	if pruned != 0 {
		t.Errorf("pruned placeholders in 4th request = %d, want 0", pruned)
	}
}
