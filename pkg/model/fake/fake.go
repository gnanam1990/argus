// Package fake provides a scriptable model.Provider for deterministic tests.
// It plays back a pre-programmed sequence of turns (and injected errors),
// records the conversation snapshot passed to each Step, and reports
// configurable capabilities — enough to drive the agent loop end-to-end with
// no network.
package fake

import (
	"context"
	"errors"
	"sync"

	"github.com/gnanam1990/argus/pkg/model"
)

// ErrExhausted is returned by Step once the scripted sequence is consumed.
var ErrExhausted = errors.New("fake: no more scripted steps")

type step struct {
	turn *model.Turn
	err  error
}

// Provider is a deterministic, concurrency-safe fake implementation of
// model.Provider. The zero value is not usable; construct with New.
type Provider struct {
	mu     sync.Mutex
	caps   model.Capabilities
	script []step
	idx    int
	calls  []*model.Conversation
	opts   []model.StepConfig
}

// Compile-time assertion that the fake satisfies the seam.
var _ model.Provider = (*Provider)(nil)

// New builds a fake that returns the given turns, in order, on successive Step
// calls. Default capabilities advertise native computer use, streaming, and
// vision; override with WithCapabilities.
func New(turns ...*model.Turn) *Provider {
	p := &Provider{
		caps: model.Capabilities{
			NativeComputerUse: true,
			Streaming:         true,
			Vision:            true,
		},
	}
	for _, t := range turns {
		p.script = append(p.script, step{turn: t})
	}
	return p
}

// WithCapabilities overrides the reported capabilities and returns the
// provider for chaining.
func (p *Provider) WithCapabilities(c model.Capabilities) *Provider {
	p.caps = c
	return p
}

// Then appends a successful turn to the script and returns the provider.
func (p *Provider) Then(t *model.Turn) *Provider {
	p.script = append(p.script, step{turn: t})
	return p
}

// ThenError appends an error to the script (returned in sequence) and returns
// the provider.
func (p *Provider) ThenError(err error) *Provider {
	p.script = append(p.script, step{err: err})
	return p
}

// Step records a snapshot of conv and the resolved options, then returns the
// next scripted result. Once the script is exhausted it returns ErrExhausted.
func (p *Provider) Step(_ context.Context, conv *model.Conversation, opts ...model.StepOption) (*model.Turn, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.calls = append(p.calls, conv.Clone())
	p.opts = append(p.opts, model.ApplyOptions(opts...))

	if p.idx >= len(p.script) {
		return nil, ErrExhausted
	}
	s := p.script[p.idx]
	p.idx++
	return s.turn, s.err
}

// Capabilities reports the configured capabilities.
func (p *Provider) Capabilities() model.Capabilities {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.caps
}

// Calls returns the recorded conversation snapshots, one per Step invocation
// (including calls made after the script was exhausted).
func (p *Provider) Calls() []*model.Conversation {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]*model.Conversation, len(p.calls))
	copy(out, p.calls)
	return out
}

// StepCount returns how many times Step has been invoked.
func (p *Provider) StepCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.calls)
}

// LastOptions returns the resolved StepConfig from the most recent Step, and
// false if Step has not been called.
func (p *Provider) LastOptions() (model.StepConfig, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.opts) == 0 {
		return model.StepConfig{}, false
	}
	return p.opts[len(p.opts)-1], true
}
