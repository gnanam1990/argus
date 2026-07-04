# Argus

**A provider-agnostic computer-use agent, written in Go.**

Argus ships as a single binary and drives a real or sandboxed desktop through a
tight *observe → think → act* loop. It captures the screen, optionally grounds
it with a swappable **set-of-marks** pipeline (numbered overlays so the model
picks a mark instead of guessing raw pixels), asks any vision + tool-use model
what to do next, normalizes every provider's action vocabulary into one
canonical schema, and executes mouse/keyboard/scroll actions behind a narrow
`Computer` interface — locally or across a WebSocket to an in-sandbox guest
server, with the *same* agent code in every deployment.

Everything cross-cutting — budget, human-in-the-loop approval, prompt-injection
defense, secret redaction, telemetry, and trajectory recording for eval/RL — is
pluggable middleware, never core-loop code.

> **Status: Stage 0 — scaffold.** This is the foundation: module, license, CI,
> and a working `argus version` command. The agent loop, providers, drivers,
> grounding, and sandbox land in subsequent stages. See the roadmap in `docs/`.

## Design principles

- **Provider-agnostic.** One thin `model.Provider` seam over the official
  first-party SDKs plus a local/OpenAI-compatible adapter. The loop never
  imports a vendor SDK.
- **Small interfaces, everywhere a fake.** Every seam (`Computer`, `Provider`,
  `Grounder`, `Sandbox`) is a narrow interface with a test fake, so the whole
  loop unit-tests with no display, no network, and no containers.
- **Set-of-marks grounding is swappable.** Numbered-overlay marking stays pure
  Go; detector backends (accessibility tree, out-of-process vision service) plug
  in behind one interface.
- **Safety is delivered in order, not bolted on.** Approval gating, injection
  defense, secret redaction, and transport TLS land before the first runnable
  `argus run`.

## Platform support

Argus separates the **host** driver (control your own desktop) from the
**sandbox** driver (control a desktop inside a container/VM over a network).

| Host OS | Host driver | Notes |
|---|---|---|
| Linux / X11 | `shell` (CGo-free, static binary) | Full host control. |
| macOS | `robotgo` (CGO_ENABLED=1) | Requires granting Screen Recording + Accessibility. |
| Windows | `robotgo` (CGO_ENABLED=1) | Built on a native Windows runner. |
| Any | `remote` → sandbox | A Linux desktop inside a container, driven over WSS. |

> A Linux container drives a *Linux* desktop; it cannot control the macOS or
> Windows host GUI. Wayland host sessions are detected and reported rather than
> silently no-op'd.

## Quickstart

```sh
# Build
make build

# Show version
./bin/argus version
```

More usage (running a task, configuring a provider, launching a sandbox) is
documented as those stages land.

## Development

```sh
make test    # go test -race ./...
make lint    # go vet + staticcheck
make cover   # coverage report
make tidy    # go mod tidy
```

## License

[MIT](LICENSE) © 2026 gnanam1990
