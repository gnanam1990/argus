package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A typo'd key must fail loudly, not silently disable a safety setting.
func TestLoadRejectsUnknownFields(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "typo.json")
	body := `{"agent": {"require_approvals": true}}`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := Load(path)
	if err == nil {
		t.Fatal("unknown field should be rejected")
	}
	if !strings.Contains(err.Error(), "require_approvals") {
		t.Errorf("error should name the offending key: %v", err)
	}
}

func TestApplyEnvRejectsMalformedInt(t *testing.T) {
	t.Parallel()
	c := Defaults()
	err := applyEnv(&c, func(k string) string {
		if k == "ARGUS_MAX_STEPS" {
			return "abc"
		}
		return ""
	})
	if err == nil {
		t.Fatal("malformed ARGUS_MAX_STEPS should be an error")
	}
}

func TestValidateDisplayAndSteps(t *testing.T) {
	t.Parallel()
	c := Defaults()
	c.Provider.DisplayWidth = 0
	if err := c.Validate(); err == nil {
		t.Error("zero display width should be rejected")
	}

	c = Defaults()
	c.Agent.MaxSteps = -1
	if err := c.Validate(); err == nil {
		t.Error("negative max_steps should be rejected")
	}
}

func TestValidateScreenshotKnobs(t *testing.T) {
	t.Parallel()
	c := Defaults()
	c.Agent.ScreenshotMaxEdge = 200 // below the 480 floor
	if err := c.Validate(); err == nil {
		t.Error("a too-small screenshot_max_edge should be rejected")
	}
	c.Agent.ScreenshotMaxEdge = 1400 // a sane cap
	if err := c.Validate(); err != nil {
		t.Errorf("1400 should be valid: %v", err)
	}
	c = Defaults()
	c.Agent.ScreenshotDelayMS = -1
	if err := c.Validate(); err == nil {
		t.Error("negative screenshot_delay_ms should be rejected")
	}
}

func TestValidateDispatch(t *testing.T) {
	t.Parallel()
	c := Defaults()
	c.Agent.Dispatch = "background"
	if err := c.Validate(); err != nil {
		t.Errorf("background dispatch should be valid: %v", err)
	}
	c.Agent.Dispatch = "teleport"
	if err := c.Validate(); err == nil {
		t.Error("an unknown dispatch mode should be rejected")
	}
}

// A USD budget on a model with no pinned rate enforces nothing — reject it up
// front instead of silently running uncapped.
func TestValidateBudgetUSDNeedsPricing(t *testing.T) {
	t.Parallel()
	c := Defaults()
	c.Provider.Kind = "ollama"
	c.Provider.Model = "qwen2.5vl" // not in the pinned rates table
	c.Agent.BudgetUSD = 5
	if err := c.Validate(); err == nil {
		t.Error("budget_usd with unpriced model should be rejected")
	}

	c.Agent.BudgetUSD = 0
	c.Agent.BudgetTokens = 100000
	if err := c.Validate(); err != nil {
		t.Errorf("token budget must stay valid for unpriced models: %v", err)
	}
}
