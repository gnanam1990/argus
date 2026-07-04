# Changelog

All notable changes to Argus are documented here. The format is based on
[Keep a Changelog](https://keepachangelog.com/); releases follow semantic
versioning of the `pkg/` public surface.

## [Unreleased]

### Added
- Provider-agnostic agent loop (`pkg/agent`) with pluggable middleware:
  budget, human-in-the-loop approval, prompt-injection guard, secret
  redaction, image retention, and telemetry.
- Canonical action/domain model (`pkg/action`), model seam (`pkg/model`),
  driver seam (`pkg/computer`), set-of-marks seam (`pkg/grounder`), trajectory
  schema and recorders (`pkg/trajectory`), and sandbox contract (`pkg/sandbox`)
  — each with a test fake.
- Providers: native Anthropic computer-use, an OpenAI-compatible adapter, a
  grounding Clicker, and an always-on action normalizer with malformed-call
  repair.
- Drivers: CGo-free X11 `shell` driver, `robotgo` native backend (build-tagged),
  and a `remote` driver over the guest transport.
- Set-of-marks: pure-Go numbered overlay, an OmniParser service client with a
  circuit breaker, an accessibility-tree detector, and an ax→vision chain.
- MCP tool server (`argus-mcp`), the in-sandbox `guestd` server with an
  authenticated/rate-limited/audited transport, and host/docker sandbox
  providers.
- Layered config, the `argus` CLI (`run`/`doctor`/`eval`/`--dry-run`), a disk
  trajectory recorder with secret masking, and an eval harness.
- CI: vet, staticcheck, `-race` tests, a CGO_ENABLED=0 cross-OS build matrix, a
  macOS robotgo build job, secret/brand hygiene gates, and a dependency
  license scan.
