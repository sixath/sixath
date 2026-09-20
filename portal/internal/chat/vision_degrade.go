package chat

import (
	"strings"

	"github.com/sixath/framework/model"
)

// AttachmentVisionDegradedMarker is emitted on SSE debug / logs when a turn
// retries once without image Parts after a vision-class upstream error.
const AttachmentVisionDegradedMarker = "attachment_vision_degraded=true"

// ShouldDegradeVisionError reports whether err looks like an upstream
// image/vision/modality capability rejection (case-insensitive substring match).
func ShouldDegradeVisionError(err error) bool {
	if err == nil {
		return false
	}
	s := strings.ToLower(err.Error())
	if s == "" {
		return false
	}
	return strings.Contains(s, "image") ||
		strings.Contains(s, "vision") ||
		strings.Contains(s, "modality") ||
		strings.Contains(s, "not support")
}

// VisionDegradeState tracks the one-shot vision degrade retry for a turn.
// First attempt uses modelContent+Parts; after a vision-class error, retry once
// with persistContent and Parts=nil. Never retries again.
type VisionDegradeState struct {
	PersistContent string
	HadImageParts  bool
	degraded       bool
}

// Degraded reports whether the turn already performed the vision degrade retry.
func (st *VisionDegradeState) Degraded() bool {
	return st != nil && st.degraded
}

// TryDegrade returns degraded messages for a single retry when err is
// vision-class and Parts were present. messages must be the list used on the
// failed attempt (last user may carry Parts).
func (st *VisionDegradeState) TryDegrade(err error, messages []model.Message) (retry bool, next []model.Message) {
	if st == nil || st.degraded || !st.HadImageParts {
		return false, nil
	}
	if !ShouldDegradeVisionError(err) {
		return false, nil
	}
	if !messagesHaveImageParts(messages) {
		return false, nil
	}
	st.degraded = true
	return true, ApplyVisionDegrade(messages, st.PersistContent)
}

// ApplyVisionDegrade returns a copy of messages where the last user message
// uses persistContent (footnotes) and Parts=nil.
func ApplyVisionDegrade(messages []model.Message, persistContent string) []model.Message {
	if len(messages) == 0 {
		return nil
	}
	out := make([]model.Message, len(messages))
	copy(out, messages)
	for i := len(out) - 1; i >= 0; i-- {
		if out[i].Role != "user" {
			continue
		}
		out[i].Content = persistContent
		out[i].Parts = nil
		break
	}
	return out
}

func messagesHaveImageParts(messages []model.Message) bool {
	for _, m := range messages {
		for _, p := range m.Parts {
			if p.Type == model.ContentTypeImageURL || strings.TrimSpace(p.URL) != "" {
				return true
			}
		}
	}
	return false
}
