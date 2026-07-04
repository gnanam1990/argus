package codex

import "encoding/json"

// OpenAI Responses API wire types (the subset the Codex backend uses).

type responsesRequest struct {
	Model           string          `json:"model"`
	Instructions    string          `json:"instructions,omitempty"`
	Input           []any           `json:"input"`
	Stream          bool            `json:"stream"`
	Store           bool            `json:"store"`
	MaxOutputTokens int             `json:"max_output_tokens,omitempty"`
	Tools           []responsesTool `json:"tools,omitempty"`
	Reasoning       *reasoningCfg   `json:"reasoning,omitempty"`
}

type reasoningCfg struct {
	Effort  string `json:"effort"`
	Summary string `json:"summary,omitempty"`
}

// responsesTool is a flat function tool (NOT nested under "function" like chat
// completions).
type responsesTool struct {
	Type        string `json:"type"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Parameters  any    `json:"parameters"`
}

// sseEvent is one decoded Server-Sent Event from the Responses stream.
type sseEvent struct {
	Type        string          `json:"type"`
	Delta       string          `json:"delta"`
	Item        sseItem         `json:"item"`
	Response    sseResponse     `json:"response"`
	OutputIndex int             `json:"output_index"`
	ItemID      string          `json:"item_id"`
	Error       json.RawMessage `json:"error"`
}

type sseItem struct {
	ID     string `json:"id"`
	Type   string `json:"type"`
	Name   string `json:"name"`
	CallID string `json:"call_id"`
}

type sseResponse struct {
	Usage struct {
		InputTokens       int `json:"input_tokens"`
		OutputTokens      int `json:"output_tokens"`
		InputTokenDetails struct {
			CachedTokens int `json:"cached_tokens"`
		} `json:"input_tokens_details"`
	} `json:"usage"`
}

// computerTool mirrors the emulated single computer tool the compat adapter
// exposes, so normalize.OpenAI decodes both identically. Kept in sync manually.
var computerTool = responsesTool{
	Type:        "function",
	Name:        "computer",
	Description: "Perform a single UI action on the computer. Pick one action per call.",
	Parameters: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"action": map[string]any{
				"type": "string",
				"enum": []string{
					"click", "right_click", "middle_click", "double_click",
					"move", "type", "key", "scroll", "wait", "screenshot", "terminate",
				},
			},
			"x":       map[string]any{"type": "integer"},
			"y":       map[string]any{"type": "integer"},
			"text":    map[string]any{"type": "string"},
			"keys":    map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
			"dx":      map[string]any{"type": "integer"},
			"dy":      map[string]any{"type": "integer"},
			"seconds": map[string]any{"type": "number"},
		},
		"required": []string{"action"},
	},
}
