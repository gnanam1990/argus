package codex_test

import (
	"context"
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
