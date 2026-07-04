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
and one PNG per observation, with secrets masked. Replay it in the browser:

```sh
argus view ./runs/first
```

## Gated capabilities (run_command, file ops)

System-level actions are **off by default**. Enable them per config and pair
them with approval:

```json
{
  "agent": { "capabilities": ["run_command"], "require_approval": true }
}
```

With approval on, each `run_command` prompts `[y/N]` (inline in the TUI).
Unattended runs (approval off, or `argus eval`) fail closed: the injection
guard denies sensitive actions instead of running them silently. Extra secret
values can be masked everywhere via `ARGUS_SECRETS=val1,val2`.

## Interactive view (`--tui`)

Add `--tui` to watch the run in a live full-screen view: a header with the
model, step, elapsed time, token count and estimated cost; a scrolling feed of
the model's reasoning and each executed action; and inline `[y/N]` prompts for
risky actions (when `require_approval` is on). Press `q` or `ctrl-c` to stop.

```sh
argus run --tui --config examples/config/host-anthropic.json "book a flight"
```

```
╭─ argus · claude-opus-4-8 · step 4 · 00:38 · 1.2k tok · $0.01 ─╮
│ ▸ clicking the Submit button                                 │
│   ✓ click (820,540)                                          │
│ ⚠ approve run_command "rm -rf build"?  [y/N]                 │
╰──────────────────────────────────────────────────────────────╯
```

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

**macOS permissions are required at runtime.** Run `argus doctor` — the robotgo
build actually attempts a capture and tells you pass/fail:

```
  screen capture:  FAILED (robotgo capture failed (Capture image not found.): grant ...)
```

- **Screen Recording** — without it, capture fails with `Capture image not
  found` and the agent can't see the screen. This is a hard macOS permission
  gate: it blocks **every** capture API, including Apple's own `screencapture`
  (which fails identically with `could not create image from display`). It is
  not a bug and cannot be worked around in code.
- **Accessibility** — required to synthesize mouse/keyboard input.

**How to grant it:** the permission attaches to the *responsible* app — when you
run `argus` from a terminal, that's usually **the terminal app itself** (Ghostty,
iTerm, Terminal, …), not the `argus` binary. In *System Settings → Privacy &
Security → Screen Recording*, enable that app, then **fully quit and reopen it**
(Screen Recording changes require the app to relaunch). Re-run `argus doctor` to
confirm `screen capture: ok`.

For a distributable binary, sign with a Developer ID and notarize so the OS
persists the grant to the binary directly. Mouse/keyboard and `GetScreenSize`
work without any permission; only capture and input synthesis are gated. On
multi-monitor setups the primary display defines the coordinate space.

## Evaluate

```sh
argus eval --manifest examples/tasks.json --config examples/config/host-anthropic.json
```

Prints a machine-readable pass/fail report over the task set.
