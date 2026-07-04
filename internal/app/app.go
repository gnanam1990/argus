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
	"github.com/gnanam1990/argus/internal/oauth"
	"github.com/gnanam1990/argus/internal/provider/anthropic"
	"github.com/gnanam1990/argus/internal/provider/codex"
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

// compatPreset is an OpenAI-compatible endpoint preset: a default base URL and
// the environment variable its API key is read from.
type compatPreset struct {
	baseURL string // default; overridden by config base_url
	keyEnv  string
}

// compatPresets are the OpenAI-compatible providers. Kimi (Moonshot), xAI
// (Grok), Gemini, and Ollama all speak the OpenAI Chat Completions API, so
// they share the compat adapter — only the endpoint and key env differ. All
// are overridable via base_url.
var compatPresets = map[string]compatPreset{
	"openai": {"https://api.openai.com/v1", "OPENAI_API_KEY"},
	"kimi":   {"https://api.moonshot.ai/v1", "MOONSHOT_API_KEY"},
	"xai":    {"https://api.x.ai/v1", "XAI_API_KEY"},
	"gemini": {"https://generativelanguage.googleapis.com/v1beta/openai", "GEMINI_API_KEY"},
	"ollama": {"http://localhost:11434/v1", "OLLAMA_API_KEY"},
	"compat": {"", "ARGUS_API_KEY"},
}

// APIKeyEnv returns the environment variable a provider kind reads its key from.
func APIKeyEnv(kind string) string {
	if kind == "anthropic" {
		return "ANTHROPIC_API_KEY"
	}
	if p, ok := compatPresets[kind]; ok {
		return p.keyEnv
	}
	return "ARGUS_API_KEY"
}

// BuildProviderWithAuth constructs the model adapter, wiring the OAuth Manager
// for subscription-login providers (chatgpt via the Codex backend, xai via an
// OAuth Bearer over the compat adapter). API-key providers delegate to
// BuildProvider. For xai OAuth, resolving the token may refresh (network);
// chatgpt fetches its token lazily per request.
func BuildProviderWithAuth(ctx context.Context, cfg config.Config, getenv func(string) string, mgr *oauth.Manager) (model.Provider, error) {
	switch cfg.Provider.Kind {
	case "chatgpt":
		if mgr == nil {
			return nil, fmt.Errorf("app: chatgpt requires an OAuth login (run: argus auth login chatgpt)")
		}
		tokenFn := chatgptTokenSource(mgr, false)
		forceFn := chatgptTokenSource(mgr, true)
		model := cfg.Provider.Model
		if model == "" {
			model = "gpt-5.5"
		}
		opts := []codex.Option{
			codex.WithModel(model),
			codex.WithTokenSource(tokenFn),
			codex.WithForceRefresh(forceFn),
			codex.WithImageRetention(cfg.Agent.RetainImages),
		}
		if cfg.Provider.BaseURL != "" {
			opts = append(opts, codex.WithBaseURL(cfg.Provider.BaseURL))
		}
		return codex.New(opts...), nil

	case "xai":
		// Prefer an API key; fall back to an OAuth Bearer from the Manager.
		// Without either, fail fast with the remedy instead of building an
		// unauthenticated client that 401s mid-run.
		if getenv("XAI_API_KEY") == "" && mgr != nil {
			tok, err := mgr.GetFresh(ctx, "xai")
			if err != nil {
				return nil, fmt.Errorf("app: no usable xai credentials (%w); set XAI_API_KEY or run \"argus auth login xai\"", err)
			}
			return compat.New(compat.WithBaseURL("https://api.x.ai/v1"),
				compat.WithAPIKey(tok), compat.WithModel(cfg.Provider.Model), compat.WithMaxTokens(cfg.Provider.MaxTokens),
				compat.WithImageRetention(cfg.Agent.RetainImages)), nil
		}
	}
	return BuildProvider(cfg, getenv)
}

