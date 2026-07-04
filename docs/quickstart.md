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
| macOS/Windows | `robotgo` | Build with `make build-robotgo` (see below). |
| Any | container sandbox | A Linux desktop in Docker, driven over the transport. |

The default `argus` binary uses the CGo-free X11 `shell` driver, which only
works on Linux/X11. Wayland and headless hosts are detected and reported by
`doctor` rather than silently no-op'd.

### macOS / Windows

The native backend is CGo + build-tagged, so build it explicitly:

```sh
make build-robotgo      # CGO_ENABLED=1 go build -tags robotgo
./bin/argus doctor      # → display server: native (robotgo/darwin)
```

**macOS permissions are required at runtime:**

- **Screen Recording** — without it, screen capture fails with
  `Capture image not found` and the agent can't see the screen.
- **Accessibility** — required to synthesize mouse/keyboard input.

Grant both under *System Settings → Privacy & Security* for the argus binary
(or the terminal that launches it). For a distributable binary, sign with a
Developer ID and notarize so the OS persists the grant. Mouse/keyboard and
`GetScreenSize` work without any permission; only capture and input synthesis
are gated. On multi-monitor setups the primary display defines the coordinate
space.

## Evaluate

```sh
argus eval --manifest examples/tasks.json --config examples/config/host-anthropic.json
```

Prints a machine-readable pass/fail report over the task set.
