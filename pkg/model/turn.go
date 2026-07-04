package model

import (
	"strconv"
	"strings"

	"github.com/gnanam1990/argus/pkg/action"
)

// StopReason explains why a provider ended a Step.
type StopReason int

const (
	// StopUnknown is the zero value.
	StopUnknown StopReason = iota
	// StopEnd means the assistant finished without requesting an action.
	StopEnd
	// StopAction means the assistant requested one or more actions to run.
	StopAction
	// StopMaxTokens means the response was truncated by the token limit.
	StopMaxTokens
)

// String returns the stop-reason name.
func (s StopReason) String() string {
	switch s {
	case StopEnd:
		return "end"
	case StopAction:
		return "action"
	case StopMaxTokens:
		return "max_tokens"
	default:
		return "unknown"
	}
}

// Turn is a provider's response for one Step: the assistant message (which may
// mix reasoning text with action-use parts), why it stopped, and token usage.
type Turn struct {
	Message Message    `json:"message"`
	Stop    StopReason `json:"stop"`
	Usage   Usage      `json:"usage"`
}

// Text returns the concatenation of all text parts in the assistant message,
// joined by newlines — the model's reasoning/output as a single string.
func (t *Turn) Text() string {
	var b strings.Builder
	first := true
	for _, c := range t.Message.Content {
		if c.Kind != KindText {
			continue
		}
		if !first {
			b.WriteByte('\n')
		}
		b.WriteString(c.Text)
		first = false
	}
	return b.String()
}

// ActionUses returns the action-request content parts, in order.
func (t *Turn) ActionUses() []Content {
	var out []Content
	for _, c := range t.Message.Content {
		if c.Kind == KindActionUse {
			out = append(out, c)
		}
	}
	return out
}

// HasActions reports whether the turn requested any actions.
func (t *Turn) HasActions() bool {
	for _, c := range t.Message.Content {
		if c.Kind == KindActionUse {
			return true
		}
	}
	return false
}

// EndTurn builds a terminal assistant turn carrying only text.
func EndTurn(text string, u Usage) *Turn {
	return &Turn{
		Message: AssistantMessage(Text(text)),
		Stop:    StopEnd,
		Usage:   u,
	}
}

// ActionTurn builds an assistant turn that requests the given actions. Each
// action is assigned a stable call id derived from its position ("call-0",
// "call-1", ...) so tests and recorders can correlate results.
func ActionTurn(u Usage, actions ...action.Action) *Turn {
	msg := Message{Role: RoleAssistant}
	for i, a := range actions {
		msg.Content = append(msg.Content, ActionUse(callID(i), a))
	}
	return &Turn{Message: msg, Stop: StopAction, Usage: u}
}

func callID(i int) string {
	return "call-" + strconv.Itoa(i)
}
