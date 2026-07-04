# Providers

Argus is provider-agnostic: the agent loop depends only on a neutral
`model.Provider` seam, and each adapter normalizes its raw computer-tool actions
into the canonical action schema. The API key for every provider is read from
the environment only.

| `provider.kind` | Adapter | API key env | Notes |
|---|---|---|---|
| `anthropic` | native computer-use | `ANTHROPIC_API_KEY` | Claude with the first-class computer tool (raw screenshots, no grounder needed). |
| `openai` | OpenAI-compatible | `OPENAI_API_KEY` | Chat Completions with an emulated computer function tool; engages set-of-marks grounding. |
| `kimi` | OpenAI-compatible | `MOONSHOT_API_KEY` | Moonshot Kimi (`https://api.moonshot.ai/v1`). |
| `xai` | OpenAI-compatible | `XAI_API_KEY` | xAI Grok (`https://api.x.ai/v1`). OAuth-subscription auth is planned. |
| `ollama` | OpenAI-compatible | `OLLAMA_API_KEY` (usually unset) | Local models (`http://localhost:11434/v1`). |
| `compat` | OpenAI-compatible | `ARGUS_API_KEY` | Any other OpenAI-compatible endpoint/router. Requires `base_url`. |

`kimi`, `xai`, `ollama` are convenience presets over the `compat` adapter — each
just fills in a default `base_url` and key env (both overridable). Any other
OpenAI-compatible service (e.g. an OpenRouter endpoint) works via `compat` with
an explicit `base_url`.

```sh
export MOONSHOT_API_KEY=...   && argus run --config examples/config/kimi.json   "..."
export XAI_API_KEY=...        && argus run --config examples/config/xai.json    "..."
argus run --config examples/config/ollama.json "..."   # local, no key
```

## Anthropic

```json
{ "provider": { "kind": "anthropic", "model": "claude-opus-4-8", "max_tokens": 4096 } }
```

The beta, tool, and model version strings are pinned in one place and verified
against the installed SDK. Sampling parameters are omitted (they are rejected on
current Claude models). Native computer use consumes raw screenshots, so
`grounding.mode` should be `none`.

## OpenAI and OpenAI-compatible

```json
{ "provider": { "kind": "compat", "model": "qwen2.5-vl", "base_url": "http://localhost:11434/v1" } }
```

These use an emulated `computer` function tool. Because they have no first-class
computer tool, pair them with a set-of-marks grounder (`grounding.mode` =
`ax`, `omniparser`, or `chain`) so the model picks numbered marks instead of raw
pixels.

## Choosing a grounding mode

| Mode | Detector | When |
|---|---|---|
| `none` | — | Native computer-use providers (Anthropic). |
| `ax` | accessibility tree | Fast, exact, free; native apps. |
| `omniparser` | vision service (GPU) | Canvas/WebGL/Electron surfaces. AGPL weights — see `omniparser.md`. |
| `chain` | ax → vision fallback | Recommended default for emulated providers. |
