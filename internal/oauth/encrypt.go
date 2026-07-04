package oauth

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

const secretBytes = 32

// aesGCMCrypter seals/opens token blobs with AES-256-GCM.
type aesGCMCrypter struct{ secret []byte }

func (c aesGCMCrypter) seal(pt []byte) ([]byte, error) {
	gcm, err := c.gcm()
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("oauth: nonce: %w", err)
	}
	return gcm.Seal(nonce, nonce, pt, nil), nil
}

func (c aesGCMCrypter) open(blob []byte) ([]byte, error) {
	gcm, err := c.gcm()
	if err != nil {
		return nil, err
	}
	if len(blob) < gcm.NonceSize() {
		return nil, errors.New("oauth: token blob too short")
	}
	nonce, ct := blob[:gcm.NonceSize()], blob[gcm.NonceSize():]
	pt, err := gcm.Open(nil, nonce, ct, nil)
	if err != nil {
		return nil, errors.New("oauth: decrypt token file (wrong secret or tampered)")
	}
	return pt, nil
}

func (c aesGCMCrypter) gcm() (cipher.AEAD, error) {
	block, err := aes.NewCipher(c.secret)
	if err != nil {
		return nil, fmt.Errorf("oauth: cipher: %w", err)
	}
	return cipher.NewGCM(block)
}

// loadOrCreateSecret reads the per-user AES key, creating it (0600) if absent
// and create is true.
func loadOrCreateSecret(path string, create bool) ([]byte, error) {
	b, err := os.ReadFile(path)
	if err == nil {
		if len(b) != secretBytes {
			return nil, fmt.Errorf("oauth: token secret has wrong length")
		}
		return b, nil
	}
	if !os.IsNotExist(err) {
		return nil, fmt.Errorf("oauth: read secret: %w", err)
	}
	if !create {
		return nil, errors.New("oauth: token secret is missing; cannot decrypt")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("oauth: mkdir secret: %w", err)
	}
	secret := make([]byte, secretBytes)
	if _, err := io.ReadFull(rand.Reader, secret); err != nil {
		return nil, fmt.Errorf("oauth: generate secret: %w", err)
	}
	if err := writeFileAtomic(path, secret, 0o600); err != nil {
		return nil, err
	}
	return secret, nil
}

// writeFileAtomic writes data to path via a temp file + rename at the given perm.
func writeFileAtomic(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, filepath.Base(path)+".*.tmp")
	if err != nil {
		return fmt.Errorf("oauth: temp file: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)

	if err := tmp.Chmod(perm); err != nil {
		tmp.Close()
		return fmt.Errorf("oauth: chmod: %w", err)
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("oauth: write: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("oauth: close: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("oauth: rename: %w", err)
	}
	return nil
}
