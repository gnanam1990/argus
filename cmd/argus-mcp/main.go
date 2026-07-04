// Command argus-mcp serves the Argus computer driver as MCP tools over stdio,
// so any MCP client can capture the screen and drive mouse/keyboard through the
// same driver the agent loop uses.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"

	"github.com/gnanam1990/argus/internal/driver/shell"
	"github.com/gnanam1990/argus/internal/mcpserver"
	"github.com/gnanam1990/argus/internal/version"
)

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "version", "--version", "-v":
			fmt.Println(version.String())
			return
		case "help", "--help", "-h":
			fmt.Fprint(os.Stdout, usage)
			return
		default:
			fmt.Fprintf(os.Stderr, "argus-mcp: unknown argument %q\n", os.Args[1])
			os.Exit(1)
		}
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	srv := mcpserver.New(shell.New(), mcpserver.WithInfo("argus-mcp", version.String()))
	if err := srv.Serve(ctx, os.Stdin, os.Stdout); err != nil && ctx.Err() == nil {
		fmt.Fprintln(os.Stderr, "argus-mcp:", err)
		os.Exit(1)
	}
}

const usage = `argus-mcp - serve the Argus computer driver as MCP tools over stdio

Usage:
  argus-mcp            Serve MCP over stdin/stdout (X11 shell driver)
  argus-mcp version    Print version

Point an MCP client at this command to expose screenshot/click/type/key/
scroll/move/cursor_position tools.
`
