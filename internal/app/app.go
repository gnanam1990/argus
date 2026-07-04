// Package app assembles a runnable agent from a Config: it builds the provider,
// computer, grounder, and middleware chain, and composes them into an
// agent.Runner. Keeping assembly here (rather than in cmd/argus) makes the
// wiring unit-testable without a process.
package app

import (
	"context"
	"fmt"
	"log/slog"

	sdkopt "github.com/anthropics/anthropic-sdk-go/option"

	"github.com/gnanam1990/argus/internal/config"
	"github.com/gnanam1990/argus/internal/grounder/ax"
	"github.com/gnanam1990/argus/internal/grounder/chain"
	"github.com/gnanam1990/argus/internal/grounder/omniparser"
	"github.com/gnanam1990/argus/internal/mark"
	"github.com/gnanam1990/argus/internal/middleware"
	"github.com/gnanam1990/argus/internal/provider/anthropic"
	"github.com/gnanam1990/argus/internal/provider/compat"
	"github.com/gnanam1990/argus/internal/sandbox/docker"
	"github.com/gnanam1990/argus/internal/sandbox/host"
	"github.com/gnanam1990/argus/pkg/agent"
	"github.com/gnanam1990/argus/pkg/computer"
	"github.com/gnanam1990/argus/pkg/grounder"
	"github.com/gnanam1990/argus/pkg/model"
	"github.com/gnanam1990/argus/pkg/sandbox"
	"github.com/gnanam1990/argus/pkg/trajectory"
)

// APIKeyEnv returns the environment variable a provider kind reads its key from.
func APIKeyEnv(kind string) string {
	switch kind {
	case "anthropic":
		return "ANTHROPIC_API_KEY"
	case "openai":
		return "OPENAI_API_KEY"
	default:
		return "ARGUS_API_KEY"
	}
}

// BuildProvider constructs the model adapter. Secrets come from getenv; no
// network call happens at construction.
func BuildProvider(cfg config.Config, getenv func(string) string) (model.Provider, error) {
	p := cfg.Provider
	key := getenv(APIKeyEnv(p.Kind))
	switch p.Kind {
	case "anthropic":
		copts := []sdkopt.RequestOption{}
		if key != "" {
			copts = append(copts, sdkopt.WithAPIKey(key))
		}
		if p.BaseURL != "" {
			copts = append(copts, sdkopt.WithBaseURL(p.BaseURL))
		}
		return anthropic.New(
			anthropic.WithModel(p.Model),
			anthropic.WithMaxTokens(p.MaxTokens),
			anthropic.WithDisplaySize(p.DisplayWidth, p.DisplayHeight),
			anthropic.WithClientOptions(copts...),
		), nil
	case "openai":
		base := p.BaseURL
		if base == "" {
			base = "https://api.openai.com/v1"
		}
		return compat.New(compat.WithBaseURL(base), compat.WithAPIKey(key), compat.WithModel(p.Model), compat.WithMaxTokens(p.MaxTokens)), nil
	case "compat":
		if p.BaseURL == "" {
			return nil, fmt.Errorf("app: compat provider requires base_url")
		}
		return compat.New(compat.WithBaseURL(p.BaseURL), compat.WithAPIKey(key), compat.WithModel(p.Model), compat.WithMaxTokens(p.MaxTokens)), nil
	default:
		return nil, fmt.Errorf("app: unknown provider %q", p.Kind)
	}
}

// BuildGrounder constructs the grounder and marker for the configured mode.
// Returns a nil grounder for mode "none".
func BuildGrounder(cfg config.Config) (grounder.Grounder, grounder.Marker) {
	marker := mark.Marker{}
	g := cfg.Grounding
	switch g.Mode {
	case "omniparser":
		return omniparser.New(g.OmniParserURL, omniparser.WithMinConfidence(g.MinConfidence)), marker
	case "ax":
		return ax.New(), marker
	case "chain":
		return chain.New(ax.New(), omniparser.New(g.OmniParserURL, omniparser.WithMinConfidence(g.MinConfidence))), marker
	default:
		return nil, marker
	}
}

