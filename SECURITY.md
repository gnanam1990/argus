# Security Policy

Argus executes model-decided actions on a real or sandboxed computer — it types
keystrokes, clicks, runs commands, and reads the screen. That makes its threat
model a first-class concern, not an afterthought.

## Reporting a vulnerability

Please report suspected vulnerabilities privately via GitHub Security Advisories
("Report a vulnerability" on the Security tab) rather than opening a public
issue. Include a description, affected version/commit, and reproduction steps.
You will receive an acknowledgement, and we ask for reasonable time to remediate
before public disclosure.

## Threat model (evolving)

### Trust levels

- **Trusted environment** — the agent drives a machine whose contents and
  network are trusted (a developer's own desktop, an internal CI runner).
- **Untrusted environment** — the agent drives content it did not author (an
  arbitrary web page, a downloaded document). On-screen text is treated as
  **untrusted input**, not instructions.

### Primary risks tracked by the roadmap

- **Prompt injection from on-screen content.** Screen-derived values are tagged
  `Untrusted` at the boundary; an injection-guard middleware inspects them, and
  destructive capabilities are gated. Untrusted runs belong in an isolated
  sandbox, not on the host.
- **Destructive / irreversible actions.** Purchases, deletes, and arbitrary
  command execution route through a human-in-the-loop approval gate; the
  capability allowlist is **off by default**.
- **Credential leakage into artifacts.** Secrets are masked in message history,
  logs, and recorded trajectories/screenshots at capture time.
- **The in-sandbox control server is remote-code-execution-shaped.** The guest
  server binds localhost by default, requires authentication for non-local use,
  runs over TLS/WSS, and keeps a per-command audit log.
- **Secrets never enter the repository.** API keys and `.env` files are read
  from the environment only, never committed (enforced in CI).

This document is a living skeleton and is expanded as the corresponding stages
(approval, injection guard, redaction, transport auth) land.
