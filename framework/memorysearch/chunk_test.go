package memorysearch

import (
	"testing"
)

func TestChunkMarkdown(t *testing.T) {
	content := "# Title\n\nParagraph one.\n\nParagraph two with more text.\n\n## Section\n\nMore content here."
	chunks := ChunkMarkdown(content, 50, 10)
	if len(chunks) == 0 {
		t.Fatal("expected at least one chunk")
	}
	for i, ch := range chunks {
		if ch.Text == "" {
			t.Errorf("chunk %d: empty text", i)
		}
		if ch.Hash == "" {
			t.Errorf("chunk %d: empty hash", i)
		}
		if ch.StartLine < 1 || ch.EndLine < 1 {
			t.Errorf("chunk %d: invalid line range %d-%d", i, ch.StartLine, ch.EndLine)
		}
	}
}

func TestChunkMarkdown_SmallContent(t *testing.T) {
	content := "Short."
	chunks := ChunkMarkdown(content, 512, 64)
	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk, got %d", len(chunks))
	}
	if chunks[0].Text != "Short." {
		t.Errorf("expected 'Short.', got %q", chunks[0].Text)
	}
}
