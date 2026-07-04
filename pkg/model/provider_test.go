package model

import "testing"

func TestUsageAddTotal(t *testing.T) {
	t.Parallel()
	a := Usage{InputTokens: 100, OutputTokens: 20, CacheReadTokens: 5}
	b := Usage{InputTokens: 50, OutputTokens: 10, CacheWriteTokens: 7}
	sum := a.Add(b)
	want := Usage{InputTokens: 150, OutputTokens: 30, CacheReadTokens: 5, CacheWriteTokens: 7}
	if sum != want {
		t.Errorf("Add = %+v, want %+v", sum, want)
	}
	if got := sum.Total(); got != 180 {
		t.Errorf("Total = %d, want 180", got)
	}
}

func TestApplyOptions(t *testing.T) {
	t.Parallel()

	t.Run("defaults are unset", func(t *testing.T) {
		t.Parallel()
		c := ApplyOptions()
		if c.Temperature != nil || c.Seed != nil || c.MaxTokens != 0 {
			t.Errorf("expected zero StepConfig, got %+v", c)
		}
	})

	t.Run("options apply", func(t *testing.T) {
		t.Parallel()
		c := ApplyOptions(WithTemperature(0.7), WithMaxTokens(2048), WithSeed(42))
		if c.Temperature == nil || *c.Temperature != 0.7 {
			t.Errorf("Temperature = %v, want 0.7", c.Temperature)
		}
		if c.MaxTokens != 2048 {
			t.Errorf("MaxTokens = %d, want 2048", c.MaxTokens)
		}
		if c.Seed == nil || *c.Seed != 42 {
			t.Errorf("Seed = %v, want 42", c.Seed)
		}
	})

	t.Run("explicit zero temperature differs from unset", func(t *testing.T) {
		t.Parallel()
		c := ApplyOptions(WithTemperature(0))
		if c.Temperature == nil {
			t.Fatal("Temperature should be set (non-nil) even at zero")
		}
		if *c.Temperature != 0 {
			t.Errorf("Temperature = %v, want 0", *c.Temperature)
		}
	})
}
