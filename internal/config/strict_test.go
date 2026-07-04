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
