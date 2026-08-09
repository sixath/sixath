package memorysearch

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

// MemoryChunk 记忆块。
type MemoryChunk struct {
	StartLine int
	EndLine   int
	Text      string
	Hash      string
}

// ChunkMarkdown 按 token 估算分块。使用字符数近似：1 token ≈ 4 字符。
func ChunkMarkdown(content string, tokensPerChunk, overlap int) []MemoryChunk {
	if tokensPerChunk <= 0 {
		tokensPerChunk = 512
	}
	if overlap < 0 {
		overlap = 64
	}
	charsPerChunk := tokensPerChunk * 4
	overlapChars := overlap * 4
	if overlapChars >= charsPerChunk {
		overlapChars = charsPerChunk / 2
	}

	lines := strings.Split(content, "\n")
	var chunks []MemoryChunk
	startLine := 1
	var buf strings.Builder
	lineCount := 0

	for i, line := range lines {
		buf.WriteString(line)
		if i < len(lines)-1 {
			buf.WriteByte('\n')
		}
		lineCount++

		if buf.Len() >= charsPerChunk {
			text := buf.String()
			hash := hashString(text)
			chunks = append(chunks, MemoryChunk{
				StartLine: startLine,
				EndLine:   startLine + lineCount - 1,
				Text:      text,
				Hash:      hash,
			})

			overlapText := text
			if len(text) > overlapChars {
				lastNewline := strings.LastIndex(text[len(text)-overlapChars:], "\n")
				if lastNewline >= 0 {
					overlapText = text[len(text)-overlapChars+lastNewline+1:]
				} else {
					overlapText = text[len(text)-overlapChars:]
				}
			}
			buf.Reset()
			buf.WriteString(overlapText)
			startLine = startLine + lineCount - strings.Count(overlapText, "\n") - 1
			if startLine < 1 {
				startLine = 1
			}
			lineCount = strings.Count(overlapText, "\n") + 1
		}
	}

	if buf.Len() > 0 {
		text := buf.String()
		chunks = append(chunks, MemoryChunk{
			StartLine: startLine,
			EndLine:   startLine + lineCount - 1,
			Text:      text,
			Hash:      hashString(text),
		})
	}

	return chunks
}

func hashString(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}
