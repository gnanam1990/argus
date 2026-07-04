# Argus

**A provider-agnostic computer-use agent, written in Go. One static binary.**

Argus drives a real or sandboxed desktop through a tight *observe → think →
act* loop. It captures the screen, optionally grounds it with a swappable
**set-of-marks** pipeline (numbered overlays so the model picks a mark instead
of guessing raw pixels), asks any vision + tool-use model what to do next,
normalizes every provider's action vocabulary into one canonical schema, and
executes mouse/keyboard/scroll actions behind a narrow `Computer` interface —
locally or against a guest server inside a container, with the *same* agent
code in every deployment.

Everything cross-cutting — budget, human-in-the-loop approval, prompt-injection
defense, secret redaction, telemetry, and trajectory recording for eval/RL — is
pluggable middleware, never core-loop code.

## What ships today

- **Agent loop** (`pkg/agent`): observe→think→act with ordered middleware
  hooks, step/budget limits, truthful outcomes, and a trajectory record of
  every step. The public `pkg/*` seams never import a vendor SDK.
- **Providers**: Anthropic native computer-use; ChatGPT subscription login
  (OAuth, Codex backend); xAI (API key or OAuth); OpenAI, Kimi/Moonshot,
  Gemini, Ollama, and any OpenAI-compatible endpoint via one emulated computer
  tool. See [docs/providers.md](docs/providers.md).
- **Interactive TUI** (`argus run --tui`): live feed of reasoning and actions,
  token/cost header, inline `[y/N]` approval for risky actions.
- **Drivers**: native macOS/Windows backend (`-tags robotgo`), CGo-free
  X11 driver, and a remote driver that speaks to `guestd` inside a container.
- **Sandboxes**: host (trusted, gated) and Docker (provisioned, health-checked,
  bearer-token auth, optional TLS, guaranteed teardown).
- **Grounding**: set-of-marks overlay in pure Go; detectors plug in behind one
  interface (accessibility tree, OmniParser vision service, or a chain).
- **Safety rail**: capability allowlist (gated `run_command`/file ops are off
  by default), human approval, injection guard (post-observation actions are
  treated as untrusted; unattended runs fail closed), secret masking across
  the wire, the TUI, and recorded trajectories, plus token/USD budgets.
- **Trajectories**: `--trajectory` records manifest + steps + screenshots
  (secrets masked); `argus view DIR` replays a run in the browser; `argus
  eval` scores task suites; RL sample export.
- **MCP server** (`argus-mcp`): expose the computer as tools over JSON-RPC.

## Quickstart

```sh
make build            # CGo-free build (Linux/X11 driver)
make build-robotgo    # native macOS/Windows driver (CGo)

./bin/argus doctor    # check driver, permissions, config, keys

export ANTHROPIC_API_KEY=sk-ant-...
./bin/argus run --tui "open a text editor and type hello"

# record + replay
./bin/argus run --trajectory ./runs/first "check the weather"
./bin/argus view ./runs/first
```

Subscription logins instead of API keys (opt-in; see
[docs/oauth-subscriptions.md](docs/oauth-subscriptions.md)):

```sh
export ARGUS_OAUTH_ALLOW_PRESETS=1
./bin/argus auth login chatgpt      # or: xai
./bin/argus run --tui --config examples/config/chatgpt.json "..."
```

More: [docs/quickstart.md](docs/quickstart.md) ·
[docs/providers.md](docs/providers.md) ·
[docs/threat-model.md](docs/threat-model.md)

## Platform support

| Host OS | Driver | Status |
|---|---|---|
| macOS | `robotgo` (CGO, `make build-robotgo`) | Built + tested in CI on macOS runners; needs Screen Recording + Accessibility grants. |
| Linux / X11 | `shell` (CGo-free) | Needs `xdotool`, `xrandr`, and `maim`/`scrot`; `argus doctor` checks them. Wayland is detected and reported (not driveable). |
| Windows | `robotgo` (CGO) | Cross-compiles in CI; the native-runner robotgo build is not yet CI-verified. |
| Any | `remote` → Docker sandbox | Drive an X11 desktop in a container via `guestd` (bearer auth; TLS via `ARGUS_GUEST_TLS_CERT/KEY`). |

## Design principles

- **Provider-agnostic.** One thin `model.Provider` seam; the loop never
  imports a vendor SDK (enforced by a dependency audit in CI).
- **Small interfaces, everywhere a fake.** Every seam (`Computer`, `Provider`,
  `Grounder`, `Sandbox`) is narrow and ships a test fake, so the whole loop
  unit-tests with no display, no network, and no containers.
- **Safety is loop policy, not vibes.** Gated capabilities are deny-by-default
  and approval-gated; what can't be verified is treated as untrusted; what the
  redactor can and cannot protect is documented honestly.
- **Recorded by default-able.** The same trajectory schema is the runtime log,
  the replay input, the eval substrate, and the RL export.

## License

MIT.
