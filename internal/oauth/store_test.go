package oauth

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestStoreRoundTripAndEncryption(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	s := NewStore(dir)
	key := ProviderKey("chatgpt")
	tok := Token{AccessToken: "super-secret-access", RefreshToken: "RT", ExpiresAt: time.Unix(1700, 0).UTC(), Account: "acct-1"}

	if err := s.Save(key, tok); err != nil {
		t.Fatal(err)
	}
	got, err := s.Load(key)
	if err != nil {
		t.Fatal(err)
	}
	if got.AccessToken != tok.AccessToken || got.RefreshToken != tok.RefreshToken || got.Account != tok.Account {
		t.Errorf("round-trip = %+v", got)
	}

	// The on-disk blob must NOT contain the plaintext token.
	blob, _ := os.ReadFile(s.tokenPath(key))
	if bytes.Contains(blob, []byte("super-secret-access")) {
		t.Error("plaintext token leaked to disk")
	}
}

func TestStoreTamperDetected(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	s := NewStore(dir)
	key := ProviderKey("x")
	_ = s.Save(key, Token{AccessToken: "a"})

	path := s.tokenPath(key)
	blob, _ := os.ReadFile(path)
	blob[len(blob)-1] ^= 0xFF // flip a ciphertext byte
	_ = os.WriteFile(path, blob, 0o600)

	if _, err := s.Load(key); err == nil {
		t.Error("expected a decrypt/tamper error")
	}
}

func TestStorePermissions(t *testing.T) {
	t.Parallel()
	// A fresh subdir so the store creates it (0700), rather than inheriting the
	// pre-existing temp dir's mode.
	dir := filepath.Join(t.TempDir(), "oauth")
	s := NewStore(dir)
	key := ProviderKey("p")
	if err := s.Save(key, Token{AccessToken: "a"}); err != nil {
		t.Fatal(err)
	}

	assertMode := func(path string, want os.FileMode) {
		fi, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if fi.Mode().Perm() != want {
			t.Errorf("%s mode = %v, want %v", filepath.Base(path), fi.Mode().Perm(), want)
		}
	}
	assertMode(s.tokenPath(key), 0o600)
	assertMode(s.secretPath(), 0o600)
	assertMode(dir, 0o700)
}

func TestStoreDeleteIdempotent(t *testing.T) {
	t.Parallel()
	s := NewStore(t.TempDir())
	key := ProviderKey("p")
	_ = s.Save(key, Token{AccessToken: "a"})
	if err := s.Delete(key); err != nil {
		t.Fatal(err)
	}
	if err := s.Delete(key); err != nil { // second delete is a no-op
		t.Errorf("second delete = %v", err)
	}
}

func TestStoreMissingIsNotExist(t *testing.T) {
	t.Parallel()
	s := NewStore(t.TempDir())
	// Save something first so the shared secret exists.
	_ = s.Save(ProviderKey("seed"), Token{AccessToken: "a"})
	if _, err := s.Load(ProviderKey("absent")); !os.IsNotExist(err) {
		t.Errorf("err = %v, want os.ErrNotExist", err)
	}
}

func TestSealFreshNonce(t *testing.T) {
	t.Parallel()
	c := aesGCMCrypter{secret: make([]byte, secretBytes)}
	a, _ := c.seal([]byte("same"))
	b, _ := c.seal([]byte("same"))
	if bytes.Equal(a, b) {
		t.Error("seal reused a nonce (identical blobs for identical plaintext)")
	}
}
