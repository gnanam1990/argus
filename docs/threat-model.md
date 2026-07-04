# Threat model

Argus executes model-decided actions on a real or sandboxed computer — it types
keystrokes, clicks, and (when enabled) runs commands. Its safety controls are
delivered as middleware and transport policy, not bolted on.

## Trust levels

- **Trusted environment** — a developer's own desktop or an internal runner.
  On-screen content is authored by you.
- **Untrusted environment** — arbitrary web pages, downloaded documents. Run
  these in an **isolated sandbox** (container/microVM), never on the host.

On-screen text is treated as **untrusted input**, not instructions.

## Controls

| Risk | Control |
|---|---|
| Prompt injection from on-screen content | Screen-derived values are tagged `Untrusted`; the injection-guard middleware flags/denies sensitive actions whose values derive from untrusted content. |
| Destructive / irreversible actions | Gated capabilities (`run_command`, file ops, window ops) are **off by default**; enabling them routes each call through a human-in-the-loop approval gate. |
| Credential leakage into artifacts | The redaction middleware masks known secrets in conversations; the disk recorder masks reasoning text and typed action text before writing. |
| The in-sandbox control server is RCE-shaped | `guestd` binds localhost by default; a routable bind requires `ARGUS_GUEST_TOKEN` bearer auth (checked on every endpoint, constant-time compare). Transport is rate-limited and per-command audited. Set `ARGUS_GUEST_TLS_CERT`/`ARGUS_GUEST_TLS_KEY` to serve TLS; a non-loopback bind without them serves cleartext and guestd logs a prominent warning — prefer a TLS-terminating proxy or SSH tunnel if you can't set those. |
| Container escape (untrusted runs) | Shared-kernel Docker is not a strong boundary — choose the substrate by trust level (microVM/gVisor for untrusted). |
| Budget runaway | Token and USD budgets halt the run at the next checkpoint. |
| Secrets in the repository | API keys and `.env` files are never committed; secrets are read from the environment only, enforced in CI. |

## Reporting

See [SECURITY.md](../SECURITY.md) for private vulnerability disclosure.
