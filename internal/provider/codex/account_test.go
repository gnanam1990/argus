package codex

import (
	"encoding/base64"
	"encoding/json"
	"testing"
)

func makeJWT(t *testing.T, claims map[string]any) string {
	t.Helper()
	enc := func(v any) string {
		b, _ := json.Marshal(v)
		return base64.RawURLEncoding.EncodeToString(b)
	}
	return enc(map[string]any{"alg": "none"}) + "." + enc(claims) + ".sig"
}

func TestExtractAccountNested(t *testing.T) {
	t.Parallel()
	tok := makeJWT(t, map[string]any{
		"https://api.openai.com/auth": map[string]any{"chatgpt_account_id": "acct-123"},
	})
	got, err := ExtractChatGPTAccountID(tok)
	if err != nil || got != "acct-123" {
		t.Errorf("got %q, %v; want acct-123", got, err)
	}
}

func TestExtractAccountTopLevelFallback(t *testing.T) {
	t.Parallel()
	tok := makeJWT(t, map[string]any{"chatgpt_account_id": "top-1"})
	got, err := ExtractChatGPTAccountID(tok)
	if err != nil || got != "top-1" {
		t.Errorf("got %q, %v; want top-1", got, err)
	}
}

func TestExtractAccountErrors(t *testing.T) {
	t.Parallel()
	for _, bad := range []string{"not-a-jwt", "a.b", makeJWT(t, map[string]any{"sub": "x"})} {
		if _, err := ExtractChatGPTAccountID(bad); err == nil {
			t.Errorf("ExtractChatGPTAccountID(%q) = nil error, want error", bad)
		}
	}
}
