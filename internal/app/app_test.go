package app_test

import (
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/gnanam1990/argus/internal/app"
	"github.com/gnanam1990/argus/internal/config"
	"github.com/gnanam1990/argus/internal/grounder/ax"
	"github.com/gnanam1990/argus/internal/grounder/chain"
	"github.com/gnanam1990/argus/internal/middleware"
	"github.com/gnanam1990/argus/internal/oauth"
	"github.com/gnanam1990/argus/pkg/action"
	compfake "github.com/gnanam1990/argus/pkg/computer/fake"
	"github.com/gnanam1990/argus/pkg/model"
	providerfake "github.com/gnanam1990/argus/pkg/model/fake"
	"github.com/gnanam1990/argus/pkg/trajectory"
)

func noEnv(string) string { return "" }

func TestBuildProviderKinds(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"anthropic", "openai"} {
		cfg := config.Defaults()
		cfg.Provider.Kind = kind
		cfg.Provider.Model = "m"
		p, err := app.BuildProvider(cfg, noEnv)
		if err != nil || p == nil {
			t.Errorf("%s: %v", kind, err)
		}
	}
	// compat requires base_url.
	cfg := config.Defaults()
	cfg.Provider.Kind = "compat"
	if _, err := app.BuildProvider(cfg, noEnv); err == nil {
		t.Error("compat without base_url should error")
	}
	cfg.Provider.BaseURL = "http://localhost:1234/v1"
	if _, err := app.BuildProvider(cfg, noEnv); err != nil {
		t.Errorf("compat with base_url: %v", err)
	}
}

func TestCompatPresets(t *testing.T) {
	t.Parallel()
	// Kimi, xAI, Gemini, and Ollama are OpenAI-compatible presets: they build
	// without a base_url (the preset supplies a default) and read distinct key
	// envs.
	for _, kind := range []string{"kimi", "xai", "gemini", "ollama"} {
		cfg := config.Defaults()
		cfg.Provider.Kind = kind
		cfg.Provider.Model = "m"
		if err := cfg.Validate(); err != nil {
			t.Errorf("%s: config invalid: %v", kind, err)
		}
		p, err := app.BuildProvider(cfg, noEnv)
		if err != nil || p == nil {
			t.Errorf("%s: BuildProvider: %v", kind, err)
		}
		// Non-native → emulated computer use → grounding engages.
		if p != nil && p.Capabilities().NativeComputerUse {
			t.Errorf("%s should report emulated computer use", kind)
		}
	}

	wantEnv := map[string]string{
		"anthropic": "ANTHROPIC_API_KEY",
		"openai":    "OPENAI_API_KEY",
		"kimi":      "MOONSHOT_API_KEY",
		"xai":       "XAI_API_KEY",
		"gemini":    "GEMINI_API_KEY",
		"ollama":    "OLLAMA_API_KEY",
		"compat":    "ARGUS_API_KEY",
	}
	for kind, env := range wantEnv {
		if got := app.APIKeyEnv(kind); got != env {
			t.Errorf("APIKeyEnv(%q) = %q, want %q", kind, got, env)
		}
	}
}

func TestBuildProviderWithAuth(t *testing.T) {
	t.Parallel()
	mgr := oauth.NewManager(oauth.NewStore(t.TempDir()))

	// chatgpt requires the manager.
	cg := config.Defaults()
	cg.Provider.Kind = "chatgpt"
	if _, err := app.BuildProviderWithAuth(context.Background(), cg, noEnv, nil); err == nil {
		t.Error("chatgpt without a manager should error")
	}
	p, err := app.BuildProviderWithAuth(context.Background(), cg, noEnv, mgr)
	if err != nil || p == nil {
		t.Fatalf("chatgpt with manager: %v", err)
	}
	if p.Capabilities().NativeComputerUse {
		t.Error("codex adapter should report emulated computer use")
	}

	// API-key kinds delegate to BuildProvider.
	ak := config.Defaults() // anthropic
	if _, err := app.BuildProviderWithAuth(context.Background(), ak, noEnv, mgr); err != nil {
		t.Errorf("anthropic delegate: %v", err)
	}

	// xai with an API key set uses compat directly.
	xk := config.Defaults()
	xk.Provider.Kind = "xai"
	env := func(k string) string {
		if k == "XAI_API_KEY" {
			return "sk-xai"
		}
		return ""
	}
	if _, err := app.BuildProviderWithAuth(context.Background(), xk, env, mgr); err != nil {
		t.Errorf("xai with key: %v", err)
	}
}

func TestBuildGrounder(t *testing.T) {
	t.Parallel()
	none := config.Defaults()
	if g, _ := app.BuildGrounder(none); g != nil {
		t.Error("mode none should yield nil grounder")
	}

	axCfg := config.Defaults()
	axCfg.Grounding.Mode = "ax"
	if g, _ := app.BuildGrounder(axCfg); g == nil {
		t.Error("ax grounder should be non-nil")
	} else if _, ok := g.(*ax.Detector); !ok {
		t.Errorf("expected *ax.Detector, got %T", g)
	}

	chainCfg := config.Defaults()
	chainCfg.Grounding.Mode = "chain"
	chainCfg.Grounding.OmniParserURL = "http://op"
	if g, _ := app.BuildGrounder(chainCfg); g == nil {
		t.Error("chain grounder should be non-nil")
	} else if _, ok := g.(*chain.Grounder); !ok {
		t.Errorf("expected *chain.Grounder, got %T", g)
	}
}

