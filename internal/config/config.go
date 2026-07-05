// Package config is the layered configuration for the argus CLI: defaults, then
// an optional JSON file, then environment overrides (flags are applied by the
// CLI on top). Secrets are never part of the config — API keys are read from
// the environment at wire time — and a stable Hash feeds run provenance.
package config

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"strconv"

	"github.com/gnanam1990/argus/internal/pricing"
	"github.com/gnanam1990/argus/pkg/action"
)

// Config is the full agent configuration (secrets excluded).
type Config struct {
	Provider  Provider  `json:"provider"`
	Agent     Agent     `json:"agent"`
	Grounding Grounding `json:"grounding"`
	Sandbox   Sandbox   `json:"sandbox"`
}

// Provider selects and configures the model adapter.
type Provider struct {
	Kind          string   `json:"kind"` // anthropic | openai | compat
	Model         string   `json:"model"`
	BaseURL       string   `json:"base_url,omitempty"`
	MaxTokens     int      `json:"max_tokens"`
	DisplayWidth  int      `json:"display_width"`
	DisplayHeight int      `json:"display_height"`
	Temperature   *float64 `json:"temperature,omitempty"`
	Seed          *int     `json:"seed,omitempty"`
}

// Agent configures the loop and middleware.
type Agent struct {
	System            string   `json:"system,omitempty"`
	MaxSteps          int      `json:"max_steps"`
	ScreenshotDelayMS int      `json:"screenshot_delay_ms"`
	ScreenshotMaxEdge int      `json:"screenshot_max_edge"` // cap the long edge of screenshots sent to the model (0 = full resolution)
	BudgetTokens      int      `json:"budget_tokens"`
	BudgetUSD         float64  `json:"budget_usd"`
	Capabilities      []string `json:"capabilities,omitempty"`
	RequireApproval   bool     `json:"require_approval"`
	RetainImages      int      `json:"retain_images"`
	Dispatch          string   `json:"dispatch,omitempty"` // cursor (default) | background (macOS accessibility press, no pointer move)
	Skills            []string `json:"skills,omitempty"`   // guidance skills prepended to the system prompt (see "argus skills")
}

// Grounding configures the set-of-marks detector.
type Grounding struct {
	Mode          string  `json:"mode"` // none | omniparser | ax | chain
	OmniParserURL string  `json:"omniparser_url,omitempty"`
	MinConfidence float64 `json:"min_confidence"`
}

// Sandbox selects the environment provider.
type Sandbox struct {
	Kind      string `json:"kind"` // host | docker
	Image     string `json:"image,omitempty"`
	HostPort  int    `json:"host_port"`
	GuestPort int    `json:"guest_port"`
}

// Defaults returns the baseline configuration.
func Defaults() Config {
	return Config{
		Provider:  Provider{Kind: "anthropic", Model: "claude-opus-4-8", MaxTokens: 4096, DisplayWidth: 1280, DisplayHeight: 800},
		Agent:     Agent{MaxSteps: 40, ScreenshotDelayMS: 300, RetainImages: 3},
		Grounding: Grounding{Mode: "none", MinConfidence: 0.3},
		Sandbox:   Sandbox{Kind: "host", Image: "argus-guest:latest", HostPort: 7180, GuestPort: 7180},
	}
}

// Load builds a config from defaults, an optional JSON file, and the
// environment. Unknown JSON fields are rejected so a typo'd key (e.g. a
// misspelled require_approval) cannot silently disable a safety setting.
func Load(path string) (Config, error) {
	c := Defaults()
	if path != "" {
		b, err := os.ReadFile(path)
		if err != nil {
			return c, fmt.Errorf("config: read %s: %w", path, err)
		}
		dec := json.NewDecoder(bytes.NewReader(b))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&c); err != nil {
			return c, fmt.Errorf("config: parse %s: %w", path, err)
		}
	}
	if err := applyEnv(&c, os.Getenv); err != nil {
		return c, err
	}
	return c, nil
}

// applyEnv overlays ARGUS_* environment overrides. Malformed values are
// errors, not silently ignored.
func applyEnv(c *Config, getenv func(string) string) error {
	if v := getenv("ARGUS_PROVIDER"); v != "" {
		c.Provider.Kind = v
	}
	if v := getenv("ARGUS_MODEL"); v != "" {
		c.Provider.Model = v
	}
	if v := getenv("ARGUS_BASE_URL"); v != "" {
		c.Provider.BaseURL = v
	}
	if v := getenv("ARGUS_GROUNDING"); v != "" {
		c.Grounding.Mode = v
	}
	if v := getenv("ARGUS_OMNIPARSER_URL"); v != "" {
		c.Grounding.OmniParserURL = v
	}
	if v := getenv("ARGUS_SANDBOX"); v != "" {
		c.Sandbox.Kind = v
	}
	if v := getenv("ARGUS_MAX_STEPS"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			return fmt.Errorf("config: ARGUS_MAX_STEPS %q is not an integer", v)
		}
		c.Agent.MaxSteps = n
	}
	return nil
}

