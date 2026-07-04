// Command argus is the entrypoint for the argus computer-use agent.
//
// Stage 0 ships only the process skeleton: a command dispatcher with a working
// `version` subcommand. Subsequent stages wire the agent loop, providers,
// drivers, grounding, and sandbox onto this root.
package main

import (
	"fmt"
	"io"
	"os"

	"github.com/gnanam1990/argus/internal/version"
)

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "argus:", err)
		os.Exit(1)
	}
}

// run dispatches a single argus invocation. It is separated from main so the
// dispatch logic is testable without spawning a process or touching os.Exit.
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
	default:
		return fmt.Errorf("unknown command %q (run \"argus help\")", args[0])
	}
}

func printUsage(out io.Writer) {
	fmt.Fprint(out, `argus - a provider-agnostic computer-use agent

Usage:
  argus <command> [flags]

Commands:
  version    Print build version information
  help       Show this help

More commands (run, doctor, eval) arrive in later stages.
`)
}
