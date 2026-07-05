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
	"sort"
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
	p := &Provider{http: defaultHTTPClient(), baseURL: defaultBaseURL, modelID: "gpt-5.5"}
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
// unset because it would also bound the body read, and the Responses backend
// streams a long-lived SSE body that can legitimately take a while.
func defaultHTTPClient() *http.Client {
	return &http.Client{
		Transport: &http.Transport{
			// A reasoning model on a large context can take a while to emit the
			// first response header (it reasons before streaming), so this is
			// generous — it only guards against a truly wedged connection, not
			// slow-but-progressing generation.
			ResponseHeaderTimeout: 120 * time.Second,
			Proxy:                 http.ProxyFromEnvironment,
		},
	}
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
	p.pruneReasoning()

	// NOTE: the ChatGPT Codex backend rejects max_output_tokens
	// ("Unsupported parameter"), unlike the API-key Responses endpoint, so the
	// configured cap is intentionally not sent (StepOption MaxTokens included).
	// include asks for reasoning items WITH their encrypted content: under
	// store:false the backend persists nothing, so a replayed reasoning item
	// is only valid when it carries encrypted_content — a bare rs_… id gets
	// "Item ... not found".
	reqBody := responsesRequest{
		Model:        p.modelID,
		Instructions: conv.System,
		Input:        p.input,
		Stream:       true,
		Store:        false,
		Include:      []string{"reasoning.encrypted_content"},
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
	if p.encoded > len(conv.Messages) {
		// The conversation is shorter than what we've already encoded: this is
		// not "no new messages", it's a different (or reset) conversation —
		// e.g. a second Run reusing the same provider instance. Resending the
		// stale private history would either replay a finished task's actions
		// or desync call_id pairing against the new conversation, so start
		// this adapter's wire history over from scratch.
		p.input = nil
		p.encoded = 0
	}
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

// keepReasoning bounds how many reasoning items are replayed. Each carries a
// large encrypted_content blob; only the most recent turns' reasoning aids
// continuity, and older turns' function_calls are already resolved by their
// tool results, so keeping them all just inflates every subsequent request
// (a long run otherwise grows to six figures of tokens).
const keepReasoning = 6

// pruneReasoning drops all but the newest keepReasoning reasoning items from
// the private history. Reasoning items are standalone (no call_id pairing), so
// removing old ones cannot orphan a function_call/function_call_output pair.
func (p *Provider) pruneReasoning() {
	total := 0
	for _, it := range p.input {
		if item, ok := it.(map[string]any); ok && item["type"] == "reasoning" {
			total++
		}
	}
	drop := total - keepReasoning
	if drop <= 0 {
		return
	}
	out := make([]any, 0, len(p.input))
	dropped := 0
	for _, it := range p.input {
		if item, ok := it.(map[string]any); ok && item["type"] == "reasoning" && dropped < drop {
			dropped++
			continue
		}
		out = append(out, it)
	}
	p.input = out
}

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

// call accumulates a streamed function_call, tagged with its position in the
// response's output array so a sibling reasoning item (see decodeStream) can
// be replayed in the right relative order ahead of it.
type call struct {
	callID string
	args   strings.Builder
	index  int
}

// replayItem is a raw item to splice into the history replayed to the model
// on a later request, tagged with its position in the response's output
// array. Currently built from reasoning items (captured verbatim off
// response.output_item.done) and function_call items (built from the
// accumulated call args), merged and sorted so reasoning correctly precedes
// the sibling function_call it produced.
type replayItem struct {
	index int
	item  any
}

// decodeStream reads the Responses SSE stream into a Turn plus the assistant
// output items to append to history (raw, so no re-normalization is needed).
//
// A stream that ends without a response.completed event is always an error
// (H4): the caller must never receive a turn that looks like a legitimate
// stop (e.g. a token-limit truncation) when the connection simply dropped,
// the backend errored out mid-response, or a per-event decode failed —
// whether or not a function_call had already fully arrived by then.
func decodeStream(r io.Reader) (*model.Turn, []any, error) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)

	var text strings.Builder
	calls := map[string]*call{}
	var order []string
	var reasoning []replayItem
	var usage model.Usage
	completed := false
	decodeFailures := 0

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
			decodeFailures++
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
				calls[key] = &call{callID: cid, index: ev.OutputIndex}
				order = append(order, key)
			}
		case "response.function_call_arguments.delta":
			if c := calls[ev.ItemID]; c != nil {
				c.args.WriteString(ev.Delta)
			}
		case "response.output_item.done":
			// The Responses backend (store:false) expects a reasoning item
			// replayed verbatim alongside its sibling function_call/message in
			// a later request's input. Capture its raw JSON as emitted rather
			// than modeling its (large, backend-specific) shape — but ONLY
			// when it carries encrypted_content: without it the item is just a
			// dangling rs_… reference the stateless backend rejects with
			// "Item ... not found" on the next request.
			if ev.Item.Type == "reasoning" {
				if raw := doneItemJSON(payload); raw != nil {
					var v map[string]any
					if err := json.Unmarshal(raw, &v); err == nil {
						if ec, ok := v["encrypted_content"].(string); ok && ec != "" {
							reasoning = append(reasoning, replayItem{index: ev.OutputIndex, item: v})
						}
					}
				}
			}
		case "response.completed":
			usage = model.Usage{
				InputTokens:     ev.Response.Usage.InputTokens,
				OutputTokens:    ev.Response.Usage.OutputTokens,
				CacheReadTokens: ev.Response.Usage.InputTokenDetails.CachedTokens,
			}
			completed = true
		case "response.failed", "response.error":
			return nil, nil, fmt.Errorf("codex: %s: %s", ev.Type, errorDetail(ev.Error))
		}
	}
	if err := sc.Err(); err != nil {
		return nil, nil, fmt.Errorf("codex stream: %w", err)
	}
	if !completed {
		if decodeFailures > 0 {
			return nil, nil, fmt.Errorf("codex: stream ended before completion (%d event(s) failed to decode)", decodeFailures)
		}
		return nil, nil, fmt.Errorf("codex: stream ended before completion")
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

	callItems := make([]replayItem, 0, len(order))
	for _, key := range order {
		c := calls[key]
		args := c.args.String()
		a, err := normalize.OpenAI([]byte(args))
		if err != nil {
			a = normalize.Repair()
		}
		msg.Content = append(msg.Content, model.ActionUse(c.callID, a))
		callItems = append(callItems, replayItem{index: c.index, item: map[string]any{
			"type": "function_call", "call_id": c.callID, "name": "computer", "arguments": args,
		}})
	}

	// Merge reasoning items with their sibling function_call items by output
	// position (not "all reasoning, then all calls") so a multi-call turn
	// with interleaved reasoning replays in the original order.
	merged := append(append([]replayItem{}, reasoning...), callItems...)
	sort.SliceStable(merged, func(i, j int) bool { return merged[i].index < merged[j].index })
	for _, m := range merged {
		items = append(items, m.item)
	}

	turn := &model.Turn{Message: msg, Usage: usage}
	if len(order) > 0 {
		turn.Stop = model.StopAction
	} else {
		turn.Stop = model.StopEnd
	}
	return turn, items, nil
}

// doneItemJSON extracts the raw "item" object from a
// response.output_item.done SSE payload, so it can be replayed verbatim
// later without argus modeling its full (large, backend-specific) shape.
func doneItemJSON(payload string) json.RawMessage {
	var env struct {
		Item json.RawMessage `json:"item"`
	}
	if err := json.Unmarshal([]byte(payload), &env); err != nil || len(env.Item) == 0 {
		return nil
	}
	return env.Item
}

// errorDetail renders a response.failed/response.error event's error payload
// for the error message, so the underlying reason (e.g. a content-policy
// refusal or an upstream 5xx passed through by the backend) is visible
// instead of a bare "stream failed".
func errorDetail(raw json.RawMessage) string {
	s := strings.TrimSpace(string(raw))
	if s == "" || s == "null" {
		return "(no error payload)"
	}
	return s
}
