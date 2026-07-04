package compat

// OpenAI Chat Completions wire types (the subset Argus uses).

type chatRequest struct {
	Model       string        `json:"model"`
	Messages    []chatMessage `json:"messages"`
	Tools       []tool        `json:"tools,omitempty"`
	ToolChoice  string        `json:"tool_choice,omitempty"`
	MaxTokens   int           `json:"max_tokens,omitempty"`
	Temperature *float64      `json:"temperature,omitempty"`
}

// chatMessage.Content is either a string (system/tool) or []contentPart (user).
type chatMessage struct {
	Role       string     `json:"role"`
	Content    any        `json:"content,omitempty"`
	ToolCalls  []toolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
}

type contentPart struct {
	Type     string    `json:"type"`
	Text     string    `json:"text,omitempty"`
	ImageURL *imageURL `json:"image_url,omitempty"`
}

type imageURL struct {
	URL string `json:"url"`
}

type toolCall struct {
	ID       string       `json:"id"`
	Type     string       `json:"type"`
	Function functionCall `json:"function"`
}

type functionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type tool struct {
	Type     string       `json:"type"`
	Function toolFunction `json:"function"`
}

type toolFunction struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Parameters  any    `json:"parameters"`
}

type chatResponse struct {
	Choices []struct {
		Message      chatMessage `json:"message"`
		FinishReason string      `json:"finish_reason"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
	} `json:"usage"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

// computerTool is the emulated function tool the model calls to act. Its
// argument schema is normalized by internal/provider/normalize.OpenAI.
var computerTool = tool{
	Type: "function",
	Function: toolFunction{
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
					"description": "The action to perform.",
				},
				"x":       map[string]any{"type": "integer", "description": "Target X in screenshot pixels."},
				"y":       map[string]any{"type": "integer", "description": "Target Y in screenshot pixels."},
				"text":    map[string]any{"type": "string", "description": "Text to type (for action=type)."},
				"keys":    map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Key chord (for action=key)."},
				"dx":      map[string]any{"type": "integer", "description": "Horizontal scroll amount."},
				"dy":      map[string]any{"type": "integer", "description": "Vertical scroll amount (positive = down)."},
				"seconds": map[string]any{"type": "number", "description": "Seconds to wait (for action=wait)."},
			},
			"required":             []string{"action"},
			"additionalProperties": false,
		},
	},
}
