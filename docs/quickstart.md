# Quickstart

Argus is a provider-agnostic computer-use agent: it captures the screen, asks a
vision + tool-use model what to do, and executes mouse/keyboard actions.

## Install

```sh
make build          # builds ./bin/argus
./bin/argus version
```

Or `go install github.com/gnanam1990/argus/cmd/argus@latest`.

## Check your environment

```sh
argus doctor
```

`doctor` reports the display server, whether the host can be driven (X11 vs
Wayland vs headless), whether your config is valid, and whether the provider API
key is set.

## Run a task

Set the provider key (env only — never in the config file):

```sh
export ANTHROPIC_API_KEY=sk-...
argus run "open a terminal and run 'echo hello'"
```

Preview the assembled plan without calling the model or driving anything:

```sh
argus run --dry-run "open a terminal"
# plan: provider=anthropic model=claude-opus-4-8 sandbox=host grounding=none ...
```

Use a config file and record the run:

```sh
argus run --config examples/config/host-anthropic.json --trajectory ./runs/first "book a flight"
```

The trajectory directory contains `manifest.json` (provenance), `steps.jsonl`,
and one PNG per observation, with secrets masked.

## Platform support

| Host OS | Driver | Notes |
|---|---|---|
| Linux/X11 | `shell` (CGo-free) | Needs `xdotool` + a screenshot tool (`maim`) + `xrandr`. |
| macOS/Windows | `robotgo` | Build with `-tags robotgo`; grant Screen Recording + Accessibility on macOS. |
| Any | container sandbox | A Linux desktop in Docker, driven over the transport. |

Wayland host sessions are detected and reported rather than silently no-op'd —
run against an Xvfb/X11 sandbox instead.

## Evaluate

```sh
argus eval --manifest examples/tasks.json --config examples/config/host-anthropic.json
```

Prints a machine-readable pass/fail report over the task set.
