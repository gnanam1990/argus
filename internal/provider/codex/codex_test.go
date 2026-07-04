package codex_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/gnanam1990/argus/internal/provider/codex"
	"github.com/gnanam1990/argus/pkg/action"
	"github.com/gnanam1990/argus/pkg/model"
)

func sse(events ...string) string {
	var b strings.Builder
	for _, e := range events {
		b.WriteString("data: ")
		b.WriteString(e)
		b.WriteString("\n\n")
	}
	return b.String()
}

const completedUsage = `{"type":"response.completed","response":{"usage":{"input_tokens":100,"output_tokens":20}}}`

func staticToken(access, account string) codex.TokenSource {
	return func(context.Context) (string, string, error) { return access, account, nil }
}

func TestStepRequestShapeAndTextTurn(t *testing.T) {
	t.Parallel()
	var gotBody, gotAuth, gotAcct, gotOriginator, gotAccept, gotPath, gotMethod string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		gotBody, gotAuth, gotAcct = string(b), r.Header.Get("Authorization"), r.Header.Get("chatgpt-account-id")
		gotOriginator, gotAccept, gotPath, gotMethod = r.Header.Get("originator"), r.Header.Get("Accept"), r.URL.Path, r.Method
		_, _ = io.WriteString(w, sse(
			`{"type":"response.output_text.delta","delta":"the task is "}`,
			`{"type":"response.output_text.delta","delta":"complete"}`,
			completedUsage,
		))
	}))
	t.Cleanup(srv.Close)

	p := codex.New(codex.WithBaseURL(srv.URL), codex.WithModel("gpt-5.5"), codex.WithTokenSource(staticToken("AT", "ACCT")))
	conv := &model.Conversation{System: "be careful"}
	conv.AddUser(model.Text("do it"))

	turn, err := p.Step(context.Background(), conv)
	if err != nil {
		t.Fatalf("Step: %v", err)
	}
	if turn.Stop != model.StopEnd || turn.Text() != "the task is complete" {
		t.Errorf("turn = %+v / %q", turn, turn.Text())
	}
	if turn.Usage.InputTokens != 100 || turn.Usage.OutputTokens != 20 {
		t.Errorf("usage = %+v", turn.Usage)
	}

	if gotMethod != "POST" || !strings.HasSuffix(gotPath, "/responses") {
		t.Errorf("request = %s %s", gotMethod, gotPath)
	}
	if gotAuth != "Bearer AT" || gotAcct != "ACCT" || gotOriginator != "codex_cli_rs" || gotAccept != "text/event-stream" {
		t.Errorf("headers auth=%q acct=%q orig=%q accept=%q", gotAuth, gotAcct, gotOriginator, gotAccept)
	}
	for _, want := range []string{`"instructions":"be careful"`, `"stream":true`, `"input"`, `"type":"input_text"`} {
		if !strings.Contains(gotBody, want) {
			t.Errorf("body missing %q\n%s", want, gotBody)
		}
	}
	if strings.Contains(gotBody, `"messages"`) {
		t.Error("body must use Responses input[], not chat-completions messages")
	}
}

func TestStepToolCallToCanonicalAction(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, sse(
			`{"type":"response.output_item.added","item_id":"i1","item":{"type":"function_call","call_id":"call_1","name":"computer"}}`,
			`{"type":"response.function_call_arguments.delta","item_id":"i1","delta":"{\"action\":\"click\","}`,
			`{"type":"response.function_call_arguments.delta","item_id":"i1","delta":"\"x\":10,\"y\":20}"}`,
			`{"type":"response.output_item.done","item_id":"i1"}`,
			completedUsage,
		))
	}))
	t.Cleanup(srv.Close)

	p := codex.New(codex.WithBaseURL(srv.URL), codex.WithTokenSource(staticToken("AT", "ACCT")))
	conv := &model.Conversation{}
	conv.AddUser(model.Text("click submit"))

	turn, err := p.Step(context.Background(), conv)
	if err != nil {
		t.Fatalf("Step: %v", err)
	}
	if turn.Stop != model.StopAction {
		t.Errorf("stop = %s, want action", turn.Stop)
	}
	uses := turn.ActionUses()
	if len(uses) != 1 || uses[0].CallID != "call_1" {
		t.Fatalf("uses = %+v", uses)
	}
	a := uses[0].Action
	if a.Type != action.Click || a.Point != (action.Point{X: 10, Y: 20}) {
		t.Errorf("action = %+v, want click (10,20)", a)
	}
}

func TestStepOmitsEmptyAccount(t *testing.T) {
	t.Parallel()
	var hadHeader int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, ok := r.Header["Chatgpt-Account-Id"]; ok {
			atomic.StoreInt32(&hadHeader, 1)
		}
		_, _ = io.WriteString(w, sse(completedUsage))
	}))
	t.Cleanup(srv.Close)

	p := codex.New(codex.WithBaseURL(srv.URL), codex.WithTokenSource(staticToken("AT", "")))
	conv := &model.Conversation{}
	conv.AddUser(model.Text("x"))
	if _, err := p.Step(context.Background(), conv); err != nil {
		t.Fatal(err)
	}
	if atomic.LoadInt32(&hadHeader) != 0 {
		t.Error("empty account should omit the chatgpt-account-id header")
	}
}

