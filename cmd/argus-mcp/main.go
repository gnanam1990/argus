// Command argus-mcp serves Argus as MCP tools over stdio. The default mode
// exposes the raw computer driver (screenshot/click/type/…); --mode=computeruse
// exposes the app-aware desktop computer-use tools (get_app_state, list_apps,
// click-by-element, …) with per-app approval and a confirmation policy.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"

	"github.com/gnanam1990/argus/internal/app"
	"github.com/gnanam1990/argus/internal/config"
	"github.com/gnanam1990/argus/internal/driver/shell"
	"github.com/gnanam1990/argus/internal/mcpserver"
	"github.com/gnanam1990/argus/internal/version"
)

func main() {
	mode, configPath := "driver", ""
	for i := 1; i < len(os.Args); i++ {
		arg := os.Args[i]
		switch {
		case arg == "version" || arg == "--version" || arg == "-v":
			fmt.Println(version.String())
			return
		case arg == "help" || arg == "--help" || arg == "-h":
			fmt.Fprint(os.Stdout, usage)
			return
		case arg == "--mode" && i+1 < len(os.Args):
			i++
			mode = os.Args[i]
		case arg == "--mode=computeruse":
			mode = "computeruse"
		case arg == "--config" && i+1 < len(os.Args):
			i++
			configPath = os.Args[i]
		default:
			fmt.Fprintf(os.Stderr, "argus-mcp: unknown argument %q\n", arg)
			os.Exit(1)
		}
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	if err := serve(ctx, mode, configPath); err != nil && ctx.Err() == nil {
		fmt.Fprintln(os.Stderr, "argus-mcp:", err)
		os.Exit(1)
	}
}

func serve(ctx context.Context, mode, configPath string) error {
	switch mode {
	case "computeruse":
		cfg, err := config.Load(configPath)
		if err != nil {
			return err
		}
		if err := cfg.Validate(); err != nil {
			return err
		}
		if !cfg.ComputerUse.IsEnabled() {
			return fmt.Errorf("computer use is disabled in config (computer_use.enabled = false)")
		}
		cu, cleanup, err := app.BuildComputerUse(cfg)
		if err != nil {
			return err
		}
		defer func() { _ = cleanup() }()
		return cu.Server.Serve(ctx, os.Stdin, os.Stdout)
	case "driver", "":
		srv := mcpserver.New(shell.New(), mcpserver.WithInfo("argus-mcp", version.String()))
		return srv.Serve(ctx, os.Stdin, os.Stdout)
	default:
		return fmt.Errorf("unknown --mode %q (want \"driver\" or \"computeruse\")", mode)
	}
}

const usage = `argus-mcp - serve Argus as MCP tools over stdio

Usage:
  argus-mcp                                 Serve the raw driver tools (X11 shell)
  argus-mcp --mode=computeruse [--config F] Serve app-aware desktop computer-use tools (macOS)
  argus-mcp version                         Print version

Driver mode exposes screenshot/click/type/key/scroll/move/cursor_position.
Computer-use mode exposes get_app_state/list_apps/click/type_text/press_key/
scroll/drag/perform_secondary_action, gated by per-app approval and permissions.
`