var (
	// Provider kinds: anthropic (native), the OpenAI-compatible adapters
	// (openai, kimi, xai, gemini, ollama, compat), and chatgpt (OAuth Codex
	// backend).
	providerKinds  = map[string]bool{"anthropic": true, "openai": true, "compat": true, "kimi": true, "xai": true, "gemini": true, "ollama": true, "chatgpt": true}
	groundingModes = map[string]bool{"none": true, "omniparser": true, "ax": true, "chain": true}
	sandboxKinds   = map[string]bool{"host": true, "docker": true}
	gatedCaps      = map[string]bool{"run_command": true, "read_file": true, "write_file": true, "window_focus": true, "window_move": true}
)

// Validate checks the configuration for consistency.
func (c Config) Validate() error {
	if !providerKinds[c.Provider.Kind] {
		return fmt.Errorf("config: unknown provider kind %q", c.Provider.Kind)
	}
	if c.Provider.Model == "" {
		return fmt.Errorf("config: provider.model is required")
	}
	if c.Provider.MaxTokens <= 0 {
		return fmt.Errorf("config: provider.max_tokens must be positive")
	}
	if c.Provider.DisplayWidth <= 0 || c.Provider.DisplayHeight <= 0 {
		return fmt.Errorf("config: provider display dimensions must be positive")
	}
	if c.Agent.MaxSteps < 0 {
		return fmt.Errorf("config: agent.max_steps must be non-negative")
	}
	if c.Agent.ScreenshotDelayMS < 0 {
		return fmt.Errorf("config: agent.screenshot_delay_ms must be non-negative")
	}
	if c.Agent.ScreenshotMaxEdge != 0 && c.Agent.ScreenshotMaxEdge < 480 {
		return fmt.Errorf("config: agent.screenshot_max_edge must be 0 (off) or >= 480")
	}
	if c.Agent.Dispatch != "" && c.Agent.Dispatch != "cursor" && c.Agent.Dispatch != "background" {
		return fmt.Errorf("config: agent.dispatch must be \"cursor\" or \"background\", got %q", c.Agent.Dispatch)
	}
	if !groundingModes[c.Grounding.Mode] {
		return fmt.Errorf("config: unknown grounding mode %q", c.Grounding.Mode)
	}
	if (c.Grounding.Mode == "omniparser" || c.Grounding.Mode == "chain") && c.Grounding.OmniParserURL == "" {
		return fmt.Errorf("config: grounding mode %q requires omniparser_url", c.Grounding.Mode)
	}
	if !sandboxKinds[c.Sandbox.Kind] {
		return fmt.Errorf("config: unknown sandbox kind %q", c.Sandbox.Kind)
	}
	if c.Agent.BudgetTokens < 0 || c.Agent.BudgetUSD < 0 {
		return fmt.Errorf("config: budgets must be non-negative")
	}
	if c.Agent.BudgetUSD > 0 {
		// A USD budget silently enforces nothing for a model without a pinned
		// rate — reject up front rather than run uncapped.
		if _, ok := pricing.Lookup(c.Provider.Model); !ok {
			return fmt.Errorf("config: budget_usd is set but model %q has no pinned pricing; use budget_tokens instead", c.Provider.Model)
		}
	}
	for _, cap := range c.Agent.Capabilities {
		if !gatedCaps[cap] {
			return fmt.Errorf("config: unknown capability %q", cap)
		}
	}
	return nil
}

// Capabilities resolves the configured gated capability names to action types.
func (c Config) Capabilities() []action.ActionType {
	m := map[string]action.ActionType{
		"run_command": action.RunCommand, "read_file": action.ReadFile,
		"write_file": action.WriteFile, "window_focus": action.WindowFocus, "window_move": action.WindowMove,
	}
	var out []action.ActionType
	for _, name := range c.Agent.Capabilities {
		if t, ok := m[name]; ok {
			out = append(out, t)
		}
	}
	return out
}

// Hash returns a stable content hash of the config, for run provenance.
func (c Config) Hash() string {
	b, err := json.Marshal(c)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(b)
	return fmt.Sprintf("%x", sum[:8])
}
