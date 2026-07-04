package compat_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
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

// TestStepUsesMaxCompletionTokensForNewModels covers o-series/gpt-5.x: they
// reject max_tokens outright, so the request must carry
// max_completion_tokens instead.
func TestStepUsesMaxCompletionTokensForNewModels(t *testing.T) {
	t.Parallel()
	srv, bodies := server(t, stopResponse)
	p := compat.New(compat.WithBaseURL(srv.URL), compat.WithModel("gpt-5.5"))
	conv := &model.Conversation{}
	conv.AddUser(model.Text("x"))
	if _, err := p.Step(context.Background(), conv); err != nil {
		t.Fatal(err)
	}
	body := (*bodies)[0]
	if !strings.Contains(body, `"max_completion_tokens"`) {
		t.Errorf("body missing max_completion_tokens:\n%s", body)
	}
	if strings.Contains(body, `"max_tokens"`) {
		t.Errorf("body must not send max_tokens for gpt-5.5:\n%s", body)
	}
}

// TestStepUsesMaxTokensForOlderModels covers older/self-hosted servers (e.g.
// Ollama) that expect max_tokens and don't understand max_completion_tokens.
func TestStepUsesMaxTokensForOlderModels(t *testing.T) {
	t.Parallel()
	srv, bodies := server(t, stopResponse)
	p := compat.New(compat.WithBaseURL(srv.URL), compat.WithModel("gpt-4o"))
	conv := &model.Conversation{}
	conv.AddUser(model.Text("x"))
	if _, err := p.Step(context.Background(), conv); err != nil {
		t.Fatal(err)
	}
	body := (*bodies)[0]
	if !strings.Contains(body, `"max_tokens"`) {
		t.Errorf("body missing max_tokens:\n%s", body)
	}
	if strings.Contains(body, `"max_completion_tokens"`) {
		t.Errorf("body must not send max_completion_tokens for gpt-4o:\n%s", body)
	}
}

const contentArrayResponse = `{
  "choices": [{
    "message": {
      "role": "assistant",
      "content": [{"type":"text","text":"click "},{"type":"text","text":"submit"}]
    },
    "finish_reason": "stop"
  }],
  "usage": {"prompt_tokens": 10, "completion_tokens": 5}
}`

// TestStepContentArrayVariantExtractsText covers the array-of-parts content
// shape some OpenAI-compatible servers emit instead of a plain string; it
// previously failed the string type-assert and silently dropped all text.
func TestStepContentArrayVariantExtractsText(t *testing.T) {
	t.Parallel()
	srv, _ := server(t, contentArrayResponse)
	p := compat.New(compat.WithBaseURL(srv.URL))
	conv := &model.Conversation{}
	conv.AddUser(model.Text("x"))

	turn, err := p.Step(context.Background(), conv)
	if err != nil {
		t.Fatal(err)
	}
	if turn.Text() != "click submit" {
		t.Errorf("Text() = %q, want %q", turn.Text(), "click submit")
	}
}

// errReader always fails, simulating a connection that drops mid-body.
type errReader struct{}

func (errReader) Read([]byte) (int, error) { return 0, errors.New("boom") }

// errTransport returns a 200 whose body errors on read, so post()'s
// io.ReadAll has something to fail on without a real dropped connection.
type errTransport struct{}

func (errTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(errReader{}),
		Header:     make(http.Header),
	}, nil
}

// TestStepReadResponseBodyErrorIsSurfaced covers the body-read error path:
// previously `raw, _ := io.ReadAll(...)` discarded the error and fed a
// truncated/empty body into json.Unmarshal, producing a confusing decode
// error instead of the real cause.
func TestStepReadResponseBodyErrorIsSurfaced(t *testing.T) {
	t.Parallel()
	p := compat.New(compat.WithHTTPClient(&http.Client{Transport: errTransport{}}))
	conv := &model.Conversation{}
	conv.AddUser(model.Text("x"))

	_, err := p.Step(context.Background(), conv)
	if err == nil || !strings.Contains(err.Error(), "read response") {
		t.Errorf("err = %v, want a wrapped \"read response\" error", err)
	}
}

