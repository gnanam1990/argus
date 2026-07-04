// Package codex adapts the ChatGPT "Codex" backend (the OpenAI Responses API at
// chatgpt.com/backend-api/codex) to the model.Provider seam, credentialed from
// an OAuth subscription token rather than an API key. It reuses the OpenAI
// action normalizer so the canonical action path is identical to the compat
// adapter; only the wire (Responses/SSE) and the credential differ.
package codex

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
)

const (
	chatgptAuthClaimNamespace = "https://api.openai.com/auth"
	chatgptAccountClaim       = "chatgpt_account_id"
)

// ExtractChatGPTAccountID reads the chatgpt-account-id from an OAuth id_token
// (JWT). It parses the payload segment without verifying the signature (matching
// the Codex CLI posture): claims["https://api.openai.com/auth"]["chatgpt_account_id"],
// falling back to a top-level "chatgpt_account_id".
func ExtractChatGPTAccountID(idToken string) (string, error) {
	parts := strings.Split(idToken, ".")
	if len(parts) != 3 {
		return "", fmt.Errorf("codex: id_token is not a JWT")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return "", fmt.Errorf("codex: decode id_token payload: %w", err)
	}
	var claims map[string]any
	if err := json.Unmarshal(payload, &claims); err != nil {
		return "", fmt.Errorf("codex: parse id_token claims: %w", err)
	}
	if auth, ok := claims[chatgptAuthClaimNamespace].(map[string]any); ok {
		if id, ok := auth[chatgptAccountClaim].(string); ok && id != "" {
			return id, nil
		}
	}
	if id, ok := claims[chatgptAccountClaim].(string); ok && id != "" {
		return id, nil
	}
	return "", fmt.Errorf("codex: id_token has no chatgpt account id")
}
