package handbook

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/sixath/framework/model"
)

const (
	stageInputBudget   = 40 << 10
	stageMaxTokens     = 700
	overviewMaxTokens  = 1500
	maxNotedRegisters  = 60
	registerBatch      = 20
	registerMaxTokens  = 1500
	maxLocationsInNote = 6
)

func regKey(kind, name string) string { return kind + ":" + name }

// stageSystemPrompt must not contain "执行阶段" (the skeleton prompt's marker in tests).
const stageSystemPrompt = "你是资深后端工程师，为故障排查撰写代码阶段说明。只输出一个 JSON 对象，不要输出其他文字。"

func stagePrompt(relPath string, st Stage, files []string, cards map[string]*Card) string {
	var b strings.Builder
	fmt.Fprintf(&b, "仓库：%s\n阶段：%s（%s）", relPath, st.Title, st.ID)
	if st.Summary != "" {
		b.WriteString("，提示：" + st.Summary)
	}
	b.WriteString("\n\n阶段内文件：\n")
	for i, p := range files {
		line := "- " + p + "："
		if c := cards[p]; c != nil {
			line += c.Purpose
			if len(c.Functions) > 0 {
				names := make([]string, 0, len(c.Functions))
				for _, fn := range c.Functions {
					names = append(names, fn.Name)
				}
				line += "；关键函数：" + strings.Join(names, "、")
			}
		} else {
			line += "（无卡片）"
		}
		if room := stageInputBudget - b.Len(); len(line) > room {
			if i > 0 {
				b.WriteString("- …其余文件省略\n")
				break
			}
			line = clipBytes(line, room-1)
		}
		b.WriteString(line + "\n")
	}
	b.WriteString("\n写阶段说明，输出 JSON：" + `{"summary":"3-6 句：该阶段在运行时做什么、主流程顺序、关键文件和函数、读写哪些共享状态（表/缓存/topic/外部接口）、常见故障点"}` + "\n只依据上面的信息，不要编造文件或函数。\n")
	return b.String()
}

// summarizeStage writes the L2 description of one stage.
func summarizeStage(ctx context.Context, m model.Model, relPath string, st Stage, files []string, cards map[string]*Card, u *usage) (string, error) {
	var r struct {
		Summary string `json:"summary"`
	}
	if err := llmJSON(ctx, m, stageSystemPrompt, stagePrompt(relPath, st, files, cards), stageMaxTokens, &r, u); err != nil {
		return "", err
	}
	s := clipRunes(r.Summary, 1500)
	if s == "" {
		return "", fmt.Errorf("%w: empty stage summary", errBadReply)
	}
	return s, nil
}

const overviewSystemPrompt = "你是资深架构师，为故障排查撰写仓库总览。只输出一个 JSON 对象，不要输出其他文字。"

func overviewPrompt(relPath string, f *Facts, sk *Skeleton) string {
	var b strings.Builder
	fmt.Fprintf(&b, "仓库：%s\n", relPath)
	if f.Module != nil && f.Module.Path != "" {
		fmt.Fprintf(&b, "Go module：%s\n", f.Module.Path)
	}
	for _, p := range f.Packages {
		if p.Main {
			fmt.Fprintf(&b, "入口：%s\n", p.Dir)
		}
	}
	b.WriteString("\n各阶段概要：\n")
	for _, s := range sk.Stages {
		fmt.Fprintf(&b, "- %s：%s\n", s.Title, clipRunes(s.Summary, 300))
	}
	byKind := map[string]map[string]int{}
	for _, h := range f.Registers {
		if byKind[h.Kind] == nil {
			byKind[h.Kind] = map[string]int{}
		}
		byKind[h.Kind][h.Name]++
	}
	b.WriteString("\n共享状态（按出现次数，最多 10 个）：\n")
	for _, k := range registerKinds {
		names := keysByCount(byKind[k.kind])
		if len(names) == 0 {
			continue
		}
		fmt.Fprintf(&b, "- %s：%s\n", k.title, strings.Join(names[:min(10, len(names))], "、"))
	}
	b.WriteString("\n输出 JSON：" + `{"overview":"Markdown，3-5 段：系统做什么；主流程（按阶段顺序串起来）；外部依赖与共享状态；排查时的切入建议"}` + "\n只依据上面的信息。\n")
	return b.String()
}

