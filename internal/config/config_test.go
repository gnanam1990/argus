package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/gnanam1990/argus/pkg/action"
)

func TestDefaultsValid(t *testing.T) {
	t.Parallel()
	if err := Defaults().Validate(); err != nil {
		t.Errorf("defaults invalid: %v", err)
	}
}

func TestLoadFileOverrides(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "argus.json")
	if err := os.WriteFile(path, []byte(`{"provider":{"kind":"compat","model":"gpt-4o","max_tokens":8000},"agent":{"max_steps":10}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if c.Provider.Kind != "compat" || c.Provider.Model != "gpt-4o" || c.Provider.MaxTokens != 8000 {
		t.Errorf("provider = %+v", c.Provider)
	}
	if c.Agent.MaxSteps != 10 {
		t.Errorf("max_steps = %d, want 10", c.Agent.MaxSteps)
	}
	// Unset fields keep defaults.
	if c.Grounding.Mode != "none" {
		t.Errorf("grounding mode = %q, want default none", c.Grounding.Mode)
	}
}

func TestLoadMissingFile(t *testing.T) {
	t.Parallel()
	if _, err := Load("/no/such/config.json"); err == nil {
		t.Error("expected error for missing file")
	}
}

func TestApplyEnv(t *testing.T) {
	t.Parallel()
	c := Defaults()
	env := map[string]string{
		"ARGUS_PROVIDER":  "openai",
		"ARGUS_MODEL":     "gpt-5",
		"ARGUS_GROUNDING": "ax",
		"ARGUS_MAX_STEPS": "7",
		"ARGUS_SANDBOX":   "docker",
	}
	applyEnv(&c, func(k string) string { return env[k] })
	if c.Provider.Kind != "openai" || c.Provider.Model != "gpt-5" {
		t.Errorf("provider = %+v", c.Provider)
	}
	if c.Grounding.Mode != "ax" || c.Agent.MaxSteps != 7 || c.Sandbox.Kind != "docker" {
		t.Errorf("overrides not applied: %+v", c)
	}
}

func TestValidate(t *testing.T) {
	t.Parallel()
	bad := []func(*Config){
		func(c *Config) { c.Provider.Kind = "bogus" },
		func(c *Config) { c.Provider.Model = "" },
		func(c *Config) { c.Provider.MaxTokens = 0 },
		func(c *Config) { c.Grounding.Mode = "bogus" },
		func(c *Config) { c.Grounding.Mode = "omniparser"; c.Grounding.OmniParserURL = "" },
		func(c *Config) { c.Sandbox.Kind = "bogus" },
		func(c *Config) { c.Agent.BudgetTokens = -1 },
		func(c *Config) { c.Agent.Capabilities = []string{"fly"} },
	}
	for i, mut := range bad {
		c := Defaults()
		mut(&c)
		if err := c.Validate(); err == nil {
			t.Errorf("case %d: expected validation error", i)
		}
	}
}

func TestCapabilities(t *testing.T) {
	t.Parallel()
	c := Defaults()
	c.Agent.Capabilities = []string{"run_command", "read_file"}
	caps := c.Capabilities()
	if len(caps) != 2 || caps[0] != action.RunCommand || caps[1] != action.ReadFile {
		t.Errorf("caps = %v", caps)
	}
}

func TestHashStableAndSensitive(t *testing.T) {
	t.Parallel()
	a := Defaults()
	b := Defaults()
	if a.Hash() != b.Hash() {
		t.Error("identical configs should hash equally")
	}
	if a.Hash() == "" {
		t.Error("hash should be non-empty")
	}
	b.Provider.Model = "different"
	if a.Hash() == b.Hash() {
		t.Error("different configs should hash differently")
	}
}