// chatgptTokenSource adapts the Manager to a codex.TokenSource, deriving the
// account id from the id_token when the stored token lacks it.
func chatgptTokenSource(mgr *oauth.Manager, force bool) codex.TokenSource {
	return func(ctx context.Context) (string, string, error) {
		var tok oauth.Token
		var err error
		if force {
			tok, err = mgr.ForceRefresh(ctx, "chatgpt")
		} else {
			tok, err = mgr.GetToken(ctx, "chatgpt")
		}
		if err != nil {
			return "", "", err
		}
		acct := tok.Account
		if acct == "" && tok.IDToken != "" {
			acct, _ = codex.ExtractChatGPTAccountID(tok.IDToken)
		}
		return tok.AccessToken, acct, nil
	}
}

// BuildProvider constructs the model adapter. Secrets come from getenv; no
// network call happens at construction.
func BuildProvider(cfg config.Config, getenv func(string) string) (model.Provider, error) {
	p := cfg.Provider
	key := getenv(APIKeyEnv(p.Kind))

	if p.Kind == "anthropic" {
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
			anthropic.WithImageRetention(cfg.Agent.RetainImages),
		), nil
	}

	preset, ok := compatPresets[p.Kind]
	if !ok {
		return nil, fmt.Errorf("app: unknown provider %q", p.Kind)
	}
	base := p.BaseURL
	if base == "" {
		base = preset.baseURL
	}
	if base == "" {
		return nil, fmt.Errorf("app: %s provider requires base_url", p.Kind)
	}
	return compat.New(
		compat.WithBaseURL(base),
		compat.WithAPIKey(key),
		compat.WithModel(p.Model),
		compat.WithMaxTokens(p.MaxTokens),
		compat.WithImageRetention(cfg.Agent.RetainImages),
	), nil
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
	// Injection-guard strictness pairs with approval: when a human approval
	// gate exists it runs report-only and the human decides; unattended runs
	// fail closed (sensitive untrusted actions are denied outright).
	mw = append(mw, middleware.NewInjectionGuard(!cfg.Agent.RequireApproval))
	if cfg.Agent.RequireApproval {
		// A nil approver (headless/eval contexts) fails closed inside
		// NewApproval: requiring approval with nobody to ask denies risky
		// actions rather than silently dropping the gate.
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
// The returned computer also exposes the sandbox's gated exec/file operations
// through the pkg/computer optional interfaces, so allowlisted run_command /
// read_file actions can actually execute. The returned cleanup stops the
// sandbox.
func BuildComputer(ctx context.Context, cfg config.Config, getenv func(string) string) (computer.Computer, func() error, error) {
	switch cfg.Sandbox.Kind {
	case "host":
		sb, err := host.New(hostDriver()).Provision(ctx, sandbox.Spec{})
		if err != nil {
			return nil, nil, err
		}
		return newSandboxComputer(sb), func() error { return sb.Stop(context.Background()) }, nil
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
		return newSandboxComputer(sb), func() error { return sb.Stop(context.Background()) }, nil
	default:
		return nil, nil, fmt.Errorf("app: unknown sandbox %q", cfg.Sandbox.Kind)
	}
}

// sandboxComputer bridges a sandbox's gated exec/file operations onto its
// computer, satisfying the pkg/computer Commander/FileReader/FileWriter
// optional interfaces the executor dispatches gated actions through.
type sandboxComputer struct {
	computer.Computer
	sb sandbox.Sandbox
}

func newSandboxComputer(sb sandbox.Sandbox) sandboxComputer {
	return sandboxComputer{Computer: sb.Computer(), sb: sb}
}

// RunCommand implements computer.Commander over the sandbox's Exec.
func (s sandboxComputer) RunCommand(ctx context.Context, cmd string) (string, error) {
	res, err := s.sb.Exec(ctx, cmd, 0)
	if err != nil {
		return "", err
	}
	out := res.Stdout
	if res.Stderr != "" {
		if out != "" {
			out += "\n"
		}
		out += res.Stderr
	}
	if res.ExitCode != 0 {
		out = fmt.Sprintf("%s\n[exit code %d]", out, res.ExitCode)
	}
	return out, nil
}

// ReadFile implements computer.FileReader over the sandbox.
func (s sandboxComputer) ReadFile(ctx context.Context, path string) ([]byte, error) {
	return s.sb.ReadFile(ctx, path)
}

// WriteFile implements computer.FileWriter over the sandbox.
func (s sandboxComputer) WriteFile(ctx context.Context, path string, data []byte) error {
	return s.sb.WriteFile(ctx, path, data)
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
