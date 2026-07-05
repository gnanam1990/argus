package approval_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/gnanam1990/argus/internal/computeruse/approval"
)

func newStore(t *testing.T) *approval.FileStore {
	t.Helper()
	path := filepath.Join(t.TempDir(), "cu-approvals.json")
	return approval.NewFileStore(path, approval.WithClock(func() time.Time { return time.Unix(1000, 0) }))
}

func TestUnknownAppIsPending(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	d, err := s.Get(context.Background(), "com.unknown.app")
	if err != nil {
		t.Fatal(err)
	}
	if d != approval.Pending {
		t.Errorf("unknown app = %q, want pending (deny-by-default)", d)
	}
}

func TestSetGetRemoveList(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	ctx := context.Background()

	if err := s.Set(ctx, "com.apple.clock", approval.Approved); err != nil {
		t.Fatal(err)
	}
	if err := s.Set(ctx, "com.evil.app", approval.Denied); err != nil {
		t.Fatal(err)
	}
	if d, _ := s.Get(ctx, "com.apple.clock"); d != approval.Approved {
		t.Errorf("clock = %q, want approved", d)
	}

	list, _ := s.List(ctx)
	if len(list) != 2 || list[0].BundleIdentifier != "com.apple.clock" {
		t.Errorf("list = %+v (want 2, sorted)", list)
	}

	if err := s.Remove(ctx, "com.apple.clock"); err != nil {
		t.Fatal(err)
	}
	if d, _ := s.Get(ctx, "com.apple.clock"); d != approval.Pending {
		t.Errorf("removed app = %q, want pending", d)
	}
}

func TestInvalidDecisionRejected(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	if err := s.Set(context.Background(), "x", approval.Decision("maybe")); err == nil {
		t.Error("invalid decision should be rejected")
	}
}

func TestPersistsAcrossInstances(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "cu-approvals.json")
	s1 := approval.NewFileStore(path)
	if err := s1.Set(context.Background(), "com.apple.clock", approval.Approved); err != nil {
		t.Fatal(err)
	}
	// The file is 0600.
	if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0o600 {
		t.Errorf("perms = %v (err %v), want 0600", info.Mode().Perm(), err)
	}
	s2 := approval.NewFileStore(path)
	if d, _ := s2.Get(context.Background(), "com.apple.clock"); d != approval.Approved {
		t.Errorf("reloaded = %q, want approved", d)
	}
}

func TestMigrateFrom(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	legacy := filepath.Join(dir, "legacy.json")
	recs := []approval.Record{{BundleIdentifier: "com.apple.music", Decision: approval.Approved}}
	data, _ := json.Marshal(recs)
	if err := os.WriteFile(legacy, data, 0o600); err != nil {
		t.Fatal(err)
	}

	s := approval.NewFileStore(filepath.Join(dir, "cu-approvals.json"))
	if err := s.MigrateFrom(legacy); err != nil {
		t.Fatal(err)
	}
	if d, _ := s.Get(context.Background(), "com.apple.music"); d != approval.Approved {
		t.Errorf("migrated = %q, want approved", d)
	}

	// Migration must not clobber an existing store.
	s2 := approval.NewFileStore(filepath.Join(dir, "cu-approvals.json"))
	_ = s2.Set(context.Background(), "com.apple.music", approval.Denied)
	if err := s2.MigrateFrom(legacy); err != nil {
		t.Fatal(err)
	}
	if d, _ := s2.Get(context.Background(), "com.apple.music"); d != approval.Denied {
		t.Errorf("migration clobbered an existing decision: %q", d)
	}
}

func TestConcurrentSetGet(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_ = s.Set(context.Background(), "com.app", approval.Approved)
			_, _ = s.Get(context.Background(), "com.app")
		}(i)
	}
	wg.Wait()
	if d, _ := s.Get(context.Background(), "com.app"); d != approval.Approved {
		t.Errorf("final = %q", d)
	}
}
