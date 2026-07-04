package oauth

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// KeyPrefixProvider namespaces per-provider token keys.
const KeyPrefixProvider = "provider:"

// ProviderKey builds the store key for a provider login.
func ProviderKey(name string) string { return KeyPrefixProvider + name }

// Store persists encrypted tokens under a directory. Files: <name>.token
// (AES-GCM blob), secret.key (shared 32-byte key). Dir 0700, files 0600.
type Store struct{ dir string }

// NewStore builds a store rooted at dir. An empty dir resolves to
// $ARGUS_OAUTH_HOME or <user config dir>/argus/oauth.
func NewStore(dir string) *Store {
	if dir == "" {
		dir = defaultStoreDir()
	}
	return &Store{dir: dir}
}

func defaultStoreDir() string {
	if h := os.Getenv("ARGUS_OAUTH_HOME"); h != "" {
		return h
	}
	base, err := os.UserConfigDir()
	if err != nil || base == "" {
		base = os.TempDir()
	}
	return filepath.Join(base, "argus", "oauth")
}

func (s *Store) tokenPath(key string) string {
	return filepath.Join(s.dir, sanitize(key)+".token")
}

func (s *Store) secretPath() string { return filepath.Join(s.dir, "secret.key") }

// sanitize turns a store key into a safe filename.
func sanitize(key string) string {
	out := make([]rune, 0, len(key))
	for _, r := range key {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			out = append(out, r)
		default:
			out = append(out, '_')
		}
	}
	return string(out)
}

// Load decrypts and returns the token for key. A missing token surfaces the
// underlying os.ErrNotExist so callers can detect "not logged in".
func (s *Store) Load(key string) (Token, error) {
	secret, err := loadOrCreateSecret(s.secretPath(), false)
	if err != nil {
		return Token{}, err
	}
	blob, err := os.ReadFile(s.tokenPath(key))
	if err != nil {
		return Token{}, err
	}
	pt, err := (aesGCMCrypter{secret}).open(blob)
	if err != nil {
		return Token{}, err
	}
	var t Token
	if err := json.Unmarshal(pt, &t); err != nil {
		return Token{}, fmt.Errorf("oauth: decode token: %w", err)
	}
	return t, nil
}

// Save encrypts and writes the token for key.
func (s *Store) Save(key string, t Token) error {
	if err := os.MkdirAll(s.dir, 0o700); err != nil {
		return fmt.Errorf("oauth: mkdir store: %w", err)
	}
	secret, err := loadOrCreateSecret(s.secretPath(), true)
	if err != nil {
		return err
	}
	pt, err := json.Marshal(t)
	if err != nil {
		return fmt.Errorf("oauth: encode token: %w", err)
	}
	blob, err := (aesGCMCrypter{secret}).seal(pt)
	if err != nil {
		return err
	}
	return writeFileAtomic(s.tokenPath(key), blob, 0o600)
}

// Delete removes the token for key (idempotent).
func (s *Store) Delete(key string) error {
	err := os.Remove(s.tokenPath(key))
	if os.IsNotExist(err) {
		return nil
	}
	return err
}
