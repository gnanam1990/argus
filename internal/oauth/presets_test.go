package oauth

import "testing"

func TestPresetsAllowed(t *testing.T) {
	t.Parallel()
	if PresetsAllowed(func(string) string { return "" }) {
		t.Error("presets should be disabled by default")
	}
	if !PresetsAllowed(func(k string) string {
		if k == "ARGUS_OAUTH_ALLOW_PRESETS" {
			return "1"
		}
		return ""
	}) {
		t.Error("presets should be enabled with =1")
	}
}

func TestPresetChatGPT(t *testing.T) {
	t.Parallel()
	cfg, ok := Preset("chatgpt", func(string) string { return "" })
	if !ok {
		t.Fatal("chatgpt preset missing")
	}
	if cfg.ClientID != "app_EMoamEEZ73f0CkXaXp7hrann" {
		t.Errorf("client id = %q", cfg.ClientID)
	}
	if cfg.RedirectPort != 1455 || cfg.RedirectPath != "/auth/callback" {
		t.Errorf("redirect = %d %q", cfg.RedirectPort, cfg.RedirectPath)
	}
	// The Codex client registers "localhost"; sending 127.0.0.1 is rejected by
	// the authorize endpoint (authorize_hydra_invalid_request).
	if cfg.RedirectHost != "localhost" {
		t.Errorf("redirect host = %q, want localhost", cfg.RedirectHost)
	}
	if cfg.ExtraAuthParams["id_token_add_organizations"] != "true" {
		t.Errorf("extra params = %v", cfg.ExtraAuthParams)
	}
}

func TestPresetChatGPTRedirectHostOverride(t *testing.T) {
	t.Parallel()
	cfg, _ := Preset("chatgpt", func(k string) string {
		if k == "ARGUS_OAUTH_CHATGPT_REDIRECT_HOST" {
			return "127.0.0.1"
		}
		return ""
	})
	if cfg.RedirectHost != "127.0.0.1" {
		t.Errorf("redirect host override = %q", cfg.RedirectHost)
	}
}

func TestPresetRedirectPortOverride(t *testing.T) {
	t.Parallel()
	cfg, _ := Preset("chatgpt", func(k string) string {
		if k == "ARGUS_OAUTH_CHATGPT_REDIRECT_PORT" {
			return "9999"
		}
		return ""
	})
	if cfg.RedirectPort != 9999 {
		t.Errorf("redirect port override = %d, want 9999", cfg.RedirectPort)
	}
}

func TestPresetRedirectPortInvalidIgnored(t *testing.T) {
	t.Parallel()
	cfg, _ := Preset("chatgpt", func(k string) string {
		if k == "ARGUS_OAUTH_CHATGPT_REDIRECT_PORT" {
			return "not-a-port"
		}
		return ""
	})
	if cfg.RedirectPort != 1455 {
		t.Errorf("invalid override should be ignored (keep preset default 1455); got %d", cfg.RedirectPort)
	}
}

func TestPresetXAI(t *testing.T) {
	t.Parallel()
	cfg, ok := Preset("xai", func(string) string { return "" })
	if !ok || cfg.TokenEndpoint != "https://auth.x.ai/oauth2/token" || cfg.DeviceAuthorizationEndpoint == "" {
		t.Errorf("xai preset = %+v, ok=%v", cfg, ok)
	}
}

func TestPresetEnvOverride(t *testing.T) {
	t.Parallel()
	env := map[string]string{
		"ARGUS_OAUTH_XAI_CLIENT_ID": "my-client",
		"ARGUS_OAUTH_XAI_TOKEN_URL": "https://proxy.local/token",
		"ARGUS_OAUTH_XAI_SCOPES":    "openid custom",
	}
	cfg, _ := Preset("xai", func(k string) string { return env[k] })
	if cfg.ClientID != "my-client" || cfg.TokenEndpoint != "https://proxy.local/token" {
		t.Errorf("override not applied: %+v", cfg)
	}
	if len(cfg.Scopes) != 2 || cfg.Scopes[1] != "custom" {
		t.Errorf("scopes override = %v", cfg.Scopes)
	}
}

func TestPresetUnknown(t *testing.T) {
	t.Parallel()
	if _, ok := Preset("gemini", func(string) string { return "" }); ok {
		t.Error("unknown preset should not resolve")
	}
}
