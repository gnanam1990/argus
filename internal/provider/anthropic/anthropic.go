// Package anthropic adapts the Anthropic Messages API (native computer-use
// tool) to the model.Provider seam. It encodes the neutral conversation into
// SDK message params, calls the beta Messages endpoint with the computer tool,
// and normalizes the returned tool calls into canonical actions.
//
// The adapter keeps an SDK-native message history internally: assistant turns
// are stored via BetaMessage.ToParam() (preserving real tool_use IDs), and only
// new user/tool observations are re-encoded each Step. This avoids reversing the
// action normalization and keeps tool_use↔tool_result pairing intact.
package anthropic

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"sync"

	sdk "github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"

	"github.com/gnanam1990/argus/internal/provider/normalize"
	"github.com/gnanam1990/argus/pkg/action"
	"github.com/gnanam1990/argus/pkg/model"
)

// Pinned API version identifiers. These are load-bearing: a stale beta or tool
// version is a 400 at runtime. Re-confirm against the installed SDK before a
// release; the gated live smoke test fails loudly on drift.
const (
	betaComputerUse = "computer-use-2025-11-24" // tool version: computer_20251124
	defaultModel    = sdk.ModelClaudeOpus4_8
)

// Provider is the Anthropic model.Provider adapter. One instance drives one
// session (it holds that session's SDK-native history).
type Provider struct {
	mu             sync.Mutex
	client         sdk.Client
	clientOpts     []option.RequestOption
	modelID        sdk.Model
	maxTokens      int
	displayW       int
	displayH       int
	imageRetention int

	messages []sdk.BetaMessageParam
	encoded  int // count of neutral messages already reflected in `messages`
}

// Option configures a Provider.
type Option func(*Provider)

// WithModel overrides the model ID.
func WithModel(m string) Option { return func(p *Provider) { p.modelID = sdk.Model(m) } }

// WithMaxTokens sets the per-response token cap.
func WithMaxTokens(n int) Option { return func(p *Provider) { p.maxTokens = n } }

// WithDisplaySize sets the display resolution advertised to the computer tool.
func WithDisplaySize(w, h int) Option {
	return func(p *Provider) { p.displayW, p.displayH = w, h }
}

// WithClientOptions passes request options to the underlying SDK client
// (e.g. option.WithAPIKey, option.WithBaseURL).
func WithClientOptions(opts ...option.RequestOption) Option {
	return func(p *Provider) { p.clientOpts = append(p.clientOpts, opts...) }
}

// WithImageRetention bounds the private wire history to the newest n
// screenshots; older ones are replaced with a text placeholder (see
// pruneImages). n <= 0 (the default) keeps every screenshot ever taken,
// preserving prior behavior.
func WithImageRetention(n int) Option { return func(p *Provider) { p.imageRetention = n } }

// New builds an Anthropic provider. Without WithClientOptions the SDK resolves
// credentials from the environment (ANTHROPIC_API_KEY / profile).
func New(opts ...Option) *Provider {
	p := &Provider{modelID: defaultModel, maxTokens: 4096, displayW: 1280, displayH: 800}
	for _, o := range opts {
		o(p)
	}
	p.client = sdk.NewClient(p.clientOpts...)
	return p
}

// Compile-time check.
var _ model.Provider = (*Provider)(nil)

// Capabilities reports native computer use (raw screenshots, no grounder).
func (p *Provider) Capabilities() model.Capabilities {
	return model.Capabilities{NativeComputerUse: true, Streaming: false, Vision: true}
}

// Step encodes new observations, calls the API, appends the assistant turn to
// the internal history, and returns the normalized Turn.
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

	params := sdk.BetaMessageNewParams{
		Model:     p.modelID,
		MaxTokens: int64(maxTok),
		Tools: []sdk.BetaToolUnionParam{
			sdk.BetaToolUnionParamOfComputerUseTool20251124(int64(p.displayH), int64(p.displayW)),
		},
		Betas: []sdk.AnthropicBeta{betaComputerUse},
	}
	if conv.System != "" {
		params.System = []sdk.BetaTextBlockParam{{Text: conv.System}}
	}

	resp, err := p.call(ctx, params)
	if err != nil {
		return nil, err
	}
	return decode(resp)
}

