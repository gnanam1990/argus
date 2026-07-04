// Package model defines the provider-neutral seam the agent loop talks to. It
// models a conversation, a single provider Step, and the token accounting —
// all in vendor-free types. Concrete adapters (Anthropic, OpenAI, Gemini, a
// local OpenAI-compatible router) live under internal/provider and translate
// these types to and from their SDKs. The loop imports this package only; it
// never imports a vendor SDK.
package model

import (
	"context"

	"github.com/gnanam1990/argus/pkg/action"
)

// Capabilities describes what a provider can do, so the loop can adapt (e.g.
// skip the set-of-marks grounder when the provider grounds natively).
type Capabilities struct {
	// NativeComputerUse is true when the provider exposes a first-class
	// computer-use tool and consumes raw screenshots directly.
	NativeComputerUse bool
	// Streaming is true when Step streams and accumulates the response.
	Streaming bool
	// Grounding is true when the provider itself resolves click targets
	// (implements Clicker), so an external grounder is optional.
	Grounding bool
	// Vision is true when the provider accepts image input.
	Vision bool
	// MaxImages caps how many screenshots may be retained in one request; 0
	// means unspecified/unlimited.
	MaxImages int
}

// Usage is the token accounting for one or more provider calls. It feeds the
// budget middleware and the end-of-run cost summary.
type Usage struct {
	InputTokens      int `json:"input_tokens"`
	OutputTokens     int `json:"output_tokens"`
	CacheReadTokens  int `json:"cache_read_tokens,omitempty"`
	CacheWriteTokens int `json:"cache_write_tokens,omitempty"`
}

// Add returns the component-wise sum, used to accumulate usage across a run.
func (u Usage) Add(o Usage) Usage {
	return Usage{
		InputTokens:      u.InputTokens + o.InputTokens,
		OutputTokens:     u.OutputTokens + o.OutputTokens,
		CacheReadTokens:  u.CacheReadTokens + o.CacheReadTokens,
		CacheWriteTokens: u.CacheWriteTokens + o.CacheWriteTokens,
	}
}

// Total returns the billable input+output token count.
func (u Usage) Total() int { return u.InputTokens + u.OutputTokens }

// StepConfig holds the per-call sampling settings resolved from StepOptions.
// Pointer fields distinguish "unset" (use the provider default) from an
// explicit zero value.
type StepConfig struct {
	Temperature *float64
	MaxTokens   int
	Seed        *int
}

// StepOption configures a single provider Step.
type StepOption func(*StepConfig)

// WithTemperature sets the sampling temperature.
func WithTemperature(t float64) StepOption {
	return func(c *StepConfig) { c.Temperature = &t }
}

// WithMaxTokens caps the response length.
func WithMaxTokens(n int) StepOption {
	return func(c *StepConfig) { c.MaxTokens = n }
}

// WithSeed requests deterministic sampling where the provider supports it.
func WithSeed(s int) StepOption {
	return func(c *StepConfig) { c.Seed = &s }
}

// ApplyOptions resolves a StepConfig from the given options.
func ApplyOptions(opts ...StepOption) StepConfig {
	var c StepConfig
	for _, opt := range opts {
		opt(&c)
	}
	return c
}

// Provider is the single model seam the agent loop depends on. Step advances
// the conversation by one assistant turn (which may request actions).
type Provider interface {
	Step(ctx context.Context, conv *Conversation, opts ...StepOption) (*Turn, error)
	Capabilities() Capabilities
}

// Clicker is an optional capability, discovered by type-assertion, that maps a
// natural-language target on a screenshot to a click point. It enables a
// composed planner-VLM + dedicated grounding-model configuration.
type Clicker interface {
	PredictClick(ctx context.Context, img action.Image, instruction string) (point action.Point, ok bool, err error)
}
