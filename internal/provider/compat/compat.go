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
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/gnanam1990/argus/internal/provider/normalize"
	"github.com/gnanam1990/argus/pkg/action"
	"github.com/gnanam1990/argus/pkg/model"
)

// Provider is the OpenAI-compatible adapter. One instance drives one session.
type Provider struct {
	mu             sync.Mutex
	http           *http.Client
	baseURL        string
	apiKey         string
	modelID        string
	maxTokens      int
	imageRetention int

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

// WithImageRetention bounds the private wire history to the newest n
// screenshots; older ones are replaced with a text placeholder (see
// pruneImages). n <= 0 (the default) keeps every screenshot ever taken,
// preserving prior behavior.
func WithImageRetention(n int) Option { return func(p *Provider) { p.imageRetention = n } }

// New builds an OpenAI-compatible provider.
func New(opts ...Option) *Provider {
	p := &Provider{
		http:      defaultHTTPClient(),
		baseURL:   "https://api.openai.com/v1",
		modelID:   "gpt-4o",
		maxTokens: 4096,
	}
	for _, o := range opts {
		o(p)
	}
	return p
}

// defaultHTTPClient is used when WithHTTPClient is not given. http.DefaultClient
// has no timeout at all, and the agent loop's context carries no deadline
// either, so a server that accepts the connection but never answers would
// otherwise wedge the run forever (H5). ResponseHeaderTimeout bounds only the
// wait for the initial response headers; Client.Timeout is deliberately left
// unset because it would also bound the body read, and some compatible
// backends stream long-lived bodies.
func defaultHTTPClient() *http.Client {
	return &http.Client{
		Transport: &http.Transport{
			ResponseHeaderTimeout: 60 * time.Second,
			Proxy:                 http.ProxyFromEnvironment,
		},
	}
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
	p.pruneImages()

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
	}
	if usesMaxCompletionTokens(p.modelID) {
		reqBody.MaxCompletionTokens = maxTok
	} else {
		reqBody.MaxTokens = maxTok
	}
	if cfg.Temperature != nil {
		reqBody.Temperature = cfg.Temperature
	}
	if cfg.Seed != nil {
		reqBody.Seed = cfg.Seed
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
	if p.encoded > len(conv.Messages) {
		// The conversation is shorter than what we've already encoded: this is
		// not "no new messages", it's a different (or reset) conversation —
		// e.g. a second Run reusing the same provider instance. Resending the
		// stale private history would either replay a finished task's actions
		// or desync tool_call_id pairing against the new conversation, so
		// start this adapter's wire history over from scratch.
		p.messages = nil
		p.encoded = 0
	}
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

// prunedImagePlaceholder replaces a pruned screenshot's content part.
const prunedImagePlaceholder = "[screenshot pruned]"

// pruneImages replaces all but the newest imageRetention image_url content
// parts in the private history with a small text placeholder, oldest first,
// so the request about to be built from p.messages stays bounded instead of
// resending every screenshot ever taken (it runs right after encodeNew,
// before the request is constructed). Only user messages carry a
// []contentPart content ([]contentPart is exactly what userParts produces):
// system/tool messages are plain strings and assistant messages are echoed
// verbatim from the API response, so tool_call_id pairing and message counts
// are unaffected. imageRetention <= 0 keeps everything (default; preserves
// prior behavior).
func (p *Provider) pruneImages() {
	if p.imageRetention <= 0 {
		return
	}
	total := 0
	for _, m := range p.messages {
		if m.Role != "user" {
			continue
		}
		parts, ok := m.Content.([]contentPart)
		if !ok {
			continue
		}
		for _, part := range parts {
			if part.Type == "image_url" {
				total++
			}
		}
	}
	drop := total - p.imageRetention
	if drop <= 0 {
		return
	}
	pruned := 0
	for i := range p.messages {
		if pruned >= drop {
			break
		}
		if p.messages[i].Role != "user" {
			continue
		}
		parts, ok := p.messages[i].Content.([]contentPart)
		if !ok {
			continue
		}
		for j := range parts {
			if pruned >= drop {
				break
			}
			if parts[j].Type != "image_url" {
				continue
			}
			parts[j] = contentPart{Type: "text", Text: prunedImagePlaceholder}
			pruned++
		}
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

// newTokenParamModel matches an OpenAI o-series (o1, o3, o4, ...) or gpt-5.x
// model id, after trimming a leading "provider/" prefix.
var newTokenParamModel = regexp.MustCompile(`(?i)^(o[0-9]|gpt-5)`)

// usesMaxCompletionTokens reports whether model expects the newer
// max_completion_tokens request field instead of max_tokens. OpenAI's
// o-series and gpt-5.x reject max_tokens outright ("Unsupported parameter"),
// while older API versions and self-hosted/local servers (e.g. Ollama) still
// expect max_tokens and don't understand max_completion_tokens — so exactly
// one of the two is ever set (see Step). A leading "provider/" prefix (e.g.
// "openai/gpt-5.5", common when the model id flows through a router) is
// trimmed before matching.
func usesMaxCompletionTokens(model string) bool {
	if i := strings.LastIndex(model, "/"); i >= 0 {
		model = model[i+1:]
	}
	return newTokenParamModel.MatchString(model)
}

// resultText renders an action result as the text fed back to the model. A
// cursor_position result carries its answer only in Cursor (Output is
// empty), so without this the model would just see the generic "action
// completed" text and never learn where the cursor actually is. Cursor is
// reported whenever it is non-zero; the accepted limitation is that a real
// cursor position of exactly (0, 0) is indistinguishable from "no cursor
// result" and won't be reported, since Result has no separate "cursor is
// set" bit to check instead.
func resultText(r action.Result) string {
	text := "action completed; see attached screenshot"
	switch {
	case r.Output != "":
		text = r.Output
	case r.Terminated:
		text = "terminated"
	}
	if r.Cursor != (action.Point{}) {
		text += fmt.Sprintf("\ncursor: (%d, %d)", r.Cursor.X, r.Cursor.Y)
	}
	return text
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

	// maxResponseBody bounds how much of a response this adapter will ever
	// buffer into memory: an unbounded io.ReadAll on a misbehaving or
	// malicious endpoint (or one that just streams forever) can OOM the
	// process rather than fail cleanly.
	const maxResponseBody = 16 << 20 // 16 MiB
	raw, err := io.ReadAll(io.LimitReader(res.Body, maxResponseBody))
	if err != nil {
		return nil, fmt.Errorf("compat: read response: %w", err)
	}
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

// contentText resolves an assistant message's Content into its text: either
// the plain-string shape most OpenAI-compatible servers use, or the
// array-of-parts shape ("content":[{"type":"text","text":"..."}, ...]) some
// servers emit instead — which previously failed the string type-assert and
// silently dropped the assistant's entire text.
func contentText(content any) string {
	switch c := content.(type) {
	case string:
		return c
	case []any:
		var b strings.Builder
		for _, p := range c {
			part, ok := p.(map[string]any)
			if !ok {
				continue
			}
			if t, _ := part["text"].(string); t != "" {
				b.WriteString(t)
			}
		}
		return b.String()
	default:
		return ""
	}
}

func decode(resp *chatResponse) *model.Turn {
	choice := resp.Choices[0]
	msg := model.Message{Role: model.RoleAssistant}

	if text := contentText(choice.Message.Content); text != "" {
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
