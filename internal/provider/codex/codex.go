package codex

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gnanam1990/argus/internal/provider/normalize"
	"github.com/gnanam1990/argus/pkg/action"
	"github.com/gnanam1990/argus/pkg/model"
)

// defaultBaseURL is the ChatGPT Codex backend (Responses API base).
const defaultBaseURL = "https://chatgpt.com/backend-api/codex"

// TokenSource returns a fresh access token and account id for the request.
type TokenSource func(ctx context.Context) (access, account string, err error)

// Provider is the ChatGPT/Codex adapter. One instance drives one session.
type Provider struct {
	mu             sync.Mutex
	http           *http.Client
	baseURL        string
	modelID        string
	reasoning      string
	token          TokenSource
	force          TokenSource // force-refresh on a hard 401 (optional)
	imageRetention int

	input   []any
	encoded int
}

// Option configures a Provider.
type Option func(*Provider)

// WithBaseURL overrides the Codex base URL.
func WithBaseURL(u string) Option { return func(p *Provider) { p.baseURL = u } }

// WithModel sets the model ID.
func WithModel(m string) Option { return func(p *Provider) { p.modelID = m } }

// WithReasoningEffort sets the reasoning effort ("" omits it).
func WithReasoningEffort(e string) Option { return func(p *Provider) { p.reasoning = e } }

// WithHTTPClient overrides the HTTP client.
func WithHTTPClient(c *http.Client) Option { return func(p *Provider) { p.http = c } }

// WithTokenSource injects the OAuth credential fetch.
func WithTokenSource(fn TokenSource) Option { return func(p *Provider) { p.token = fn } }

// WithForceRefresh injects the force-refresh used on a hard 401.
func WithForceRefresh(fn TokenSource) Option { return func(p *Provider) { p.force = fn } }

// WithImageRetention bounds the private wire history to the newest n
// screenshots; older ones are replaced with a text placeholder (see
// pruneImages). n <= 0 (the default) keeps every screenshot ever taken,
// preserving prior behavior.
func WithImageRetention(n int) Option { return func(p *Provider) { p.imageRetention = n } }

// New builds a Codex provider.
func New(opts ...Option) *Provider {
	p := &Provider{http: http.DefaultClient, baseURL: defaultBaseURL, modelID: "gpt-5.5"}
	for _, o := range opts {
		o(p)
	}
	return p
}

var _ model.Provider = (*Provider)(nil)

// Capabilities reports an emulated computer tool, so the loop engages grounding.
func (p *Provider) Capabilities() model.Capabilities {
	return model.Capabilities{NativeComputerUse: false, Streaming: true, Vision: true}
}

// Step encodes new observations, calls the Codex Responses endpoint, and returns
// the normalized Turn.
func (p *Provider) Step(ctx context.Context, conv *model.Conversation, opts ...model.StepOption) (*model.Turn, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.encodeNew(conv)
	p.pruneImages()

	// NOTE: the ChatGPT Codex backend rejects max_output_tokens
	// ("Unsupported parameter"), unlike the API-key Responses endpoint, so the
	// configured cap is intentionally not sent (StepOption MaxTokens included).
	reqBody := responsesRequest{
		Model:        p.modelID,
		Instructions: conv.System,
		Input:        p.input,
		Stream:       true,
		Store:        false,
		Tools:        []responsesTool{computerTool},
	}
	if p.reasoning != "" {
		reqBody.Reasoning = &reasoningCfg{Effort: p.reasoning, Summary: "auto"}
	}
	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("codex marshal: %w", err)
	}

	turn, items, err := p.send(ctx, body, false)
	if err != nil {
		return nil, err
	}
	p.input = append(p.input, items...)
	return turn, nil
}

