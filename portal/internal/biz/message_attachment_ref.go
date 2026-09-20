package biz

import "strings"

// MessageReferencesAttachment reports whether any message metadata.attachments
// entry has the given attachment id.
func MessageReferencesAttachment(messages []*ChatMessage, attID string) bool {
	attID = strings.TrimSpace(attID)
	if attID == "" || len(messages) == 0 {
		return false
	}
	for _, msg := range messages {
		if msg == nil || msg.Metadata == nil {
			continue
		}
		raw, ok := msg.Metadata["attachments"]
		if !ok || raw == nil {
			continue
		}
		switch list := raw.(type) {
		case []any:
			for _, item := range list {
				if attachmentMetaID(item) == attID {
					return true
				}
			}
		case []map[string]any:
			for _, item := range list {
				if attachmentMetaID(item) == attID {
					return true
				}
			}
		}
	}
	return false
}

func attachmentMetaID(item any) string {
	switch v := item.(type) {
	case map[string]any:
		if id, ok := v["id"].(string); ok {
			return strings.TrimSpace(id)
		}
	case ChatAttachment:
		return strings.TrimSpace(v.ID)
	case *ChatAttachment:
		if v != nil {
			return strings.TrimSpace(v.ID)
		}
	}
	return ""
}