// writeOverview writes the L1 overview of the repository.
func writeOverview(ctx context.Context, m model.Model, relPath string, f *Facts, sk *Skeleton, u *usage) (string, error) {
	var r struct {
		Overview string `json:"overview"`
	}
	if err := llmJSON(ctx, m, overviewSystemPrompt, overviewPrompt(relPath, f, sk), overviewMaxTokens, &r, u); err != nil {
		return "", err
	}
	s := clipRunes(r.Overview, 4000)
	if s == "" {
		return "", fmt.Errorf("%w: empty overview", errBadReply)
	}
	return s, nil
}

const registerSystemPrompt = "你是资深后端工程师，为共享状态（数据表、HTTP 路由、MQ topic、缓存键）写一句话用途，供故障排查使用。只输出一个 JSON 对象。"

type regGroup struct {
	Key  string
	Hits []RegisterHit
}

// registerNotes returns notes for the most referenced registers. Registers with an existing
// note keep it unless one of their locations is in changed; unusable replies keep old notes.
func registerNotes(ctx context.Context, m model.Model, relPath string, hits []RegisterHit, cards map[string]*Card, changed map[string]bool, old map[string]string, u *usage) (map[string]string, error) {
	byKey := map[string]*regGroup{}
	var groups []*regGroup
	for _, h := range hits {
		k := regKey(h.Kind, h.Name)
		g := byKey[k]
		if g == nil {
			g = &regGroup{Key: k}
			byKey[k] = g
			groups = append(groups, g)
		}
		g.Hits = append(g.Hits, h)
	}
	sort.SliceStable(groups, func(i, j int) bool {
		if len(groups[i].Hits) != len(groups[j].Hits) {
			return len(groups[i].Hits) > len(groups[j].Hits)
		}
		return groups[i].Key < groups[j].Key
	})
	if len(groups) > maxNotedRegisters {
		groups = groups[:maxNotedRegisters]
	}
	notes := map[string]string{}
	var todo []*regGroup
	for _, g := range groups {
		n, ok := old[g.Key]
		if ok && !touches(g.Hits, changed) {
			notes[g.Key] = n
			continue
		}
		if ok {
			notes[g.Key] = n
		}
		todo = append(todo, g)
	}
	for i := 0; i < len(todo); i += registerBatch {
		batch := todo[i:min(i+registerBatch, len(todo))]
		var r struct {
			Notes map[string]string `json:"notes"`
		}
		err := llmJSON(ctx, m, registerSystemPrompt, registerPrompt(relPath, batch, cards), registerMaxTokens, &r, u)
		if err != nil {
			if errors.Is(err, errBadReply) {
				continue
			}
			return nil, err
		}
		for _, g := range batch {
			if n := clipRunes(collapseSpaces(r.Notes[g.Key]), 160); n != "" {
				notes[g.Key] = n
			}
		}
	}
	return notes, nil
}

// clipBytes keeps at most n bytes of s (cut at a UTF-8 boundary), marking the cut with "…".
func clipBytes(s string, n int) string {
	if len(s) <= n {
		return s
	}
	const mark = "…"
	cut := max(n-len(mark), 0)
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + mark
}

func touches(hits []RegisterHit, changed map[string]bool) bool {
	for _, h := range hits {
		if changed[h.Path] {
			return true
		}
	}
	return false
}

func registerPrompt(relPath string, batch []*regGroup, cards map[string]*Card) string {
	var b strings.Builder
	fmt.Fprintf(&b, "仓库：%s\n\n", relPath)
	for _, g := range batch {
		fmt.Fprintf(&b, "%s\n", g.Key)
		for i, h := range g.Hits {
			if i == maxLocationsInNote {
				fmt.Fprintf(&b, "  - …另有 %d 处\n", len(g.Hits)-i)
				break
			}
			fmt.Fprintf(&b, "  - %s %s:%d", accessLabels[h.Access], h.Path, h.Line)
			if c := cards[h.Path]; c != nil {
				b.WriteString("（" + clipRunes(c.Purpose, 60) + "）")
			}
			b.WriteString("\n")
		}
	}
	b.WriteString("\n为每个条目写一句话用途，输出 JSON：" + `{"notes":{"上面的条目原样作为 key":"一句话用途"}}` + "\n")
	return b.String()
}
