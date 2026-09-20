package biz

import (
	"testing"
)

func TestMessageReferencesAttachment(t *testing.T) {
	msgs := []*ChatMessage{
		{ID: "m1", Metadata: map[string]any{
			"attachments": []any{
				map[string]any{"id": "att-a", "kind": "image"},
			},
		}},
		{ID: "m2", Metadata: map[string]any{"foo": "bar"}},
	}
	if !MessageReferencesAttachment(msgs, "att-a") {
		t.Fatal("expected att-a referenced")
	}
	if MessageReferencesAttachment(msgs, "att-missing") {
		t.Fatal("att-missing should not be referenced")
	}
	if MessageReferencesAttachment(nil, "att-a") {
		t.Fatal("nil messages should not reference")
	}
}
