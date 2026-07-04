package middleware

import (
	"context"

	"github.com/gnanam1990/argus/pkg/agent"
	"github.com/gnanam1990/argus/pkg/model"
)

// ImageRetention keeps only the newest N screenshots in the conversation sent to
// the provider, replacing older image content with a text placeholder. Old
// screenshots are the largest token sink in a long computer-use run, and stale
// frames rarely help the model — dropping them bounds context growth.
type ImageRetention struct {
	agent.Base
	keep        int
	placeholder string
}

// NewImageRetention keeps the newest `keep` screenshots (keep <= 0 keeps none).
func NewImageRetention(keep int) *ImageRetention {
	return &ImageRetention{keep: keep, placeholder: "[older screenshot omitted]"}
}

// OnLLMStart replaces all but the newest `keep` image content parts with a text
// placeholder, walking newest-first.
func (m *ImageRetention) OnLLMStart(_ context.Context, conv *model.Conversation) error {
	seen := 0
	for i := len(conv.Messages) - 1; i >= 0; i-- {
		content := conv.Messages[i].Content
		for j := len(content) - 1; j >= 0; j-- {
			if content[j].Kind != model.KindImage {
				continue
			}
			seen++
			if seen > m.keep {
				content[j] = model.Text(m.placeholder)
			}
		}
	}
	return nil
}
