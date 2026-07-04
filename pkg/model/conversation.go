package model

import "github.com/gnanam1990/argus/pkg/action"

// Role identifies who authored a message.
type Role int

const (
	// RoleSystem is reserved; the system prompt lives on Conversation.System.
	RoleSystem Role = iota
	RoleUser
	RoleAssistant
	// RoleTool carries the results of executed actions back to the model.
	RoleTool
)

// String returns the lowercase role name.
func (r Role) String() string {
	switch r {
	case RoleSystem:
		return "system"
	case RoleUser:
		return "user"
	case RoleAssistant:
		return "assistant"
	case RoleTool:
		return "tool"
	default:
		return "unknown"
	}
}

// ContentKind discriminates the content part union.
type ContentKind int

const (
	// KindText is a plain text part.
	KindText ContentKind = iota
	// KindImage is a screenshot supplied to the model.
	KindImage
	// KindActionUse is an assistant request to perform a canonical action.
	KindActionUse
	// KindActionResult feeds the outcome of an action back to the model,
	// typically with a fresh screenshot.
	KindActionResult
)

// String returns the content-kind name.
func (k ContentKind) String() string {
	switch k {
	case KindText:
		return "text"
	case KindImage:
		return "image"
	case KindActionUse:
		return "action_use"
	case KindActionResult:
		return "action_result"
	default:
		return "unknown"
	}
}

// Content is one part of a message. Only the fields relevant to Kind are
// populated. CallID correlates a KindActionUse with its KindActionResult, the
// neutral analogue of a provider's tool-use id.
type Content struct {
	Kind   ContentKind   `json:"kind"`
	Text   string        `json:"text,omitempty"`
	Image  action.Image  `json:"image,omitempty"`
	Action action.Action `json:"action,omitempty"`
	Result action.Result `json:"result,omitempty"`
	CallID string        `json:"call_id,omitempty"`
}

// Text returns a text content part.
func Text(s string) Content { return Content{Kind: KindText, Text: s} }

// ImageContent returns an image content part.
func ImageContent(img action.Image) Content { return Content{Kind: KindImage, Image: img} }

// ActionUse returns an assistant action-request part.
func ActionUse(callID string, a action.Action) Content {
	return Content{Kind: KindActionUse, Action: a, CallID: callID}
}

// ActionResult returns a tool action-result part.
func ActionResult(callID string, r action.Result) Content {
	return Content{Kind: KindActionResult, Result: r, CallID: callID}
}

// Message is a single conversational turn's content, authored by one role.
type Message struct {
	Role    Role      `json:"role"`
	Content []Content `json:"content"`
}

// UserMessage builds a user message from content parts.
func UserMessage(c ...Content) Message { return Message{Role: RoleUser, Content: c} }

// AssistantMessage builds an assistant message from content parts.
func AssistantMessage(c ...Content) Message { return Message{Role: RoleAssistant, Content: c} }

// ToolMessage builds a tool (action-result) message from content parts.
func ToolMessage(c ...Content) Message { return Message{Role: RoleTool, Content: c} }

// Conversation is the full exchange handed to a provider on each Step. System
// is the system prompt; Messages is the ordered history.
type Conversation struct {
	System   string    `json:"system,omitempty"`
	Messages []Message `json:"messages"`
}

// Add appends a message to the history.
func (c *Conversation) Add(m Message) { c.Messages = append(c.Messages, m) }

// AddUser appends a user message built from the given content.
func (c *Conversation) AddUser(content ...Content) { c.Add(UserMessage(content...)) }

// AddAssistant appends an assistant message built from the given content.
func (c *Conversation) AddAssistant(content ...Content) { c.Add(AssistantMessage(content...)) }

// AddTool appends a tool (action-result) message built from the given content.
func (c *Conversation) AddTool(content ...Content) { c.Add(ToolMessage(content...)) }

// Len returns the number of messages in the history.
func (c *Conversation) Len() int { return len(c.Messages) }

// Clone returns a deep copy safe to retain across further mutation of the
// original — the loop keeps appending to a conversation, so recorders and
// middleware snapshot it via Clone rather than aliasing its slices.
func (c *Conversation) Clone() *Conversation {
	if c == nil {
		return nil
	}
	out := &Conversation{System: c.System}
	if c.Messages == nil {
		return out
	}
	out.Messages = make([]Message, len(c.Messages))
	for i, m := range c.Messages {
		cm := Message{Role: m.Role}
		if m.Content != nil {
			cm.Content = make([]Content, len(m.Content))
			for j, part := range m.Content {
				cm.Content[j] = cloneContent(part)
			}
		}
		out.Messages[i] = cm
	}
	return out
}

func cloneContent(c Content) Content {
	c.Image.Data = cloneBytes(c.Image.Data)
	c.Result.Screenshot.Data = cloneBytes(c.Result.Screenshot.Data)
	c.Result.Data = cloneBytes(c.Result.Data)
	c.Action.Keys = cloneStrings(c.Action.Keys)
	c.Action.Path = clonePoints(c.Action.Path)
	return c
}

func cloneBytes(b []byte) []byte {
	if b == nil {
		return nil
	}
	out := make([]byte, len(b))
	copy(out, b)
	return out
}

func cloneStrings(s []string) []string {
	if s == nil {
		return nil
	}
	out := make([]string, len(s))
	copy(out, s)
	return out
}

func clonePoints(p []action.Point) []action.Point {
	if p == nil {
		return nil
	}
	out := make([]action.Point, len(p))
	copy(out, p)
	return out
}
