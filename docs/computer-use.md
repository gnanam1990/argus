# Computer Use (app-aware desktop control)

The **computer-use subsystem** is an app-aware alternative to driving raw
screen pixels: instead of "click (820, 540)", a caller names a macOS
application by its bundle identifier (e.g. `com.apple.Notes`) and gets back
that app's window, its accessibility element tree (each element carrying a
stable index), and a screenshot — then acts against an element index instead
of a guessed coordinate. It is **deny-by-default**: an app the agent has
never been told about cannot be driven at all, and every risky action is
classified against a confirmation policy before it runs.

This is a separate surface from the raw driver (`argus run`, `argus-mcp`
default mode) — it exists for tasks that are naturally "operate this specific
app" rather than "operate whatever's on screen."

## Build requirement (macOS)

Computer use needs the native backend: build with `make build-robotgo` (i.e.
`-tags robotgo`, CGo enabled). A default `make build` on macOS produces a
binary with the X11 shell driver, which has no backend on macOS — screen
capture and input silently fail at first use. Such a binary now prints a
`WARNING …` to stderr on startup, but the fix is to rebuild with the robotgo
tag. The native build also uses the AXUIElement API directly, so it needs only
the two permissions below (no Automation grant, and the accessibility walk
can't hang the way a System Events / `osascript` walk can).

## macOS permissions

Computer use needs the same two permissions as the raw driver, checked
together as one precondition gate before any capture or action:

- **Accessibility** (System Settings → Privacy & Security → Accessibility) —
  to read the element tree and synthesize input.
- **Screen Recording** (System Settings → Privacy & Security → Screen
  Recording) — to capture screenshots.

Grant both to the app actually running the process (usually your terminal),
then fully quit and reopen it — Screen Recording grants only take effect
after a relaunch. Run `argus cu doctor` to check current status:

```sh
argus cu doctor
#   screen lock:     false
#   permissions:     accessibility + screen recording granted
#   approved apps:   2 recorded (argus cu approvals list)
```

If the screen is locked or a permission is missing, every capture and action
fails closed with a message naming exactly what to fix — nothing is
attempted against a locked screen or without both permissions.

## Two surfaces

### `argus-mcp --mode=computeruse` — MCP tools

Serves the app-aware tools over stdio JSON-RPC (MCP), for an external client
(an editor, another agent) to drive:

```sh
argus-mcp --mode=computeruse --config examples/config/computeruse.json
```

Tools exposed:

| Tool | Purpose |
|---|---|
| `get_app_state` | Observe an app's window, element tree (with indices), and any per-app instruction. |
| `list_apps` | List the apps available to target. |
| `click` | Click an element (by index) or a point. |
| `type_text` | Type literal text into the focused element. |
| `press_key` | Press a key or key chord. |
| `scroll` | Scroll at an element or a point. |
| `drag` | Drag between two points. |
| `perform_secondary_action` | Right-click an element or a point. |

**`get_app_state` must be called before any other action on that app, every
turn.** The server tracks per-app freshness: an action tool refuses to run
unless `get_app_state` was the most recently observed thing for that app, so
a client can never act on a stale tree. Any action also invalidates the
cached observation for that app — the next action needs a fresh
`get_app_state` first, even if nothing else changed.

### `argus cu` — CLI

Runs a task, or manages the pieces the MCP server depends on:

```sh
argus cu run [--config F] [--tui] "check the reminder in Notes"
argus cu approvals list
argus cu approvals add com.apple.Notes
argus cu approvals remove com.apple.Notes
argus cu instructions list
argus cu doctor
```

`argus cu run` behaves like `argus run` but forces the confirmation policy on
regardless of config, since driving a named app on your real desktop is
exactly the situation that policy exists for.

## Approval: deny-by-default per app

Every app starts **Pending** — neither approved nor denied — and Pending is
treated the same as Denied: computer use refuses to observe or act on it.
Only an explicit `Approved` decision, set via `argus cu approvals add
<bundle-id>` or `computer_use.auto_approve_apps` in config, unlocks an app.
Decisions persist in a small JSON file
(`<userConfigDir>/argus/cu-approvals.json`) so approvals survive restarts;
`argus cu approvals remove` reverts an app back to Pending.

## Confirmation policy

Every proposed action is classified into a risk level before it runs:

| Level | Meaning |
|---|---|
| `no_confirm` | Routine UI interaction in an approved app — allowed silently. |
| `pre_approval` | Allowed only if the user's task explicitly authorized it (e.g. named both the sensitive data and its destination); otherwise routed to the approver. |
| `always_confirm` | Routed to the approver every time — destructive, credential, financial, install, or third-party-communication actions, and anything gated (`run_command`, file ops, window control) or derived from untrusted on-screen content. |
| `hand_off` | Refused outright — changing a password, bypassing a security/paywall barrier — the agent should not attempt this at all; the user must take over. |

The classifier is a heuristic backstop, not the authoritative gate: it pairs
with the same approval middleware and injection guard the raw driver uses
(`internal/middleware`) — the model-facing `computer-use-safety` skill tells
the model when to pause, the classifier catches risky intent even if the
model doesn't, and the approval/injection layers are what actually deny or
prompt for an action. `computer_use.require_confirmation` (or `argus cu run`,
which forces it on) wires this policy into the run.

## Adding per-app instructions

An app can have optional Markdown guidance folded into the agent's context
when it operates that app — hints like "click the Edit tab first" or "the
toggle relabels this button." A few ship built-in; `argus cu instructions
list` shows them along with the override directory to use:

```sh
argus cu instructions list
#   com.apple.clock        Clock
#   com.apple.Notes        Notes
#   com.apple.calculator   Calculator
#
# Override or add your own at ~/Library/Application Support/argus/cu-instructions/<bundle-id>.md
```

Add your own the same way `ARGUS_SKILLS_DIR` lets you bring your own
skills: drop a Markdown file named after the app's bundle identifier into
that directory.

```md
<!-- ~/Library/Application Support/argus/cu-instructions/com.example.app.md -->
Open the "Edit" tab before typing; the default "View" tab is read-only.
```

A user-supplied file always takes precedence over a built-in for the same
bundle identifier; an app with neither yields no instruction, which is the
default and entirely fine.

## Example config

```json
{
  "computer_use": {
    "enabled": true,
    "require_confirmation": true,
    "auto_approve_apps": ["com.apple.clock", "com.apple.Notes"],
    "max_capture_timeout_ms": 120000
  }
}
```

See `examples/config/computeruse.json` for a complete config. `enabled` turns
the subsystem on; `require_confirmation` wires in the confirmation policy;
`auto_approve_apps` pre-approves bundle identifiers at startup (equivalent to
running `argus cu approvals add` for each); `max_capture_timeout_ms` bounds
how long a capture retries a pending permission/lock precondition before
failing (default 120000ms).
