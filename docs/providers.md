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
| `xai` | OpenAI-compatible | `XAI_API_KEY` | xAI Grok (`https://api.x.ai/v1`). Also supports OAuth login (`argus auth login xai`). |
| `gemini` | OpenAI-compatible | `GEMINI_API_KEY` | Google Gemini via its OpenAI-compatible endpoint (`generativelanguage.googleapis.com/v1beta/openai`). Use a vision model (e.g. `gemini-2.5-flash`). |
| `ollama` | OpenAI-compatible | `OLLAMA_API_KEY` (usually unset) | Local models (`http://localhost:11434/v1`). |
| `compat` | OpenAI-compatible | `ARGUS_API_KEY` | Any other OpenAI-compatible endpoint/router. Requires `base_url`. |
| `chatgpt` | ChatGPT Codex (Responses API) | OAuth (`argus auth login chatgpt`) | Subscription login via the Codex backend — see [oauth-subscriptions.md](oauth-subscriptions.md). |

## Subscription OAuth (xai, chatgpt)

`xai` and `chatgpt` can authenticate with an OAuth subscription login instead of
an API key:

```sh
export ARGUS_OAUTH_ALLOW_PRESETS=1   # opt-in (see the ToS caveat)
argus auth login chatgpt             # or: argus auth login xai
argus run --config examples/config/chatgpt.json "..."
```

`xai` uses the OAuth token as a plain Bearer over the compat adapter; `chatgpt`
uses a dedicated adapter against the Codex Responses backend. Details and the
ToS caveat: [oauth-subscriptions.md](oauth-subscriptions.md).

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
computer tool, either run a **vision model** and let it pick pixel coordinates
from the screenshot (`grounding.mode: none`), or pair a text model with a
set-of-marks grounder (`omniparser` / `chain`) so it picks numbered marks.

## Ollama (local, no API key)

Ollama exposes an OpenAI-compatible API at `http://localhost:11434/v1`, so it
works through the `ollama` preset with no key. Use a **vision** model — computer
use needs the model to see the screen.

```sh
# 1. Install and start the server (https://ollama.com/download)
ollama serve                     # or the menubar app on macOS

# 2. Pull a vision-capable model
ollama pull qwen2.5vl            # or: llama3.2-vision, minicpm-v

# 3. Point argus at it (no key needed)
argus run --tui --config examples/config/ollama.json "open Notes and type hello"
```

`examples/config/ollama.json` uses `model: qwen2.5vl`, `base_url:
http://localhost:11434/v1`, and `grounding.mode: none` (the model reads the raw
screenshot and emits coordinates). To use a different local model, edit `model`;
to reach Ollama on another host/port, set `base_url` in the config or the
`ARGUS_BASE_URL` env var. `OLLAMA_API_KEY` is normally unset; Ollama ignores the
bearer token.

> Local vision models are smaller than frontier models — expect to raise
> `agent.max_steps` and to babysit early runs. For the most reliable control on
> macOS, the Anthropic native computer-use provider is still the strongest.

## Choosing a grounding mode

| Mode | Detector | When |
|---|---|---|
| `none` | — | Native computer-use providers (Anthropic), **and vision models** (e.g. `qwen2.5vl` on Ollama) that read the screenshot and emit pixel coordinates directly. |
| `ax` | accessibility tree | Fast, exact, free; native apps. **macOS: works today** via the System Events tree (see below). Linux/Windows backends are not implemented yet. |
| `omniparser` | vision service (GPU) | Canvas/WebGL/Electron surfaces. AGPL weights — see `omniparser.md`. |
| `chain` | ax → vision fallback | Best for emulated providers: exact tree hits when available, vision otherwise. |

> **`ax` on macOS.** The detector walks the frontmost app's accessibility tree
> (bounded depth/size, ~5s budget) and scales element frames from screen points
> to screenshot pixels, so marks line up on Retina displays. It requires the
> **Accessibility** permission for your terminal (System Settings → Privacy &
> Security → Accessibility — this is separate from Screen Recording and from
> Automation), then a terminal restart. Without it — and on Linux/Windows,
> where no tree source is implemented yet — `ax` reports *unavailable*: the
> `chain` mode falls back to vision, while plain `ax` mode fails the run. The
> emulated-provider examples ship with `grounding.mode: none`, which needs no
> permission and no service.
