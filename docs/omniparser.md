# OmniParser grounding service

The `omniparser` grounder detects UI elements with a vision model so any
provider can be driven by set-of-marks. It runs **out of process** because it
needs a GPU (~0.6–0.8 s/frame on a modern GPU; 10–30× slower on CPU).

## ⚠️ Licensing

OmniParser's `icon_detect` weights are **AGPL** (network copyleft). Serving them
over a network can trigger source-availability obligations. Argus therefore:

- defaults grounding to the permissive **accessibility-tree** detector, with
  OmniParser opt-in;
- gates any shipped release path with a dependency license scan;
- keeps OmniParser as a separate service you deploy, not a bundled component.

Resolve the AGPL obligation, or swap in a permissively-licensed detector, before
making OmniParser your default in a distributed product.

## The contract

Argus owns and versions a small JSON contract (`SchemaVersion = 2`):

```
POST /parse   {"image": "<base64>", "version": 2}
  -> {"version": 2, "elements": [
        {"id": 0, "box": [x0,y0,x1,y1], "label": "...", "text": "...",
         "interactable": true, "confidence": 0.9}
     ]}

GET  /status  -> 200
```

A response whose `version` differs is rejected, so service drift surfaces
immediately. A consecutive-failure **circuit breaker** wraps the client: after
the threshold, `Detect` fails fast (rather than stalling every step) until a
cooldown elapses — the `chain` grounder then falls back to the accessibility
detector.

## Config

```json
{ "grounding": { "mode": "omniparser", "omniparser_url": "http://localhost:8000", "min_confidence": 0.4 } }
```

Use `mode: "chain"` to try the accessibility tree first and fall back to
OmniParser only for canvas/WebGL/Electron surfaces.