// send performs the request (with one 401 force-refresh retry) and decodes it.
func (p *Provider) send(ctx context.Context, body []byte, retried bool) (*model.Turn, []any, error) {
	access, account, err := p.token(ctx)
	if err != nil {
		return nil, nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/responses", bytes.NewReader(body))
	if err != nil {
		return nil, nil, fmt.Errorf("codex request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+access)
	if account != "" {
		req.Header.Set("chatgpt-account-id", account)
	}
	req.Header.Set("originator", "codex_cli_rs")
	req.Header.Set("User-Agent", "argus")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")

	res, err := p.http.Do(req)
	if err != nil {
		return nil, nil, fmt.Errorf("codex: %w", err)
	}
	defer res.Body.Close()

	if res.StatusCode == http.StatusUnauthorized && !retried && p.force != nil {
		if _, _, ferr := p.force(ctx); ferr == nil {
			return p.send(ctx, body, true)
		}
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		if msg := readErrorBody(res.Body); msg != "" {
			return nil, nil, fmt.Errorf("codex api error (status %d): %s", res.StatusCode, msg)
		}
		return nil, nil, fmt.Errorf("codex api error (status %d)", res.StatusCode)
	}
	return decodeStream(res.Body)
}

func (p *Provider) encodeNew(conv *model.Conversation) {
	for i := p.encoded; i < len(conv.Messages); i++ {
		m := conv.Messages[i]
		switch m.Role {
		case model.RoleUser:
			if parts := inputContent(m.Content); len(parts) > 0 {
				p.input = append(p.input, map[string]any{"type": "message", "role": "user", "content": parts})
			}
		case model.RoleTool:
			for _, c := range m.Content {
				if c.Kind == model.KindActionResult {
					p.input = append(p.input, map[string]any{
						"type": "function_call_output", "call_id": c.CallID, "output": resultText(c.Result),
					})
				}
			}
		case model.RoleAssistant, model.RoleSystem:
			// Assistant items are appended natively after each Step; system → instructions.
		}
	}
	p.encoded = len(conv.Messages)
}

// prunedImagePlaceholder replaces a pruned screenshot's content part.
const prunedImagePlaceholder = "[screenshot pruned]"

// pruneImages replaces all but the newest imageRetention input_image parts in
// the private history with a small input_text placeholder, oldest first, so
// the request about to be built from p.input stays bounded instead of
// resending every screenshot ever taken (it runs right after encodeNew,
// before the request is constructed). Only "message"/"user" items are
// scanned: function_call and function_call_output items never carry images,
// so call_id pairing and item counts are unaffected. imageRetention <= 0
// keeps everything (default; preserves prior behavior).
func (p *Provider) pruneImages() {
	if p.imageRetention <= 0 {
		return
	}
	total := 0
	for _, it := range p.input {
		item, ok := it.(map[string]any)
		if !ok || item["type"] != "message" || item["role"] != "user" {
			continue
		}
		content, ok := item["content"].([]map[string]any)
		if !ok {
			continue
		}
		for _, part := range content {
			if part["type"] == "input_image" {
				total++
			}
		}
	}
	drop := total - p.imageRetention
	if drop <= 0 {
		return
	}
	pruned := 0
	for _, it := range p.input {
		if pruned >= drop {
			break
		}
		item, ok := it.(map[string]any)
		if !ok || item["type"] != "message" || item["role"] != "user" {
			continue
		}
		content, ok := item["content"].([]map[string]any)
		if !ok {
			continue
		}
		for j, part := range content {
			if pruned >= drop {
				break
			}
			if part["type"] != "input_image" {
				continue
			}
			content[j] = map[string]any{"type": "input_text", "text": prunedImagePlaceholder}
			pruned++
		}
	}
}

func inputContent(content []model.Content) []map[string]any {
	var parts []map[string]any
	for _, c := range content {
		switch c.Kind {
		case model.KindText:
			parts = append(parts, map[string]any{"type": "input_text", "text": c.Text})
		case model.KindImage:
			parts = append(parts, map[string]any{"type": "input_image", "image_url": dataURL(c.Image)})
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

// readErrorBody drains up to 2 KiB of an error response without risking a
// hang: the backend can hold an SSE error connection open past the error
// payload, so the read is abandoned after a short grace period and whatever
// arrived is returned (the deferred Body.Close unblocks the goroutine).
func readErrorBody(r io.Reader) string {
	var mu sync.Mutex
	var buf bytes.Buffer
	done := make(chan struct{})
	go func() {
		defer close(done)
		b := make([]byte, 512)
		for buf.Len() < 2048 {
			n, err := r.Read(b)
			if n > 0 {
				mu.Lock()
				buf.Write(b[:n])
				mu.Unlock()
			}
			if err != nil {
				return
			}
		}
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
	}
	mu.Lock()
	defer mu.Unlock()
	return strings.TrimSpace(buf.String())
}

// call accumulates a streamed function_call.
type call struct {
	callID string
	args   strings.Builder
}

// decodeStream reads the Responses SSE stream into a Turn plus the assistant
// output items to append to history (raw, so no re-normalization is needed).
func decodeStream(r io.Reader) (*model.Turn, []any, error) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)

	var text strings.Builder
	calls := map[string]*call{}
	var order []string
	var usage model.Usage
	completed := false

	for sc.Scan() {
		line := sc.Text()
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if payload == "" || payload == "[DONE]" {
			continue
		}
		var ev sseEvent
		if err := json.Unmarshal([]byte(payload), &ev); err != nil {
			continue
		}
		switch ev.Type {
		case "response.output_text.delta":
			text.WriteString(ev.Delta)
		case "response.output_item.added":
			if ev.Item.Type == "function_call" {
				key := ev.ItemID
				if key == "" {
					key = ev.Item.ID
				}
				cid := ev.Item.CallID
				if cid == "" {
					cid = ev.Item.ID
				}
				calls[key] = &call{callID: cid}
				order = append(order, key)
			}
		case "response.function_call_arguments.delta":
			if c := calls[ev.ItemID]; c != nil {
				c.args.WriteString(ev.Delta)
			}
		case "response.completed":
			usage = model.Usage{
				InputTokens:     ev.Response.Usage.InputTokens,
				OutputTokens:    ev.Response.Usage.OutputTokens,
				CacheReadTokens: ev.Response.Usage.InputTokenDetails.CachedTokens,
			}
			completed = true
		case "response.failed", "response.error":
			return nil, nil, fmt.Errorf("codex: response failed")
		}
	}
	if err := sc.Err(); err != nil {
		return nil, nil, fmt.Errorf("codex stream: %w", err)
	}

	msg := model.Message{Role: model.RoleAssistant}
	var items []any
	if t := text.String(); t != "" {
		msg.Content = append(msg.Content, model.Text(t))
		items = append(items, map[string]any{
			"type": "message", "role": "assistant",
			"content": []map[string]any{{"type": "output_text", "text": t}},
		})
	}
	for _, key := range order {
		c := calls[key]
		args := c.args.String()
		a, err := normalize.OpenAI([]byte(args))
		if err != nil {
			a = normalize.Repair()
		}
		msg.Content = append(msg.Content, model.ActionUse(c.callID, a))
		items = append(items, map[string]any{
			"type": "function_call", "call_id": c.callID, "name": "computer", "arguments": args,
		})
	}

	turn := &model.Turn{Message: msg, Usage: usage}
	switch {
	case len(order) > 0:
		turn.Stop = model.StopAction
	case completed:
		turn.Stop = model.StopEnd
	default:
		turn.Stop = model.StopMaxTokens
	}
	return turn, items, nil
}
