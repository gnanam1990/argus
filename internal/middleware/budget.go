// Package middleware provides the cross-cutting agent.Middleware
// implementations that wrap the agent loop: budget enforcement, human-in-the-
// loop approval, prompt-injection defense, secret redaction, image retention,
// and telemetry. Each embeds agent.Base and overrides only the hooks it needs.
package middleware

import (
	"context"
	"sync"

	"github.com/gnanam1990/argus/internal/pricing"
	"github.com/gnanam1990/argus/pkg/agent"
	"github.com/gnanam1990/argus/pkg/model"
)

// Budget halts a run when it exceeds a token and/or USD ceiling. It accumulates
// usage as the run progresses and stops it at the next continuation checkpoint.
type Budget struct {
	agent.Base
	mu        sync.Mutex
	maxTokens int
	maxUSD    float64
	modelID   string
	usage     model.Usage
	cost      float64
}

// BudgetOption configures a Budget.
type BudgetOption func(*Budget)

// WithTokenBudget caps cumulative input+output tokens (0 = no token cap).
func WithTokenBudget(n int) BudgetOption { return func(b *Budget) { b.maxTokens = n } }

// WithUSDBudget caps cumulative cost in USD, priced with modelID's rate
// (0 = no USD cap). If the model has no known rate, the USD cap is inactive.
func WithUSDBudget(modelID string, usd float64) BudgetOption {
	return func(b *Budget) { b.modelID, b.maxUSD = modelID, usd }
}

// NewBudget builds a budget middleware.
func NewBudget(opts ...BudgetOption) *Budget {
	b := &Budget{}
	for _, opt := range opts {
		opt(b)
	}
	return b
}

// OnUsage accumulates token usage (and USD cost when a model rate is set).
func (b *Budget) OnUsage(_ context.Context, u model.Usage) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.usage = b.usage.Add(u)
	if b.modelID != "" {
		if c, ok := pricing.Cost(b.modelID, u); ok {
			b.cost += c
		}
	}
	return nil
}

// OnRunContinue stops the run once a configured ceiling is reached.
func (b *Budget) OnRunContinue(_ context.Context, _ *agent.State) (bool, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.maxTokens > 0 && b.usage.Total() >= b.maxTokens {
		return false, nil
	}
	if b.maxUSD > 0 && b.cost >= b.maxUSD {
		return false, nil
	}
	return true, nil
}

// Usage returns the accumulated usage.
func (b *Budget) Usage() model.Usage {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.usage
}

// Cost returns the accumulated USD cost (0 if no model rate was configured).
func (b *Budget) Cost() float64 {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.cost
}
