package model

import (
	"testing"

	openai "github.com/sashabaranov/go-openai"
)

func TestOpenAIChatMessage_WithImageParts(t *testing.T) {
	msg, err := openAIChatMessage(Message{
		Role:    "user",
		Content: "see",
		Parts: []ContentPart{
			{Type: ContentTypeText, Text: "see"},
			{Type: ContentTypeImageURL, URL: "data:image/png;base64,abc"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(msg.MultiContent) != 2 {
		t.Fatalf("MultiContent=%d", len(msg.MultiContent))
	}
	if msg.MultiContent[1].Type != openai.ChatMessagePartTypeImageURL {
		t.Fatalf("part1=%v", msg.MultiContent[1].Type)
	}
	if msg.Content != "" {
		t.Fatalf("Content should be empty when MultiContent set, got %q", msg.Content)
	}
}

func TestOpenAIChatMessage_PlainTextUnchanged(t *testing.T) {
	msg, err := openAIChatMessage(Message{Role: "user", Content: "hi"})
	if err != nil {
		t.Fatal(err)
	}
	if msg.Content != "hi" || len(msg.MultiContent) != 0 {
		t.Fatalf("%+v", msg)
	}
}
