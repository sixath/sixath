package tool

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// 大段文本输出（远程命令 stdout 等）落盘：上下文里只留头尾预览与路径，全文可用 read_file / search_files 翻阅，
// 避免被截断后信息永久丢失。与 query_spill 的行级落盘共用 tmp/results/<session>/ 目录与过期清理。
const (
	textSpillThresholdBytes = 16 * 1024
	textSpillHeadBytes      = 2 * 1024
	textSpillTailBytes      = 6 * 1024
	textSpillFileMaxBytes   = 8 << 20
)

// TextSpill 描述一次文本落盘结果。
type TextSpill struct {
	Path      string `json:"path"`
	Bytes     int    `json:"bytes"`
	Lines     int    `json:"lines"`
	Truncated bool   `json:"file_truncated,omitempty"`
}

// MaybeSpillText 在 text 超过阈值且会话有 workspace 时写入 tmp/results，返回头尾预览；
// 未落盘时返回 (text, nil)。落盘失败同样返回原文，由调用方沿用原有截断逻辑。
func MaybeSpillText(ctx context.Context, toolName, text string) (string, *TextSpill) {
	if len(text) <= textSpillThresholdBytes || ctx == nil {
		return text, nil
	}
	ws, _ := ctx.Value(ContextKeyWorkspaceRoot).(string)
	if strings.TrimSpace(ws) == "" {
		return text, nil
	}
	sess, _ := ctx.Value(ContextKeySessionID).(string)
	rel, full, err := newSpillNamedFile(ws, sess, toolName, ".txt")
	if err != nil {
		return text, nil
	}
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		return text, nil
	}
	body := text
	truncated := false
	if len(body) > textSpillFileMaxBytes {
		body = body[:textSpillFileMaxBytes]
		truncated = true
	}
	if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
		return text, nil
	}
	expireSessionResults(filepath.Dir(full), time.Now())
	info := &TextSpill{Path: rel, Bytes: len(text), Lines: strings.Count(text, "\n") + 1, Truncated: truncated}
	return textSpillPreview(text, info), info
}

func textSpillPreview(text string, info *TextSpill) string {
	head := cutUTF8Prefix(text, textSpillHeadBytes)
	tail := cutUTF8Suffix(text, textSpillTailBytes)
	return fmt.Sprintf("%s\n...[%d bytes omitted; full output (%d lines) saved to %s — use read_file with offset/limit or search_files on that path]...\n%s",
		head, info.Bytes-len(head)-len(tail), info.Lines, info.Path, tail)
}

func cutUTF8Prefix(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && !isRuneStart(s[n]) {
		n--
	}
	return s[:n]
}

func cutUTF8Suffix(s string, n int) string {
	if len(s) <= n {
		return s
	}
	i := len(s) - n
	for i < len(s) && !isRuneStart(s[i]) {
		i++
	}
	return s[i:]
}

func isRuneStart(b byte) bool { return b&0xC0 != 0x80 }