func TestStep401RefreshRetry(t *testing.T) {
	t.Parallel()
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if atomic.AddInt32(&calls, 1) == 1 {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_, _ = io.WriteString(w, sse(`{"type":"response.output_text.delta","delta":"ok"}`, completedUsage))
	}))
	t.Cleanup(srv.Close)

	var forced int32
	p := codex.New(srvOpts(srv.URL,
		staticToken("AT", "ACCT"),
		func(context.Context) (string, string, error) { atomic.AddInt32(&forced, 1); return "AT2", "ACCT", nil },
	)...)
	conv := &model.Conversation{}
	conv.AddUser(model.Text("x"))

	turn, err := p.Step(context.Background(), conv)
	if err != nil {
		t.Fatalf("Step: %v", err)
	}
	if turn.Text() != "ok" || atomic.LoadInt32(&calls) != 2 || atomic.LoadInt32(&forced) != 1 {
		t.Errorf("calls=%d forced=%d text=%q; want 2/1/ok", calls, forced, turn.Text())
	}
}

func srvOpts(url string, tok, force codex.TokenSource) []codex.Option {
	return []codex.Option{codex.WithBaseURL(url), codex.WithTokenSource(tok), codex.WithForceRefresh(force)}
}

func TestCapabilities(t *testing.T) {
	t.Parallel()
	caps := codex.New().Capabilities()
	if caps.NativeComputerUse || !caps.Vision {
		t.Errorf("caps = %+v", caps)
	}
}

// sequentialServer returns the given SSE bodies in order (repeating the last
// for any extra requests) and records each request body, mirroring the
// compat/anthropic packages' captureServer helpers.
func sequentialServer(t *testing.T, responses ...string) (*httptest.Server, *[]string) {
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
		_, _ = io.WriteString(w, resp)
	}))
	t.Cleanup(srv.Close)
	return srv, &bodies
}

// toolCallSSE builds a one-turn SSE stream emitting a single "computer"
// function_call with distinct item/call ids, so a multi-turn script can be
// scripted deterministically.
func toolCallSSE(itemID, callID string) string {
	return sse(
		`{"type":"response.output_item.added","item_id":"`+itemID+`","item":{"type":"function_call","call_id":"`+callID+`","name":"computer"}}`,
		`{"type":"response.function_call_arguments.delta","item_id":"`+itemID+`","delta":"{\"action\":\"click\",\"x\":10,\"y\":20}"}`,
		`{"type":"response.output_item.done","item_id":"`+itemID+`"}`,
		completedUsage,
	)
}

// stepScreenshotScript drives 4 Steps, each adding one screenshot observation,
// wiring each turn's function_call through a matching function_call_output
// before the next step — mirroring how the agent loop actually drives a
// provider.
func stepScreenshotScript(t *testing.T, p *codex.Provider) {
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

// codexReqItem is the subset of a decoded Responses "input" item this file's
// retention tests need.
type codexReqItem struct {
	Type    string `json:"type"`
	Role    string `json:"role,omitempty"`
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text,omitempty"`
	} `json:"content,omitempty"`
}

type codexReqBody struct {
	Input []codexReqItem `json:"input"`
}

func decodeCodexRequest(t *testing.T, body string) codexReqBody {
	t.Helper()
	var req codexReqBody
	if err := json.Unmarshal([]byte(body), &req); err != nil {
		t.Fatalf("decode request body: %v\n%s", err, body)
	}
	return req
}

// countCodexImages returns the number of live "input_image" parts and the
// number of "input_text" parts carrying the pruned-screenshot placeholder,
// across "message"/"user" input items (function_call/function_call_output
// items never carry images).
func countCodexImages(req codexReqBody) (live, pruned int) {
	for _, item := range req.Input {
		if item.Type != "message" || item.Role != "user" {
			continue
		}
		for _, part := range item.Content {
			switch {
			case part.Type == "input_image":
				live++
			case part.Type == "input_text" && part.Text == wantPrunedText:
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
	srv, bodies := sequentialServer(t,
		toolCallSSE("i1", "call_1"), toolCallSSE("i2", "call_2"),
		toolCallSSE("i3", "call_3"), toolCallSSE("i4", "call_4"),
	)
	p := codex.New(codex.WithBaseURL(srv.URL), codex.WithTokenSource(staticToken("AT", "ACCT")), codex.WithImageRetention(2))
	stepScreenshotScript(t, p)

	if len(*bodies) != 4 {
		t.Fatalf("requests = %d, want 4", len(*bodies))
	}
	req := decodeCodexRequest(t, (*bodies)[3])

	live, pruned := countCodexImages(req)
	if live != 2 {
		t.Errorf("live images in 4th request = %d, want 2", live)
	}
	if pruned != 2 {
		t.Errorf("pruned placeholders in 4th request = %d, want 2", pruned)
	}
}

func TestImageRetentionZeroKeepsAll(t *testing.T) {
	t.Parallel()
	srv, bodies := sequentialServer(t,
		toolCallSSE("i1", "call_1"), toolCallSSE("i2", "call_2"),
		toolCallSSE("i3", "call_3"), toolCallSSE("i4", "call_4"),
	)
	p := codex.New(codex.WithBaseURL(srv.URL), codex.WithTokenSource(staticToken("AT", "ACCT")), codex.WithImageRetention(0))
	stepScreenshotScript(t, p)

	req := decodeCodexRequest(t, (*bodies)[3])
	live, pruned := countCodexImages(req)
	if live != 4 {
		t.Errorf("live images in 4th request = %d, want 4 (retention disabled)", live)
	}
	if pruned != 0 {
		t.Errorf("pruned placeholders in 4th request = %d, want 0", pruned)
	}
}
