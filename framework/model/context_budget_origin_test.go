package model

import (
	"strings"
	"testing"
)

func TestCompressionNoticeUserMessage_MetadataWithoutChineseKeyword(t *testing.T) {
	m := Message{
		Role:    "user",
		Content: "short",
		Metadata: map[string]any{
			MetadataKeySixathOrigin: OriginCompressionNotice,
		},
	}
	if !compressionNoticeUserMessage(m) {
		t.Fatal("expected metadata origin to qualify as compression notice")
	}
}

func TestStripLeadingOrphanTools_SkipsToolAfterCompressionNoticeByMetadata(t *testing.T) {
	msgs := []Message{
		{Role: "system", Content: "s"},
		{
			Role:    "user",
			Content: "x",
			Metadata: map[string]any{
				MetadataKeySixathOrigin: OriginCompressionNotice,
			},
		},
		{Role: "tool", Content: `{}`, Metadata: map[string]any{"tool_call_id": "1"}},
		{Role: "user", Content: "real"},
	}
	out := stripLeadingOrphanToolsAfterSystem(msgs)
	// 与「上下文已压缩」user 后紧跟 tool 的语义一致：前缀中该 user+tool 一并跳过，保留其后第一条真实 user。
	if len(out) != 2 {
		t.Fatalf("expected system + real user, got %d msgs %#v", len(out), out)
	}
	if !strings.EqualFold(out[1].Role, "user") || out[1].Content != "real" {
		t.Fatalf("unexpected tail: %#v", out)
	}
}
