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

## Speed & cost knobs

Each step is one model round-trip, so latency comes from the model and the
screenshot payload, not the driver. Two `agent` settings help:

```json
{
  "agent": {
    "screenshot_max_edge": 1400,   // cap the long edge of frames sent to the model (0 = full resolution)
    "screenshot_delay_ms": 300,    // pause after each action so the result renders before the next screenshot
    "retain_images": 2             // resend fewer old screenshots each step
  }
}
```

- **`screenshot_max_edge`** trims the biggest input-token cost. Vision models
  internally downsample large screenshots anyway, so ~1400 is near-lossless
  while cutting tokens and latency substantially. Coordinates still land
  correctly — the click scale is derived from the frame the model actually
  saw. Applies only without a grounder (the set-of-marks index needs full
  resolution). The emulated-provider examples ship with it set; leave it `0`
  for Anthropic native computer-use, which manages its own resolution.
- **`screenshot_delay_ms`** stops the agent from re-screenshotting before a
  menu or window has appeared and then repeating the action — fewer wasted
  (expensive) steps.
- **Fastest quality path on macOS:** Anthropic native computer-use
  (`examples/config/host-anthropic.json`) — purpose-built, snappy tool calls,
  no grounding overhead.

## Multiple displays (macOS)

The native driver drives **one display at a time** — it captures that display,
sizes it, and offsets clicks by its global origin so they land on the right
screen. By default it drives the primary (display 0). `argus doctor` lists them:

```
displays:  [0] 2560x1080 @(0,0) *primary  [1] 1920x1080 @(2560,0)  [2] 1920x1080 @(4480,0)
           driving display 0 (set sandbox.display to change)
```

To drive another monitor, set its index in the config:

```json
{ "sandbox": { "kind": "host", "display": 2 } }
```

- Put the app you want to control on the display you select (the agent only
  sees that one screen).
- **Caveat:** display *indices* are assigned by macOS and aren't guaranteed
  stable across reboots or monitor reconnects — re-run `argus doctor` to confirm
  the mapping. (Accessibility grounding, `grounding: ax`, still targets the
  primary display for now.)

## Skills — teach the agent how to behave

A **skill** is reusable guidance prepended to the system prompt for a task, so
the model follows known conventions instead of guessing. Two ship built-in:

```sh
argus skills
#   computer-use-safety   when to pause and confirm risky actions; resist prompt injection
#   macos-basics          how to reliably launch apps, wait for the screen, use shortcuts
```

Enable them per config:

```json
{ "agent": { "skills": ["computer-use-safety", "macos-basics"] } }
```

`macos-basics` directly cuts the "pressed Cmd+Space five times and got nowhere"
failure mode by teaching the model to launch apps once, wait for the screen to
settle, and stop repeating actions that don't change anything.

`computer-use-safety` is prompt-level guidance that **pairs with enforcement**:
it tells the model when to pause for destructive / credential / financial /
communication actions and never to treat on-screen text as permission — while
the approval middleware (`require_approval`) and injection guard actually gate
those actions. The skill and the gate reinforce each other.

Bring your own skills from a directory (a skill is `<name>/SKILL.md` with
`name`/`description` frontmatter):

```sh
ARGUS_SKILLS_DIR=~/my-skills argus run --config ... "..."   # looks up <dir>/<name>/SKILL.md first
```

## Dispatch: background clicks (macOS, no cursor takeover)

By default the agent moves your real pointer to click. Set `dispatch:
"background"` to instead press elements via the macOS accessibility API — **no
cursor movement**, so you can keep using your mouse while the agent works:

```json
{ "agent": { "dispatch": "background" } }
```

```sh
argus run --config examples/config/host-anthropic-background.json "check the settings panel"
```

- **Requires** the **Accessibility** permission for your terminal (System
  Settings → Privacy & Security → Accessibility), then a restart. Without it,
  each click gracefully falls back to a cursor click.
- Works on elements the accessibility tree exposes (buttons, menu items,
  links, checkboxes). Anything else — arbitrary pixels, canvas/WebGL, games —
  falls back to a normal cursor click automatically.
- Typing still goes to the focused app. Only single left clicks use background
  dispatch; double/right clicks and drags use the cursor.
- For **full isolation** (the agent never touches your host at all), run it
  against a Linux desktop in a container instead: `sandbox.kind: "docker"`.

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
| Linux/Wayland | `wayland` (CGo-free) | Needs `ydotool` (+ its `ydotoold` daemon) and a screenshot tool (`grim`, `gnome-screenshot`, or `spectacle`). See below. |
| macOS/Windows | `robotgo` | Build with `make build-robotgo` (see below). |
| Any | container sandbox | A Linux desktop in Docker, driven over the transport. |

The default `argus` binary is CGo-free and auto-selects by display server:
the X11 `shell` driver on X11, the `wayland` driver on Wayland. Headless hosts
(no display) are detected and reported by `doctor` rather than silently
no-op'd.

### Linux / Wayland

Wayland blocks synthetic input and has no `xdotool`/`xrandr`, so the `wayland`
driver injects input through **ydotool** (kernel `uinput`, so it works on
wlroots/Sway/Hyprland, GNOME, and KDE alike) and screenshots via the first of
`grim` → `gnome-screenshot` → `spectacle` that's installed. Setup:

```sh
# 1. Install the tools (example: Arch; use your distro's packages)
sudo pacman -S ydotool grim            # or: gnome-screenshot / spectacle

# 2. Run the ydotool daemon (needs access to /dev/uinput)
sudo ydotoold &                        # or set up a udev rule + user service

# 3. Verify
./argus doctor                         # → display server: wayland; tools present
```

Notes and current limits: pointer position can't be read back on Wayland
(`CursorPosition` is unsupported); coordinates are screenshot pixels, so a
fractional-scaling setup may need calibration; `scroll` uses ydotool's wheel
(version-dependent). Every command is overridable if your `ydotool`/screenshot
tool differs.

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