// call invokes the Messages API and applies the documented pause_turn
// recovery: a paused long-running turn is resumed by resending the request
// with the paused assistant turn appended as-is — see BetaStopReasonPauseTurn
// ("you may provide the response back as-is in a subsequent request to let
// the model continue") — exactly once. A second consecutive pause is
// returned to the caller (decode maps it like any other non-tool-use stop)
// rather than retried forever. Every response, paused or not, is appended to
// the private history so it stays a faithful transcript of what the API
// actually said.
func (p *Provider) call(ctx context.Context, params sdk.BetaMessageNewParams) (*sdk.BetaMessage, error) {
	const maxPauseResumes = 1
	for attempt := 0; ; attempt++ {
		params.Messages = p.messages
		resp, err := p.client.Beta.Messages.New(ctx, params)
		if err != nil {
			return nil, wrapErr(err)
		}
		p.messages = append(p.messages, resp.ToParam())
		if resp.StopReason != sdk.BetaStopReasonPauseTurn || attempt >= maxPauseResumes {
			return resp, nil
		}
	}
}

// encodeNew appends SDK params for neutral messages not yet encoded, skipping
// assistant turns (which are stored natively via ToParam after each Step).
func (p *Provider) encodeNew(conv *model.Conversation) {
	if p.encoded > len(conv.Messages) {
		// The conversation is shorter than what we've already encoded: this is
		// not "no new messages", it's a different (or reset) conversation —
		// e.g. a second Run reusing the same provider instance. Resending the
		// stale private history would either replay a finished task's actions
		// or desync tool_use/tool_result pairing against the new
		// conversation, so start this adapter's wire history over from
		// scratch.
		p.messages = nil
		p.encoded = 0
	}
	for i := p.encoded; i < len(conv.Messages); i++ {
		m := conv.Messages[i]
		switch m.Role {
		case model.RoleUser:
			if blocks := userBlocks(m.Content); len(blocks) > 0 {
				p.messages = append(p.messages, sdk.NewBetaUserMessage(blocks...))
			}
		case model.RoleTool:
			if blocks := toolResultBlocks(m.Content); len(blocks) > 0 {
				p.messages = append(p.messages, sdk.NewBetaUserMessage(blocks...))
			}
		case model.RoleAssistant, model.RoleSystem:
			// Assistant turns are appended natively; system lives on params.System.
		}
	}
	p.encoded = len(conv.Messages)
}

// prunedImagePlaceholder replaces a pruned screenshot's content block.
const prunedImagePlaceholder = "[screenshot pruned]"

