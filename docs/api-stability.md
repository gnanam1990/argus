# API stability

Argus separates a stable public surface (`pkg/`) from implementation details
(`internal/`).

## `pkg/` — public contracts

These packages are the reusable API. Their exported types and function
signatures follow semantic versioning; breaking changes bump the major version.

| Package | Contract |
|---|---|
| `pkg/action` | Canonical `Action` union, geometry, `Image`, `Result`. |
| `pkg/model` | `Provider`/`Clicker` seam, neutral `Conversation`/`Turn`/`Usage`. |
| `pkg/computer` | `Computer` driver seam + `ActionExecutor`. |
| `pkg/grounder` | Set-of-marks `Grounder`/`Marker`/`Element`. |
| `pkg/agent` | `Session`/`Runner` loop + `Middleware`. |
| `pkg/trajectory` | Versioned `Step`/`Manifest` schema + recorders. |
| `pkg/sandbox` | `Sandbox`/`Provider` contract. |

Every seam ships with a fake under `.../fake` for testing.

## `internal/` — no stability promise

Provider adapters, drivers, grounder backends, transport, config, and the CLI
wiring live under `internal/` and may change at any time. Depend on `pkg/` and
implement its interfaces rather than importing `internal/`.

## Wire schemas

- **Trajectory** (`pkg/trajectory`): `SchemaVersion` in the manifest; bumped on
  any backward-incompatible change to `Manifest`/`Step`.
- **OmniParser** (`internal/grounder/omniparser`): `SchemaVersion = 2`; a
  mismatched service response is rejected.
- **Guest transport** (`internal/guest/proto`): the command vocabulary is
  versioned with the module.

## Provider version pinning

Provider beta/model/tool version strings are pinned in one `const` block per
adapter and verified against the installed SDK. A gated live smoke test fails
loudly when a pinned string goes stale, so schema churn surfaces in CI rather
than as a production 400.