func TestBuildMiddlewareComposition(t *testing.T) {
	t.Parallel()
	log := slog.New(slog.NewTextHandler(discard{}, nil))
	allow := middleware.ApproverFunc(func(context.Context, action.Action) (bool, error) { return true, nil })

	// Default: telemetry + image-retention + injection-guard = 3.
	base := app.BuildMiddleware(config.Defaults(), nil, log, "run", allow)
	if len(base) != 3 {
		t.Errorf("default middleware = %d, want 3", len(base))
	}

	cfg := config.Defaults()
	cfg.Agent.RequireApproval = true
	cfg.Agent.BudgetTokens = 1000
	full := app.BuildMiddleware(cfg, []string{"secret"}, log, "run", allow)
	// + redaction + approval + budget = 6.
	if len(full) != 6 {
		t.Errorf("full middleware = %d, want 6", len(full))
	}
}

func TestNewRunnerRunsEndToEnd(t *testing.T) {
	t.Parallel()
	cfg := config.Defaults()
	prov := providerfake.New(
		model.ActionTurn(model.Usage{}, action.Action{Type: action.Click, Button: action.Left, Mark: action.NoMark}),
		model.EndTurn("done", model.Usage{}),
	)
	comp := compfake.New()
	log := slog.New(slog.NewTextHandler(discard{}, nil))
	mw := app.BuildMiddleware(cfg, nil, log, "run", nil)

	r := app.NewRunner(cfg, prov, comp, nil, nil, trajectory.NoOp{}, mw)
	out, err := r.Run(context.Background(), "do it")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if out.Reason != "completed" {
		t.Errorf("reason = %q, want completed", out.Reason)
	}
}

func TestBuildComputerHost(t *testing.T) {
	t.Parallel()
	cfg := config.Defaults() // sandbox.kind = host
	comp, cleanup, err := app.BuildComputer(context.Background(), cfg, noEnv)
	if err != nil {
		t.Fatalf("BuildComputer(host): %v", err)
	}
	if comp == nil || cleanup == nil {
		t.Fatal("expected a computer and cleanup")
	}
	if err := cleanup(); err != nil {
		t.Errorf("cleanup: %v", err)
	}

	bad := config.Defaults()
	bad.Sandbox.Kind = "bogus"
	if _, _, err := app.BuildComputer(context.Background(), bad, noEnv); err == nil {
		t.Error("unknown sandbox kind should error")
	}
}

func TestBuildProviderWithKey(t *testing.T) {
	t.Parallel()
	cfg := config.Defaults()
	cfg.Provider.BaseURL = "https://example.test"
	env := func(k string) string {
		if k == "ANTHROPIC_API_KEY" {
			return "sk-test"
		}
		return ""
	}
	if p, err := app.BuildProvider(cfg, env); err != nil || p == nil {
		t.Errorf("BuildProvider with key/base-url: %v", err)
	}
}

func TestSummaryAndManifest(t *testing.T) {
	t.Parallel()
	cfg := config.Defaults()
	cfg.Agent.BudgetTokens = 5000
	s := app.Summary(cfg)
	for _, want := range []string{"provider=anthropic", "model=claude-opus-4-8", "5000 tokens", "config-hash="} {
		if !strings.Contains(s, want) {
			t.Errorf("summary missing %q: %s", want, s)
		}
	}

	m := app.Manifest(cfg, "task", "deadbee", "2026-07-04T00:00:00Z")
	if m.Model != "claude-opus-4-8" || m.GitSHA != "deadbee" || m.ConfigHash == "" || m.Task != "task" {
		t.Errorf("manifest = %+v", m)
	}
}

type discard struct{}

func (discard) Write(p []byte) (int, error) { return len(p), nil }

func TestSystemPromptFoldsInSkills(t *testing.T) {
	t.Parallel()
	cfg := config.Defaults()
	cfg.Agent.System = "Base prompt."
	cfg.Agent.Skills = []string{"macos-basics"}

	sys, err := app.SystemPrompt(cfg, func(string) string { return "" })
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(sys, "Base prompt.") {
		t.Error("base system prompt must be preserved")
	}
	if !strings.Contains(sys, "Operating macOS") {
		t.Error("skill guidance must be folded into the system prompt")
	}

	// No skills → unchanged.
	cfg.Agent.Skills = nil
	sys, err = app.SystemPrompt(cfg, func(string) string { return "" })
	if err != nil || sys != "Base prompt." {
		t.Errorf("no skills should leave the prompt unchanged, got %q / %v", sys, err)
	}
}
