package main

import (
	"context"
	"fmt"
	"io"

	"github.com/gnanam1990/argus/internal/app"
	"github.com/gnanam1990/argus/internal/computeruse/approval"
	"github.com/gnanam1990/argus/internal/computeruse/instructions"
	"github.com/gnanam1990/argus/internal/config"
)

// cuCmd dispatches the `argus cu` app-aware computer-use management commands.
func cuCmd(args []string, out io.Writer) error {
	if len(args) == 0 {
		fmt.Fprint(out, cuUsage)
		return nil
	}
	switch args[0] {
	case "run":
		return cuRun(args[1:], out)
	case "approvals":
		return cuApprovals(args[1:], out)
	case "instructions":
		return cuInstructions(args[1:], out)
	case "doctor":
		return cuDoctor(args[1:], out)
	default:
		return fmt.Errorf("cu: unknown subcommand %q (run \"argus cu\")", args[0])
	}
}

// cuRun runs the agent loop with the computer-use confirmation policy forced on,
// so risky actions are classified and gated even if the config didn't enable it.
func cuRun(args []string, out io.Writer) error {
	configPath, trajDir, _, tuiMode, task := parseRun(args)
	cfg, err := config.Load(configPath)
	if err != nil {
		return err
	}
	cfg.ComputerUse.RequireConfirm = true
	if err := cfg.Validate(); err != nil {
		return err
	}
	if task == "" {
		return fmt.Errorf("cu run: a task is required (argus cu run \"do the thing\")")
	}
	return runResolved(cfg, trajDir, tuiMode, task, out)
}

// cuApprovals manages the per-app approval store.
func cuApprovals(args []string, out io.Writer) error {
	store, err := openApprovalStore()
	if err != nil {
		return err
	}
	ctx := context.Background()
	action := "list"
	if len(args) > 0 {
		action = args[0]
	}
	switch action {
	case "list":
		recs, err := store.List(ctx)
		if err != nil {
			return err
		}
		if len(recs) == 0 {
			fmt.Fprintln(out, "no app approvals recorded (every app is pending/denied by default)")
			return nil
		}
		for _, r := range recs {
			fmt.Fprintf(out, "  %-40s %s\n", r.BundleIdentifier, r.Decision)
		}
		return nil
	case "add", "approve":
		if len(args) < 2 {
			return fmt.Errorf("cu approvals add: a bundle identifier is required")
		}
		if err := store.Set(ctx, args[1], approval.Approved); err != nil {
			return err
		}
		fmt.Fprintf(out, "approved %s\n", args[1])
		return nil
	case "remove", "revoke":
		if len(args) < 2 {
			return fmt.Errorf("cu approvals remove: a bundle identifier is required")
		}
		if err := store.Remove(ctx, args[1]); err != nil {
			return err
		}
		fmt.Fprintf(out, "removed %s (now pending)\n", args[1])
		return nil
	default:
		return fmt.Errorf("cu approvals: unknown action %q (list|add|remove)", action)
	}
}

// cuInstructions lists the built-in per-app instructions.
func cuInstructions(args []string, out io.Writer) error {
	if len(args) > 0 && args[0] != "list" {
		return fmt.Errorf("cu instructions: unknown action %q (list)", args[0])
	}
	list, err := instructions.List()
	if err != nil {
		return err
	}
	fmt.Fprintln(out, "built-in per-app instructions:")
	for _, in := range list {
		fmt.Fprintf(out, "  %-22s %s\n", in.BundleIdentifier, in.AppName)
	}
	dir, derr := instructions.DefaultDir()
	if derr == nil {
		fmt.Fprintf(out, "\nOverride or add your own at %s/<bundle-id>.md\n", dir)
	}
	return nil
}

// cuDoctor reports permission status and the recorded app approvals.
func cuDoctor(_ []string, out io.Writer) error {
	fmt.Fprintln(out, "argus cu doctor")

	orch := app.PermissionOrchestrator()
	ctx := context.Background()
	if locked, err := orch.IsLocked(ctx); err != nil {
		fmt.Fprintln(out, "  screen lock:   ", err)
	} else {
		fmt.Fprintf(out, "  screen lock:    %v\n", locked)
	}
	if err := orch.Ensure(ctx); err != nil {
		fmt.Fprintln(out, "  permissions:   ", err)
	} else {
		fmt.Fprintln(out, "  permissions:    accessibility + screen recording granted")
	}

	store, err := openApprovalStore()
	if err != nil {
		return err
	}
	recs, err := store.List(ctx)
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "  approved apps:  %d recorded (argus cu approvals list)\n", len(recs))
	return nil
}

func openApprovalStore() (approval.Store, error) {
	path, err := approval.DefaultPath()
	if err != nil {
		return nil, err
	}
	return approval.NewFileStore(path), nil
}

const cuUsage = `argus cu - desktop computer use helpers (macOS)

Usage:
  argus cu run [--config F] [--tui] "TASK"   Run a task with the confirmation policy on
  argus cu approvals list                    List per-app approval decisions
  argus cu approvals add <bundle-id>         Approve an app (e.g. com.apple.clock)
  argus cu approvals remove <bundle-id>      Revoke an app (back to pending)
  argus cu instructions list                 List built-in per-app instructions
  argus cu doctor                            Check permissions and approvals

'argus cu run' runs the ordinary agent with the risk-classifying confirmation
policy enabled (it prompts before risky actions). The full app-aware toolset
(get_app_state, list_apps, click-by-element) with the per-app approval store and
per-app instructions is served over MCP by 'argus-mcp --mode=computeruse' —
point an MCP client at that. approvals/instructions/doctor here manage the state
that server uses.
`