// BuildMiddleware assembles the ordered middleware chain. secrets are masked in
// conversations; approver gates risky actions when approval is enabled.
func BuildMiddleware(cfg config.Config, secrets []string, log *slog.Logger, runID string, approver middleware.Approver) []agent.Middleware {
	mw := []agent.Middleware{middleware.NewTelemetry(log, runID)}
	if len(secrets) > 0 {
		mw = append(mw, middleware.NewRedaction(secrets...))
	}
	if cfg.Agent.RetainImages > 0 {
		mw = append(mw, middleware.NewImageRetention(cfg.Agent.RetainImages))
	}
	mw = append(mw, middleware.NewInjectionGuard(true))
	if cfg.Agent.RequireApproval && approver != nil {
		mw = append(mw, middleware.NewApproval(nil, approver))
	}
	if cfg.Agent.BudgetTokens > 0 || cfg.Agent.BudgetUSD > 0 {
		var opts []middleware.BudgetOption
		if cfg.Agent.BudgetTokens > 0 {
			opts = append(opts, middleware.WithTokenBudget(cfg.Agent.BudgetTokens))
		}
		if cfg.Agent.BudgetUSD > 0 {
			opts = append(opts, middleware.WithUSDBudget(cfg.Provider.Model, cfg.Agent.BudgetUSD))
		}
		mw = append(mw, middleware.NewBudget(opts...))
	}
	return mw
}

// BuildComputer provisions the computer for the configured sandbox. For "host"
// it returns the local shell driver; for "docker" it provisions a container.
// The returned cleanup stops the sandbox.
func BuildComputer(ctx context.Context, cfg config.Config, getenv func(string) string) (computer.Computer, func() error, error) {
	switch cfg.Sandbox.Kind {
	case "host":
		sb, err := host.New(hostDriver()).Provision(ctx, sandbox.Spec{})
		if err != nil {
			return nil, nil, err
		}
		return sb.Computer(), func() error { return sb.Stop(context.Background()) }, nil
	case "docker":
		opts := []docker.Option{
			docker.WithImage(cfg.Sandbox.Image),
			docker.WithPorts(cfg.Sandbox.HostPort, cfg.Sandbox.GuestPort),
		}
		if token := getenv("ARGUS_GUEST_TOKEN"); token != "" {
			opts = append(opts, docker.WithToken(token))
		}
		sb, err := docker.New(opts...).Provision(ctx, sandbox.Spec{Image: cfg.Sandbox.Image})
		if err != nil {
			return nil, nil, err
		}
		return sb.Computer(), func() error { return sb.Stop(context.Background()) }, nil
	default:
		return nil, nil, fmt.Errorf("app: unknown sandbox %q", cfg.Sandbox.Kind)
	}
}

// NewRunner composes a Runner from the built parts.
func NewRunner(cfg config.Config, prov model.Provider, comp computer.Computer, gr grounder.Grounder, marker grounder.Marker, rec trajectory.Recorder, mw []agent.Middleware) *agent.Runner {
	opts := []agent.Option{
		agent.WithSystemPrompt(cfg.Agent.System),
		agent.WithMaxSteps(cfg.Agent.MaxSteps),
		agent.WithTrajectory(rec),
		agent.WithMiddleware(mw...),
	}
	if gr != nil {
		opts = append(opts, agent.WithGrounder(gr, marker, cfg.Grounding.MinConfidence))
	}
	if caps := cfg.Capabilities(); len(caps) > 0 {
		opts = append(opts, agent.WithCapabilities(caps...))
	}
	if mopts := modelOptions(cfg); len(mopts) > 0 {
		opts = append(opts, agent.WithModelOptions(mopts...))
	}
	return agent.NewRunner(prov, comp, opts...)
}

func modelOptions(cfg config.Config) []model.StepOption {
	var out []model.StepOption
	if cfg.Provider.Temperature != nil {
		out = append(out, model.WithTemperature(*cfg.Provider.Temperature))
	}
	if cfg.Provider.Seed != nil {
		out = append(out, model.WithSeed(*cfg.Provider.Seed))
	}
	return out
}

// Manifest builds a trajectory manifest stamped with provenance from cfg.
func Manifest(cfg config.Config, task, gitSHA, startedAt string) trajectory.Manifest {
	m := trajectory.NewManifest(task)
	m.Model = cfg.Provider.Model
	m.ConfigHash = cfg.Hash()
	m.GitSHA = gitSHA
	m.StartedAt = startedAt
	m.Temperature = cfg.Provider.Temperature
	m.Seed = cfg.Provider.Seed
	return m
}

// Summary returns a human-readable description of the configured run.
func Summary(cfg config.Config) string {
	grounding := cfg.Grounding.Mode
	budget := "none"
	if cfg.Agent.BudgetTokens > 0 {
		budget = fmt.Sprintf("%d tokens", cfg.Agent.BudgetTokens)
	}
	if cfg.Agent.BudgetUSD > 0 {
		budget = fmt.Sprintf("$%.2f", cfg.Agent.BudgetUSD)
	}
	return fmt.Sprintf(
		"provider=%s model=%s sandbox=%s grounding=%s max-steps=%d approval=%v budget=%s config-hash=%s",
		cfg.Provider.Kind, cfg.Provider.Model, cfg.Sandbox.Kind, grounding,
		cfg.Agent.MaxSteps, cfg.Agent.RequireApproval, budget, cfg.Hash(),
	)
}
