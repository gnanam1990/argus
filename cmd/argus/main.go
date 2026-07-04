// Command argus is the entrypoint for the argus computer-use agent: run a task,
// diagnose the environment, or print the version.
package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"time"

	"github.com/gnanam1990/argus/internal/app"
	"github.com/gnanam1990/argus/internal/config"
	"github.com/gnanam1990/argus/internal/eval"
	"github.com/gnanam1990/argus/internal/pricing"
	"github.com/gnanam1990/argus/internal/version"
	"github.com/gnanam1990/argus/pkg/action"
	"github.com/gnanam1990/argus/pkg/agent"
	"github.com/gnanam1990/argus/pkg/trajectory"
)

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "argus:", err)
		os.Exit(1)
	}
}

func run(args []string, out io.Writer) error {
	if len(args) == 0 {
		printUsage(out)
		return nil
	}
	switch args[0] {
	case "version", "--version", "-v":
		fmt.Fprintln(out, version.String())
		return nil
	case "help", "--help", "-h":
		printUsage(out)
		return nil
	case "doctor":
		return doctor(args[1:], out)
	case "run":
		return runTask(args[1:], out)
	case "eval":
		return evalCmd(args[1:], out)
	default:
		return fmt.Errorf("unknown command %q (run \"argus help\")", args[0])
	}
}

// parseRun extracts --config, --trajectory, --dry-run, and the task.
func parseRun(args []string) (configPath, trajDir string, dryRun bool, task string) {
	var rest []string
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--config", "-c":
			if i+1 < len(args) {
				i++
				configPath = args[i]
			}
		case "--trajectory", "-t":
			if i+1 < len(args) {
				i++
				trajDir = args[i]
			}
		case "--dry-run":
			dryRun = true
		default:
			rest = append(rest, args[i])
		}
	}
	return configPath, trajDir, dryRun, strings.TrimSpace(strings.Join(rest, " "))
}

func runTask(args []string, out io.Writer) error {
	configPath, trajDir, dryRun, task := parseRun(args)

	cfg, err := config.Load(configPath)
	if err != nil {
		return err
	}
	if err := cfg.Validate(); err != nil {
		return err
	}
	if dryRun {
		fmt.Fprintln(out, "plan:", app.Summary(cfg))
		return nil
	}
	if task == "" {
		return fmt.Errorf("run: a task is required (argus run \"do the thing\")")
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	prov, err := app.BuildProvider(cfg, os.Getenv)
	if err != nil {
		return err
	}
	key := os.Getenv(app.APIKeyEnv(cfg.Provider.Kind))
	if key == "" {
		fmt.Fprintf(out, "warning: %s is not set; provider calls will fail\n", app.APIKeyEnv(cfg.Provider.Kind))
	}

	comp, cleanup, err := app.BuildComputer(ctx, cfg, os.Getenv)
	if err != nil {
		return err
	}
	defer func() { _ = cleanup() }()

	gr, marker := app.BuildGrounder(cfg)
	var secrets []string
	if key != "" {
		secrets = []string{key}
	}
	runID := fmt.Sprintf("run-%d", time.Now().UnixNano())
	mw := app.BuildMiddleware(cfg, secrets, logger(), runID, stdinApprover{out: out})

	manifest := app.Manifest(cfg, task, version.Commit, time.Now().UTC().Format(time.RFC3339))
	rec, err := buildRecorder(trajDir, manifest, maskFunc(secrets))
	if err != nil {
		return err
	}
	defer func() { _ = rec.Close() }()

	r := app.NewRunner(cfg, prov, comp, gr, marker, rec, mw)
	fmt.Fprintln(out, "running:", app.Summary(cfg))

	outcome, err := r.Run(ctx, task)
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "\noutcome: %s in %d steps\n", outcome.Reason, outcome.Steps)
	if outcome.FinalText != "" {
		fmt.Fprintln(out, outcome.FinalText)
	}
	if cost, ok := pricing.Cost(cfg.Provider.Model, outcome.Usage); ok {
		fmt.Fprintf(out, "cost: $%.4f (in %d / out %d tokens)\n", cost, outcome.Usage.InputTokens, outcome.Usage.OutputTokens)
	}
	return nil
}

