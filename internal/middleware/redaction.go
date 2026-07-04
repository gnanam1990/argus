package middleware

import (
	"context"
	"strings"

	"github.com/gnanam1990/argus/pkg/agent"
	"github.com/gnanam1990/argus/pkg/model"
)

// Redaction masks known secret values in the conversation's TEXT content before
// each provider call: model-visible text and tool-result output never carry a
// registered secret, and the trajectory recorder masks the same values at
// persist time. Scope, honestly stated: this cannot redact pixels — a secret
// visible INSIDE a screenshot still reaches the vision model and the recorded
// PNGs. Keep secret-bearing windows off-screen while an agent drives, and
// register extra values via ARGUS_SECRETS. It does not touch action-use content
// (the model's intended keystrokes).
type Redaction struct {
	agent.Base
	secrets []string
	mask    string
}

// NewRedaction builds a redactor for the given secret literals. Empty secrets
// are ignored.
func NewRedaction(secrets ...string) *Redaction {
	kept := make([]string, 0, len(secrets))
	for _, s := range secrets {
		if s != "" {
			kept = append(kept, s)
		}
	}
	return &Redaction{secrets: kept, mask: "«redacted»"}
}

// Mask replaces every known secret occurrence in s with the mask token.
func (r *Redaction) Mask(s string) string {
	for _, secret := range r.secrets {
		s = strings.ReplaceAll(s, secret, r.mask)
	}
	return s
}

// OnLLMStart masks secrets in text and action-result output content in place.
func (r *Redaction) OnLLMStart(_ context.Context, conv *model.Conversation) error {
	if len(r.secrets) == 0 {
		return nil
	}
	for i := range conv.Messages {
		for j := range conv.Messages[i].Content {
			c := &conv.Messages[i].Content[j]
			switch c.Kind {
			case model.KindText:
				c.Text = r.Mask(c.Text)
			case model.KindActionResult:
				c.Result.Output = r.Mask(c.Result.Output)
			}
		}
	}
	return nil
}
