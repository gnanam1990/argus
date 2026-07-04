package oauth

import (
	"strconv"
	"strings"
)

// PresetsAllowed reports whether OAuth presets are enabled. Presets reuse
// public, undocumented CLI client identities and may violate provider ToS, so
// they are opt-in behind ARGUS_OAUTH_ALLOW_PRESETS=1.
func PresetsAllowed(getenv func(string) string) bool {
	return getenv("ARGUS_OAUTH_ALLOW_PRESETS") == "1"
}

// basePresets are the built-in OAuth client configs. Every field is overridable
// per provider via ARGUS_OAUTH_<PROVIDER>_* so an operator can point at a proxy
// or supply their own client id without a code change.
var basePresets = map[string]Config{
	// ChatGPT (Codex) — reuses the public Codex CLI client identity. The token
	// targets chatgpt.com/backend-api/codex (Stage C), not api.openai.com.
	"chatgpt": {
		ClientID:              "app_EMoamEEZ73f0CkXaXp7hrann",
		AuthorizationEndpoint: "https://auth.openai.com/oauth/authorize",
		TokenEndpoint:         "https://auth.openai.com/oauth/token",
		IssuerURL:             "https://auth.openai.com",
		Scopes:                []string{"openid", "profile", "email", "offline_access"},
		RedirectPort:          1455,
		RedirectPath:          "/auth/callback",
		// The Codex client registers "localhost" (not 127.0.0.1); the redirect_uri
		// must match that exactly or the authorize request is rejected.
		RedirectHost:    "localhost",
		ExtraAuthParams: map[string]string{"id_token_add_organizations": "true", "codex_cli_simplified_flow": "true"},
	},
	// xAI (Grok) — public Grok CLI client; standard OpenAI-compatible API, so
	// the token is used as a plain Bearer against api.x.ai (no special adapter).
	"xai": {
		ClientID:                    "b1a00492-073a-47ea-816f-4c329264a828",
		AuthorizationEndpoint:       "https://auth.x.ai/oauth2/authorize",
		TokenEndpoint:               "https://auth.x.ai/oauth2/token",
		DeviceAuthorizationEndpoint: "https://auth.x.ai/oauth2/device/code",
		IssuerURL:                   "https://auth.x.ai",
		Scopes:                      []string{"openid", "profile", "email", "offline_access", "grok-cli:access", "api:access"},
		RedirectPath:                "/callback",
	},
}

// Preset returns the environment-overlaid OAuth Config for a provider.
func Preset(name string, getenv func(string) string) (Config, bool) {
	cfg, ok := basePresets[name]
	if !ok {
		return Config{}, false
	}
	applyPresetEnv(&cfg, strings.ToUpper(name), getenv)
	return cfg, true
}

func applyPresetEnv(cfg *Config, up string, getenv func(string) string) {
	set := func(suffix string, dst *string) {
		if v := getenv("ARGUS_OAUTH_" + up + "_" + suffix); v != "" {
			*dst = v
		}
	}
	set("CLIENT_ID", &cfg.ClientID)
	set("CLIENT_SECRET", &cfg.ClientSecret)
	set("AUTH_URL", &cfg.AuthorizationEndpoint)
	set("TOKEN_URL", &cfg.TokenEndpoint)
	set("DEVICE_URL", &cfg.DeviceAuthorizationEndpoint)
	set("REDIRECT_HOST", &cfg.RedirectHost)
	if v := getenv("ARGUS_OAUTH_" + up + "_SCOPES"); v != "" {
		cfg.Scopes = strings.Fields(v)
	}
	if v := getenv("ARGUS_OAUTH_" + up + "_REDIRECT_PORT"); v != "" {
		// An invalid (non-integer) value is ignored — the preset's default
		// port stays in effect — rather than propagating a bad port number
		// into the loopback listener bind.
		if port, err := strconv.Atoi(v); err == nil {
			cfg.RedirectPort = port
		}
	}
}
