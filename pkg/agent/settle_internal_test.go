package agent

import (
	"context"
	"testing"
	"time"
)

func TestSettleZeroDelayNoWait(t *testing.T) {
	t.Parallel()
	r := &Runner{settleDelay: 0}
	start := time.Now()
	if err := r.settle(context.Background()); err != nil {
		t.Fatal(err)
	}
	if time.Since(start) > 50*time.Millisecond {
		t.Error("zero settle delay must not wait")
	}
}

func TestSettleShortDelayCompletes(t *testing.T) {
	t.Parallel()
	r := &Runner{settleDelay: 5 * time.Millisecond}
	if err := r.settle(context.Background()); err != nil {
		t.Fatalf("short settle should complete: %v", err)
	}
}

func TestSettleRespectsCancellation(t *testing.T) {
	t.Parallel()
	r := &Runner{settleDelay: time.Hour}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := r.settle(ctx); err == nil {
		t.Error("a cancelled context must abort the settle wait promptly")
	}
}
