// Package compat adapts any OpenAI-compatible Chat Completions endpoint —
// OpenAI itself, a local model server, or an OpenAI-compatible router — to the
// model.Provider seam. It exposes a single emulated "computer" function tool
// and normalizes the model's tool calls into canonical actions, so a computer-
// use agent can be driven by a provider that has no first-class computer tool.
//
// It is self-contained (net/http + encoding/json), so it is fully testable
// against an httptest server with no vendor SDK.
package compat

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"

	"github.com/gnanam1990/argus/internal/provider/normalize"
	"github.com/gnanam1990/argus/pkg/action"
	"github.com/gnanam1990/argus/pkg/model"
)

// Provider is the OpenAI-compatible adapter. One instance drives one session.
type Provider struct {
	mu        sync.Mutex
	http      *http.Client
	baseURL   string
	apiKey    string
	modelID   string
	maxTokens int

	messages []chatMessage
	encoded  int
}

// Option configures a Provider.
type Option func(*Provider)

// WithBaseURL sets the API base URL (default https://api.openai.com/v1).
func WithBaseURL(u string) Option { return func(p *Provider) { p.baseURL = u } }

// WithAPIKey sets the bearer token.
func WithAPIKey(k string) Option { return func(p *Provider) { p.apiKey = k } }

// WithModel sets the model ID.
func WithModel(m string) Option { return func(p *Provider) { p.modelID = m } }

// WithMaxTokens sets the response token cap.
func WithMaxTokens(n int) Option { return func(p *Provider) { p.maxTokens = n } }

// WithHTTPClient overrides the HTTP client.
func WithHTTPClient(c *http.Client) Option { return func(p *Provider) { p.http = c } }

// New builds an OpenAI-compatible provider.
func New(opts ...Option) *Provider {
	p := &Provider{
		http:      http.DefaultClient,
		baseURL:   "https://api.openai.com/v1",
		modelID:   "gpt-4o",
		maxTokens: 4096,
	}
	for _, o := range opts {
		o(p)
	}
	return p
}

var _ model.Provider = (*Provider)(nil)

// Capabilities reports an emulated (non-native) computer tool, so the loop
// engages the set-of-marks grounder.
func (p *Provider) Capabilities() model.Capabilities {
	return model.Capabilities{NativeComputerUse: false, Streaming: false, Vision: true, Grounding: false}
}

// Step encodes new observations, calls the endpoint, appends the assistant
// message to history, and returns the normalized Turn.
func (p *Provider) Step(ctx context.Context, conv *model.Conversation, opts ...model.StepOption) (*model.Turn, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.encodeNew(conv)

	cfg := model.ApplyOptions(opts...)
	maxTok := p.maxTokens
	if cfg.MaxTokens > 0 {
		maxTok = cfg.MaxTokens
	}

	reqBody := chatRequest{
		Model:      p.modelID,
		Messages:   p.messages,
		Tools:      []tool{computerTool},
		ToolChoice: "auto",
		MaxTokens:  maxTok,
	}
	if cfg.Temperature != nil {
		reqBody.Temperature = cfg.Temperature
	}

	resp, err := p.post(ctx, reqBody)
	if err != nil {
		return nil, err
	}
	if len(resp.Choices) == 0 {
		return nil, fmt.Errorf("compat: response had no choices")
	}

	assistant := resp.Choices[0].Message
	assistant.Role = "assistant"
	p.messages = append(p.messages, assistant)
	return decode(resp), nil
}

func (p *Provider) encodeNew(conv *model.Conversation) {
	for i := p.encoded; i < len(conv.Messages); i++ {
		m := conv.Messages[i]
		switch m.Role {
		case model.RoleUser:
			if parts := userParts(m.Content); len(parts) > 0 {
				p.messages = append(p.messages, chatMessage{Role: "user", Content: parts})
			}
		case model.RoleTool:
			for _, c := range m.Content {
				if c.Kind == model.KindActionResult {
					p.messages = append(p.messages, chatMessage{
						Role: "tool", ToolCallID: c.CallID, Content: resultText(c.Result),
					})
				}
			}
		case model.RoleAssistant, model.RoleSystem:
			// Assistant turns are appended natively; system is prepended below.
		}
	}
	p.encoded = len(conv.Messages)

	// Ensure a leading system message reflects conv.System.
	if conv.System != "" && (len(p.messages) == 0 || p.messages[0].Role != "system") {
		p.messages = append([]chatMessage{{Role: "system", Content: conv.System}}, p.messages...)
	}
}

func userParts(content []model.Content) []contentPart {
	var parts []contentPart
	for _, c := range content {
		switch c.Kind {
		case model.KindText:
			parts = append(parts, contentPart{Type: "text", Text: c.Text})
		case model.KindImage:
			parts = append(parts, contentPart{Type: "image_url", ImageURL: &imageURL{URL: dataURL(c.Image)}})
		}
	}
	return parts
}

func dataURL(img action.Image) string {
	mime := img.MIME
	if mime == "" {
		mime = action.MIMEPNG
	}
	return "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(img.Data)
}

func resultText(r action.Result) string {
	if r.Output != "" {
		return r.Output
	}
	if r.Terminated {
		return "terminated"
	}
	return "action completed; see attached screenshot"
}

func (p *Provider) post(ctx context.Context, body chatRequest) (*chatResponse, error) {
	b, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("compat marshal: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/chat/completions", bytes.NewReader(b))
	if err != nil {
		return nil, fmt.Errorf("compat request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if p.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+p.apiKey)
	}

	res, err := p.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("compat: %w", err)
	}
	defer res.Body.Close()

	raw, _ := io.ReadAll(res.Body)
	if res.StatusCode >= 400 {
		return nil, fmt.Errorf("compat api error (status %d): %s", res.StatusCode, string(raw))
	}
	var out chatResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("compat decode: %w", err)
	}
	if out.Error != nil {
		return nil, fmt.Errorf("compat api error: %s", out.Error.Message)
	}
	return &out, nil
}

func decode(resp *chatResponse) *model.Turn {
	choice := resp.Choices[0]
	msg := model.Message{Role: model.RoleAssistant}

	if text, ok := choice.Message.Content.(string); ok && text != "" {
		msg.Content = append(msg.Content, model.Text(text))
	}
	for _, tc := range choice.Message.ToolCalls {
		a, err := normalize.OpenAI([]byte(tc.Function.Arguments))
		if err != nil {
			a = normalize.Repair()
		}
		msg.Content = append(msg.Content, model.ActionUse(tc.ID, a))
	}

	turn := &model.Turn{
		Message: msg,
		Usage:   model.Usage{InputTokens: resp.Usage.PromptTokens, OutputTokens: resp.Usage.CompletionTokens},
	}
	switch choice.FinishReason {
	case "tool_calls":
		turn.Stop = model.StopAction
	case "length":
		turn.Stop = model.StopMaxTokens
	default:
		turn.Stop = model.StopEnd
	}
	return turn
}