func evalCmd(args []string, out io.Writer) error {
	var manifestPath, configPath string
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--manifest", "-m":
			if i+1 < len(args) {
				i++
				manifestPath = args[i]
			}
		case "--config", "-c":
			if i+1 < len(args) {
				i++
				configPath = args[i]
			}
		}
	}
	if manifestPath == "" {
		return fmt.Errorf("eval: --manifest FILE is required")
	}
	tasks, err := eval.LoadTasks(manifestPath)
	if err != nil {
		return err
	}
	cfg, err := config.Load(configPath)
	if err != nil {
		return err
	}
	if err := cfg.Validate(); err != nil {
		return err
	}
	// Fail fast if the provider can't be constructed.
	if _, err := app.BuildProvider(cfg, os.Getenv); err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	comp, cleanup, err := app.BuildComputer(ctx, cfg, os.Getenv)
	if err != nil {
		return err
	}
	defer func() { _ = cleanup() }()
	gr, marker := app.BuildGrounder(cfg)

	factory := func(task eval.Task) agent.Session {
		prov, _ := app.BuildProvider(cfg, os.Getenv)
		mw := app.BuildMiddleware(cfg, nil, logger(), "eval-"+task.Name, nil)
		return app.NewRunner(cfg, prov, comp, gr, marker, trajectory.NoOp{}, mw)
	}

	rep := eval.Run(ctx, tasks, factory, eval.Completed)
	fmt.Fprintln(out, string(rep.JSON()))
	fmt.Fprintf(out, "\n%d/%d passed\n", rep.Passed, rep.Total)
	return nil
}

func doctor(args []string, out io.Writer) error {
	configPath, _, _, _ := parseRun(args)
	cfg, err := config.Load(configPath)
	if err != nil {
		return err
	}

	fmt.Fprintln(out, "argus doctor")
	fmt.Fprintln(out, "  display server:", displayServer())
	if err := preflight(); err != nil {
		fmt.Fprintln(out, "  host control:  ", err)
	} else {
		fmt.Fprintln(out, "  host control:   ok")
	}
	if err := cfg.Validate(); err != nil {
		fmt.Fprintln(out, "  config:        ", err)
	} else {
		fmt.Fprintln(out, "  config:         valid ("+app.Summary(cfg)+")")
	}
	env := app.APIKeyEnv(cfg.Provider.Kind)
	if os.Getenv(env) == "" {
		fmt.Fprintf(out, "  api key:        %s not set\n", env)
	} else {
		fmt.Fprintf(out, "  api key:        %s set\n", env)
	}
	return nil
}

// stdinApprover prompts the operator to approve a risky action.
type stdinApprover struct {
	out io.Writer
}

func (a stdinApprover) Approve(_ context.Context, act action.Action) (bool, error) {
	fmt.Fprintf(a.out, "approve %s? [y/N] ", act.Type)
	r := bufio.NewReader(os.Stdin)
	line, _ := r.ReadString('\n')
	line = strings.ToLower(strings.TrimSpace(line))
	return line == "y" || line == "yes", nil
}

func printUsage(out io.Writer) {
	fmt.Fprint(out, `argus - a provider-agnostic computer-use agent

Usage:
  argus run [--config FILE] [--trajectory DIR] [--dry-run] "TASK"   Run a task
  argus eval --manifest FILE [--config FILE]                        Evaluate tasks
  argus doctor [--config FILE]                                      Diagnose the environment
  argus version                                                     Print version
  argus help                                                        Show this help

Config is layered: defaults < JSON file (--config) < ARGUS_* env vars.
Secrets (ANTHROPIC_API_KEY / OPENAI_API_KEY / ARGUS_API_KEY) come from the
environment only.
`)
}