// TestStepCapsResponseBodyRead confirms an oversized response body (bigger
// than the 16 MiB cap) is truncated rather than fully buffered — proven by
// the resulting decode failure on this deliberately-truncated JSON, not a
// hang or an out-of-memory read.
func TestStepCapsResponseBodyRead(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"pad":"`))
		_, _ = w.Write(bytes.Repeat([]byte("a"), 17<<20)) // > 16 MiB cap
		// Deliberately never closes the JSON string/object.
	}))
	t.Cleanup(srv.Close)

	p := compat.New(compat.WithBaseURL(srv.URL))
	conv := &model.Conversation{}
	conv.AddUser(model.Text("x"))
	if _, err := p.Step(context.Background(), conv); err == nil {
		t.Error("expected a decode error from the capped (truncated) body")
	}
}

// TestStepAppliesTemperatureAndSeed covers StepConfig plumbing: Temperature
// and Seed must reach the wire request when set.
func TestStepAppliesTemperatureAndSeed(t *testing.T) {
	t.Parallel()
	srv, bodies := server(t, stopResponse)
	p := compat.New(compat.WithBaseURL(srv.URL))
	conv := &model.Conversation{}
	conv.AddUser(model.Text("x"))

	if _, err := p.Step(context.Background(), conv, model.WithTemperature(0.7), model.WithSeed(42)); err != nil {
		t.Fatal(err)
	}
	body := (*bodies)[0]
	if !strings.Contains(body, `"temperature":0.7`) {
		t.Errorf("body missing temperature:\n%s", body)
	}
	if !strings.Contains(body, `"seed":42`) {
		t.Errorf("body missing seed:\n%s", body)
	}
}

// TestEncodeNewResyncsOnConversationReset covers the resync guard: a second,
// unrelated (shorter) conversation driven through the same provider instance
// — e.g. a second Run reusing it — must not resend the first conversation's
// content or actions.
func TestEncodeNewResyncsOnConversationReset(t *testing.T) {
	t.Parallel()
	srv, bodies := server(t, toolCallResponse, toolCallResponse, stopResponse)
	p := compat.New(compat.WithBaseURL(srv.URL))

	conv1 := &model.Conversation{}
	conv1.AddUser(model.Text("first task"))
	turn1, err := p.Step(context.Background(), conv1)
	if err != nil {
		t.Fatalf("step1: %v", err)
	}
	conv1.Add(turn1.Message)
	conv1.AddTool(model.ActionResult(turn1.ActionUses()[0].CallID, action.Result{Output: "done"}))

	if _, err := p.Step(context.Background(), conv1); err != nil {
		t.Fatalf("step2: %v", err)
	}

	conv2 := &model.Conversation{}
	conv2.AddUser(model.Text("second task"))
	if _, err := p.Step(context.Background(), conv2); err != nil {
		t.Fatalf("step3: %v", err)
	}

	body := (*bodies)[2]
	for _, stale := range []string{"first task", "call_1"} {
		if strings.Contains(body, stale) {
			t.Errorf("resync failed: 3rd request still contains stale content %q:\n%s", stale, body)
		}
	}
	if !strings.Contains(body, "second task") {
		t.Errorf("3rd request missing the new conversation's content:\n%s", body)
	}
}

// TestResultTextIncludesCursorPosition covers cursor_position result
// reporting: Result.Cursor must reach the model, since a cursor_position
// action's only useful output is the coordinates.
func TestResultTextIncludesCursorPosition(t *testing.T) {
	t.Parallel()
	srv, bodies := server(t, toolCallResponse, stopResponse)
	p := compat.New(compat.WithBaseURL(srv.URL))

	conv := &model.Conversation{}
	conv.AddUser(model.Text("where is the cursor"))
	turn1, err := p.Step(context.Background(), conv)
	if err != nil {
		t.Fatalf("step1: %v", err)
	}
	conv.Add(turn1.Message)
	conv.AddTool(model.ActionResult(turn1.ActionUses()[0].CallID, action.Result{Cursor: action.Point{X: 820, Y: 540}}))

	if _, err := p.Step(context.Background(), conv); err != nil {
		t.Fatalf("step2: %v", err)
	}

	body := (*bodies)[1]
	if !strings.Contains(body, "820") || !strings.Contains(body, "540") {
		t.Errorf("2nd request missing cursor coordinates:\n%s", body)
	}
}
