# Argus — build roadmap

Argus is built in disciplined, independently-testable stages. The ordering is
**loop-first**: the core interface seams and their fakes land before any real
provider or driver, so the agent loop can be built and tested as a walking
skeleton before the heavy integration work begins. Fakes always precede reals;
providers and drivers never depend on the loop.

## Principles

- **Small interfaces, everywhere a fake.** Every seam (`Computer`, `Provider`,
  `Grounder`, `Sandbox`) is narrow and has a test fake — the default
  `go test ./...` needs no display, network, or containers.
- **Provider-agnostic.** One thin `model.Provider` seam over official
  first-party SDKs plus a local/OpenAI-compatible adapter; the loop imports no
  vendor SDK. A single always-on normalizer maps every provider's raw action
  vocabulary into one canonical `action.Action`.
- **Coordinate scaling is owned in one place** (`pkg/computer` + the loop), not
  scattered — the HiDPI / multi-monitor invariant lives at the executor.
- **Safety is sequenced, not bolted on.** Approval gating, injection defense,
  secret redaction, and transport TLS land *before* the first runnable
  `argus run`.

## Stages

| Stage | Title | Focus | Tests |
|---|---|---|---|
| **S0** | Scaffold | module, license, CI, `version` command | version formatting, CLI dispatch |
| S1 | `pkg/action` | canonical domain model: `Image`, geometry, `Action` union, `Validate` | scale/center incl. HiDPI, JSON round-trip, per-type validation |
| S2 | `pkg/model` + fakes | neutral conversation types, `Provider`/`Clicker` seam, scriptable fake | scripted multi-turn playback, zero vendor imports |
| S3 | `pkg/computer` + executor | driver seam, `ActionExecutor` (scaling, mark→center, capability gate), fake | scaling, mark resolution, dangerous-op-blocked-by-default |
| S4 | `pkg/grounder` | set-of-marks seam, `Element`, `Marker`, fake | box-space invariants, overlay index integrity |
| S5 | `pkg/trajectory` | versioned `Step` schema, in-memory + no-op recorders | JSON round-trip + schema version, concurrent append |
| S6 | `pkg/agent` | observe→think→act loop, ordered middleware, functional options | end-to-end with fakes: seam order, termination modes |
| S7 | middleware + pricing | image-retention, budget(+pricing), HITL approval, injection guard, redaction, compaction, timeouts, telemetry | per-middleware table tests |
| S8 | `anthropic` adapter + normalizer | native computer tool, streaming, raw→canonical repair | SSE golden fixtures, version-string pinning |
| S9 | `openai` adapter | Responses computer tool, safety-check ack via HITL | goldens, canonical parity |
| S10 | `gemini` + `compat` + `clicker` | emulated tool, local router, pure-grounding click | goldens, offline |
| S11 | `shell` driver | CGo-free Linux/X11 backend via injectable command runner | argv assertions, fixture screenshot — no real binaries |
| S12 | `robotgo` driver + platform | native mac/Windows backend (build-tagged), TCC/Wayland/multi-monitor probes | default test never compiles CGo; Xvfb smoke |
| S13 | `mark` overlay | pure-Go numbered overlay + grounding downscale | golden-PNG compare |
| S14 | `omniparser` + `chain` | out-of-process vision client, health check + circuit breaker, AGPL caveat | mock service, breaker paths |
| S15 | `ax` detector | accessibility-tree grounding + fallback | recorded AX-tree fixtures |
| S16 | `pkg/mcpserver` | MCP tool surface (pinned SDK + contract test) | in-memory transport, per-tool schema |
| S17 | `cmd/argus-mcp` | tool-server binary (stdio / auth-gated HTTP) | flag parse, start/stop smoke |
| S18 | `transport` | `{id,command,params}` envelope, WSS/TLS, run-ID correlation, rate limit | round-trip, concurrent writes |
| S19 | `guestd` | in-sandbox server: dispatch, auth, token rotation, audit log | per-command dispatch, auth accept/reject |
| S20 | `pkg/sandbox` + remote + host | sandbox seam, `RemoteComputer`, host provider, teardown | remote↔fake-guest, gated exec/file |
| S21 | `docker` provider | container provisioning, guaranteed teardown, guest image | integration (build-tagged) + hermetic run-args |
| S22 | `config` | layered load (defaults→file→env→flags), env-only secrets, validate/doctor | precedence, secret-never-from-file |
| S23 | `cmd/argus` CLI | wire everything; SIGINT teardown; dry-run; allowlist; cost summary | run-with-fakes, doctor report |
| S24 | trajectory recorder | JSONL + screenshots, provenance manifest, secret masking, RL export | reload-lossless, masking |
| S25 | eval harness | run/replay/score/report + `argus eval` | scripted pass/fail, deterministic replay |
| S26 | docs + examples | quickstart, per-provider setup, threat model, API-stability, runnable examples | doc-lint, example configs vet |
| S27 | release automation | GoReleaser per-OS, SBOM + signing, guest image, license-scan gate | release dry-run, checksum/SBOM |

## PR batching (~2–3k lines each, loop-first)

| PR | Stages | Theme |
|---|---|---|
| PR-1 | S0–S2 | Foundation |
| PR-2 | S3–S5 | Seams + fakes |
| PR-3 | S6–S7 | Agent loop + safety (walking skeleton) |
| PR-4a / 4b | S8 / S9–S10 | Providers |
| PR-5 | S11–S12 | Local drivers + platform hardening |
| PR-6 | S13–S15 | Set-of-marks |
| PR-7 | S16–S17 | MCP surface |
| PR-8 | S18–S19 | Transport + guest |
| PR-9 | S20–S21 | Sandbox providers |
| PR-10 | S22–S23 | Config + CLI |
| PR-11 | S24–S25 | Trajectory + eval |
| PR-12 | S26–S27 | Docs + release |

## Platform posture (honest)

- **Linux/X11:** CGo-free static `shell` driver — full host control.
- **Linux/Wayland:** CGo-free `wayland` driver — input via `ydotool` (uinput),
  screenshots via `grim`/`gnome-screenshot`/`spectacle`. Auto-selected on a
  Wayland session; needs those tools + the `ydotoold` daemon.
- **macOS/Windows:** `robotgo` built `CGO_ENABLED=1` on native runners (not
  cross-compiled); the "single static binary" claim is dropped for those OSes.
- **Containers** drive a *Linux* desktop only — never the macOS/Windows host GUI.

## Key risks tracked

- robotgo cannot be cross-compiled → per-OS CI runners for mac/Windows.
- Vision grounding needs a GPU and carries AGPL weights → out-of-process,
  opt-in, and CI license-scan-gated out of any shipped path; default grounding
  is `none` when the provider has native computer-use.
- Provider schema/version strings drift → pinned `const` blocks, CI smoke fails
  loudly on stale strings.
- The in-sandbox control server is RCE-shaped → TLS/WSS, localhost-default bind,
  token rotation, audit log, rate limiting — delivered before it is exposed.