// pruneImages replaces all but the newest imageRetention image content blocks
// in the private history with a small text placeholder, oldest first, so the
// request about to be built from p.messages stays bounded instead of resending
// every screenshot ever taken (it runs right after encodeNew, before the
// request is constructed). Only user-role messages are scanned: assistant
// turns are appended natively via resp.ToParam() and never carry image blocks
// in this codebase, and tool_result blocks here are always text (see
// toolResultBlocks) — so tool_use/tool_result pairing and message/block counts
// are unaffected. imageRetention <= 0 keeps everything (default; preserves
// prior behavior).
func (p *Provider) pruneImages() {
	if p.imageRetention <= 0 {
		return
	}
	total := 0
	for _, m := range p.messages {
		if m.Role != sdk.BetaMessageParamRoleUser {
			continue
		}
		for _, b := range m.Content {
			if b.OfImage != nil {
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
		if p.messages[i].Role != sdk.BetaMessageParamRoleUser {
			continue
		}
		for j := range p.messages[i].Content {
			if pruned >= drop {
				break
			}
			if p.messages[i].Content[j].OfImage == nil {
				continue
			}
			p.messages[i].Content[j] = sdk.NewBetaTextBlock(prunedImagePlaceholder)
			pruned++
		}
	}
}

func userBlocks(content []model.Content) []sdk.BetaContentBlockParamUnion {
	var blocks []sdk.BetaContentBlockParamUnion
	for _, c := range content {
		switch c.Kind {
		case model.KindText:
			blocks = append(blocks, sdk.NewBetaTextBlock(c.Text))
		case model.KindImage:
			blocks = append(blocks, imageBlock(c.Image))
		}
	}
	return blocks
}

func toolResultBlocks(content []model.Content) []sdk.BetaContentBlockParamUnion {
	var blocks []sdk.BetaContentBlockParamUnion
	for _, c := range content {
		if c.Kind == model.KindActionResult {
			blocks = append(blocks, sdk.NewBetaToolResultBlock(c.CallID, resultText(c.Result), false))
		}
	}
	return blocks
}

func imageBlock(img action.Image) sdk.BetaContentBlockParamUnion {
	mt := sdk.BetaBase64ImageSourceMediaTypeImagePNG
	if img.MIME == action.MIMEJPEG {
		mt = sdk.BetaBase64ImageSourceMediaTypeImageJPEG
	}
	return sdk.NewBetaImageBlock(sdk.BetaBase64ImageSourceParam{
		Data:      base64.StdEncoding.EncodeToString(img.Data),
		MediaType: mt,
	})
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

// decode converts an API response into a neutral Turn, normalizing each
// tool_use into a canonical action (repairing malformed calls).
//
// A refusal stop reason has no legitimate Turn to build (the SDK's own tool
// runner treats it the same way — see BetaStopReasonRefusal in
// betatoolrunner.go: tool calls before a refusal belong to a dead
// conversation) and is reported as an error instead.
// model_context_window_exceeded is mapped like max_tokens: both mean the
// response was cut short by a length limit, just a different one.
// pause_turn never reaches here as the stop reason of the FINAL response
// call already resent it once (see call); a still-paused response after that
// retry falls through to the default case, i.e. StopEnd.
func decode(resp *sdk.BetaMessage) (*model.Turn, error) {
	if resp.StopReason == sdk.BetaStopReasonRefusal {
		return nil, errors.New("anthropic: model refused the request")
	}

	msg := model.Message{Role: model.RoleAssistant}
	for _, block := range resp.Content {
		switch b := block.AsAny().(type) {
		case sdk.BetaTextBlock:
			msg.Content = append(msg.Content, model.Text(b.Text))
		case sdk.BetaToolUseBlock:
			raw, err := json.Marshal(b.Input)
			var a action.Action
			if err != nil {
				a = normalize.Repair()
			} else if a, err = normalize.Anthropic(raw); err != nil {
				a = normalize.Repair()
			}
			msg.Content = append(msg.Content, model.ActionUse(b.ID, a))
		}
	}

	turn := &model.Turn{Message: msg, Usage: usage(resp.Usage)}
	switch resp.StopReason {
	case sdk.BetaStopReasonToolUse:
		turn.Stop = model.StopAction
	case sdk.BetaStopReasonMaxTokens, sdk.BetaStopReasonModelContextWindowExceeded:
		turn.Stop = model.StopMaxTokens
	default:
		turn.Stop = model.StopEnd
	}
	return turn, nil
}

func usage(u sdk.BetaUsage) model.Usage {
	return model.Usage{
		InputTokens:      int(u.InputTokens),
		OutputTokens:     int(u.OutputTokens),
		CacheReadTokens:  int(u.CacheReadInputTokens),
		CacheWriteTokens: int(u.CacheCreationInputTokens),
	}
}

func wrapErr(err error) error {
	var apierr *sdk.Error
	if errors.As(err, &apierr) {
		return fmt.Errorf("anthropic api error (status %d): %w", apierr.StatusCode, err)
	}
	return fmt.Errorf("anthropic: %w", err)
}
