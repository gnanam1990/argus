package omniparser

import (
	"sync"
	"testing"
	"time"
)

// TestBreakerHalfOpenSingleProbe checks that once the cooldown elapses,
// exactly one call is admitted as a probe while its outcome is unresolved —
// a second concurrent caller must still fail fast rather than also hitting
// the (possibly still-unhealthy) service.
func TestBreakerHalfOpenSingleProbe(t *testing.T) {
	t.Parallel()
	now := time.Unix(1000, 0)
	b := &breaker{threshold: 1, cooldown: time.Minute, now: func() time.Time { return now }}

	if !b.allow() {
		t.Fatal("closed breaker should allow")
	}
	b.failure() // opens (threshold=1)

	if b.allow() {
		t.Error("breaker should still be open within the cooldown")
	}

	now = now.Add(2 * time.Minute) // cooldown elapses
	if !b.allow() {
		t.Fatal("first call after cooldown should be admitted as a probe")
	}
	if b.allow() {
		t.Error("a 2nd concurrent probe must not be admitted while one is in flight")
	}

	b.failure() // the probe fails
	if b.allow() {
		t.Error("breaker should reopen after a failed probe")
	}
}

// TestBreakerHalfOpenProbeSuccessCloses checks a successful probe fully
// closes the breaker (not just for one more call).
func TestBreakerHalfOpenProbeSuccessCloses(t *testing.T) {
	t.Parallel()
	now := time.Unix(1000, 0)
	b := &breaker{threshold: 1, cooldown: time.Minute, now: func() time.Time { return now }}

	b.allow()
	b.failure() // opens

	now = now.Add(2 * time.Minute)
	if !b.allow() {
		t.Fatal("probe should be admitted")
	}
	b.success()

	if !b.allow() {
		t.Error("breaker should be closed after a successful probe")
	}
	if !b.allow() {
		t.Error("breaker should stay closed on a subsequent call")
	}
}

// TestBreakerFailureBelowThresholdStaysClosed checks failures under the
// threshold never open the breaker.
func TestBreakerFailureBelowThresholdStaysClosed(t *testing.T) {
	t.Parallel()
	now := time.Unix(0, 0)
	b := &breaker{threshold: 3, cooldown: time.Minute, now: func() time.Time { return now }}
	for i := 0; i < 2; i++ {
		if !b.allow() {
			t.Fatalf("call %d: breaker should stay closed below threshold", i)
		}
		b.failure()
	}
	if !b.allow() {
		t.Error("breaker should still be closed (2 fails < threshold 3)")
	}
}

// TestBreakerConcurrentAccessRaceFree hammers the breaker from many
// goroutines; the point is to be clean under -race, not to assert a specific
// outcome (the mutex already guarantees a consistent, if arbitrary,
// interleaving).
func TestBreakerConcurrentAccessRaceFree(t *testing.T) {
	t.Parallel()
	var mu sync.Mutex
	now := time.Unix(0, 0)
	b := &breaker{threshold: 5, cooldown: time.Millisecond, now: func() time.Time {
		mu.Lock()
		defer mu.Unlock()
		return now
	}}

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if b.allow() {
				if i%2 == 0 {
					b.failure()
				} else {
					b.success()
				}
			}
		}(i)
	}
	wg.Wait()
}
