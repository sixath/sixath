package handbook

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/sixath/framework/model"
)

const (
	maxCardFuncs   = 8
	cardMaxTokens  = 900
	maxCardSymbols = 80
	clippedMark    = "…（后续内容已截断）\n"
)

var cardRoles = map[string]bool{
	"entry": true, "handler": true, "service": true, "repository": true, "model": true,
	"config": true, "util": true, "client": true, "job": true, "other": true,
}

const cardSystemPrompt = "你是资深后端工程师，为故障排查（RCA）生成代码文件卡片。只输出一个 JSON 对象，不要输出其他文字。"

func cardPrompt(relPath string, f File, syms []Symbol, content string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "为下面的文件生成文件卡片。\n\n仓库：%s\n文件：%s（%s，%d 行）\n", relPath, f.Path, f.Lang, f.Lines)
	if len(syms) > 0 {
		b.WriteString("\n符号清单（functions.name 只能从这里原样选取）：\n")
		for i, s := range syms {
			if i == maxCardSymbols {
				fmt.Fprintf(&b, "- …另有 %d 个\n", len(syms)-i)
				break
			}
			fmt.Fprintf(&b, "- %s `%s` L%d-%d\n", s.Kind, s.Name, s.Line, s.EndLine)
		}
	}
	fence := codeFence(content)
	fmt.Fprintf(&b, "\n文件内容：\n%s%s\n%s\n%s\n\n", fence, f.Lang, content, fence)
	b.WriteString(`输出 JSON：{"purpose":"一句话说明文件职责（不超过 60 字）",` +
		`"description":"2-4 句：做什么、被谁调用、读写哪些表/缓存/topic/外部接口",` +
		`"role":"entry|handler|service|repository|model|config|util|client|job|other 之一",` +
		`"lifecycle":"何时执行：启动/请求/定时任务/消息消费等，不确定留空",` +
		`"functions":[{"name":"符号清单中的名字","summary":"一句话"}]}` + "\n")
	fmt.Fprintf(&b, "functions 最多 %d 个，选与业务流程和状态读写关系最大的；没有符号清单时输出空数组。只依据文件内容，不要猜测。\n", maxCardFuncs)
	return b.String()
}

// codeFence returns a backtick fence longer than any backtick run in content (at least 3).
func codeFence(content string) string {
	longest, run := 0, 0
	for i := 0; i < len(content); i++ {
		if content[i] == '`' {
			run++
			longest = max(longest, run)
		} else {
			run = 0
		}
	}
	return strings.Repeat("`", max(3, longest+1))
}

// clipContent keeps at most maxBytes of content, cut at a line boundary when there is one
// and otherwise at a UTF-8 boundary.
func clipContent(content []byte, maxBytes int) string {
	maxBytes = max(maxBytes, 1)
	if len(content) <= maxBytes {
		return string(content)
	}
	cut := maxBytes
	for cut > 0 && !utf8.RuneStart(content[cut]) {
		cut--
	}
	if i := bytes.LastIndexByte(content[:cut], '\n'); i > 0 {
		cut = i + 1
	}
	return string(content[:cut]) + clippedMark
}

type cardReply struct {
	Purpose     string     `json:"purpose"`
	Description string     `json:"description"`
	Role        string     `json:"role"`
	Lifecycle   string     `json:"lifecycle"`
	Functions   []CardFunc `json:"functions"`
}

// generateCard asks the model for the card of one file whose content hashes to f.Hash.
func generateCard(ctx context.Context, m model.Model, modelName, relPath string, f File, syms []Symbol, content []byte, maxBytes int, u *usage) (*Card, error) {
	var r cardReply
	if err := llmJSON(ctx, m, cardSystemPrompt, cardPrompt(relPath, f, syms, clipContent(content, maxBytes)), cardMaxTokens, &r, u); err != nil {
		return nil, err
	}
	card := sanitizeCard(r, syms)
	if card.Purpose == "" {
		return nil, fmt.Errorf("%w: empty purpose", errBadReply)
	}
	card.Hash, card.PromptVersion, card.Model, card.CreatedAt = f.Hash, LLMPromptVersion, modelName, time.Now().UTC()
	return card, nil
}

// sanitizeCard clips fields and keeps only functions that name one of the file's symbols.
func sanitizeCard(r cardReply, syms []Symbol) *Card {
	c := &Card{
		Purpose:     clipRunes(r.Purpose, 120),
		Description: clipRunes(r.Description, 600),
		Lifecycle:   clipRunes(r.Lifecycle, 120),
		Role:        strings.ToLower(strings.TrimSpace(r.Role)),
	}
	if !cardRoles[c.Role] {
		c.Role = "other"
	}
	known := make(map[string]bool, len(syms))
	for _, s := range syms {
		known[s.Name] = true
	}
	seen := map[string]bool{}
	for _, fn := range r.Functions {
		name := strings.Trim(fn.Name, " `")
		if !known[name] || seen[name] || len(c.Functions) == maxCardFuncs {
			continue
		}
		seen[name] = true
		c.Functions = append(c.Functions, CardFunc{Name: name, Summary: clipRunes(fn.Summary, 160)})
	}
	return c
}
