# OAuth subscription logins

Argus can authenticate some providers with an OAuth **subscription** login
instead of an API key — currently **xAI (Grok)** and **OpenAI/ChatGPT** (Codex
backend). This lets you drive the agent with a Plus/Pro-style subscription.

## ⚠️ Read this first (ToS / unofficial APIs)

These logins reuse **public, undocumented CLI client identities** (the Codex CLI
and Grok CLI OAuth clients). They are **not** officially-documented developer
APIs, can change or be revoked without notice, and may **violate the provider's
terms of service** if used outside their sanctioned client. The stable,
ToS-safe path for automation remains a plain **API key** (`kind: openai` /
`kind: xai` + the key env). Argus does **not** implement Anthropic subscription
OAuth — that is prohibited outside Claude Code / claude.ai.

Because of this, OAuth presets are **opt-in**:

```sh
export ARGUS_OAUTH_ALLOW_PRESETS=1
```

## Login

```sh
argus auth login xai         # browser (loopback) flow
argus auth login chatgpt     # browser (loopback) flow, callback port 1455 by default (override below)
argus auth login xai --device        # headless device-code flow (where supported)
argus auth login xai --no-browser    # print the URL instead of opening a browser
argus auth status            # show which providers are logged in (redacted)
argus auth logout xai
```

Then select the provider in your config as usual (`kind: xai` or `kind: chatgpt`)
— Argus reads the token from the OAuth store instead of the key env.

## Security

- Tokens are stored **AES-256-GCM encrypted** under
  `$ARGUS_OAUTH_HOME` (default `<user config dir>/argus/oauth/`), with the token
  and secret files at `0600` and the directory at `0700`.
- Tokens are **never logged** and are masked in recorded trajectories.
- PKCE **S256** is mandatory; the token endpoint must be HTTPS.
- The refresh token is single-flight-protected so a rotating token is not spent
  twice concurrently.

## Overrides

Every preset field is overridable per provider, so you can point at a proxy or
supply your own client id without a code change:

```
ARGUS_OAUTH_<PROVIDER>_CLIENT_ID
ARGUS_OAUTH_<PROVIDER>_CLIENT_SECRET
ARGUS_OAUTH_<PROVIDER>_AUTH_URL
ARGUS_OAUTH_<PROVIDER>_TOKEN_URL
ARGUS_OAUTH_<PROVIDER>_DEVICE_URL
ARGUS_OAUTH_<PROVIDER>_SCOPES         # space-separated
ARGUS_OAUTH_<PROVIDER>_REDIRECT_PORT  # loopback callback port (integer; invalid values are ignored)
ARGUS_OAUTH_HOME                       # token store directory
```

`<PROVIDER>` ∈ `CHATGPT | XAI`.

## Notes

- The ChatGPT backend is bot-protected; a headless host may be challenged. The
  provider adapter (which sends the token to `chatgpt.com/backend-api/codex`)
  ships in a follow-up stage.
- xAI is a standard OpenAI-compatible API, so its OAuth token is used as a plain
  `Authorization: Bearer` against `https://api.x.ai/v1` — no special adapter.
