package oauth

import (
	"context"
	"net/http"
	"testing"
	"time"
)

func TestLoopbackDeliversCode(t *testing.T) {
	t.Parallel()
	l, err := NewLoopbackListener("STATE", "/callback")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()

	res, err := http.Get(l.RedirectURI() + "?code=abc123&state=STATE")
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	code, err := l.Wait(ctx)
	if err != nil || code != "abc123" {
		t.Errorf("Wait = %q, %v; want abc123", code, err)
	}
}

func TestLoopbackStateMismatch(t *testing.T) {
	t.Parallel()
	l, err := NewLoopbackListener("GOOD", "/callback")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()

	res, err := http.Get(l.RedirectURI() + "?code=x&state=BAD")
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, err := l.Wait(ctx); err != ErrStateMismatch {
		t.Errorf("err = %v, want ErrStateMismatch", err)
	}
}

func TestLoopbackTimeout(t *testing.T) {
	t.Parallel()
	l, err := NewLoopbackListener("S", "/callback")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := l.Wait(ctx); err == nil {
		t.Error("expected timeout error on cancelled ctx")
	}
}
