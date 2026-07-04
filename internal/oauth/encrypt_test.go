package oauth

import (
	"bytes"
	"path/filepath"
	"sync"
	"testing"
)

// TestLoadOrCreateSecretConcurrent exercises H9: two goroutines racing the
// first-ever creation of secret.key on a fresh directory must converge on the
// SAME key bytes rather than each generating and persisting its own (which
// would silently orphan whichever provider's tokens were sealed under the
// loser's key).
func TestLoadOrCreateSecretConcurrent(t *testing.T) {
	t.Parallel()
	dir := filepath.Join(t.TempDir(), "oauth")
	path := filepath.Join(dir, "secret.key")

	const n = 20
	results := make([][]byte, n)
	errs := make([]error, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			results[i], errs[i] = loadOrCreateSecret(path, true)
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("goroutine %d: %v", i, err)
		}
		if len(results[i]) != secretBytes {
			t.Fatalf("goroutine %d: len = %d, want %d", i, len(results[i]), secretBytes)
		}
		if !bytes.Equal(results[i], results[0]) {
			t.Errorf("goroutine %d produced a different secret than goroutine 0 — two keys were created", i)
		}
	}
}

// TestLoadOrCreateSecretNoCreateMissing keeps the pre-existing "not logged
// in"-shaped error for the create=false / absent-file path (Store.Load's use)
// unchanged by the O_EXCL rewrite.
func TestLoadOrCreateSecretNoCreateMissing(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if _, err := loadOrCreateSecret(filepath.Join(dir, "secret.key"), false); err == nil {
		t.Error("expected an error reading a missing secret with create=false")
	}
}

// TestLoadOrCreateSecretWrongLength still rejects a corrupt/short secret file.
func TestLoadOrCreateSecretWrongLength(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "secret.key")
	if err := writeFileAtomic(path, []byte("short"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadOrCreateSecret(path, true); err == nil {
		t.Error("expected an error for a wrong-length secret file")
	}
}
