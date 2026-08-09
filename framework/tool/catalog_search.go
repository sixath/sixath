package tool

import (
	"math"
	"regexp"
	"sort"
	"strings"
)

const (
	bm25K1 = 1.5
	bm25B  = 0.75
)

// AskUserGuardConfig 控制 ask_user BM25 意图守卫。
type AskUserGuardConfig struct {
	MinScore       float64
	ExemptKinds    []string
	ExemptPatterns []string
}

var (
	tokenFindRE   = regexp.MustCompile(`[\p{L}\p{N}]+`)
	camelSplitRE1 = regexp.MustCompile(`([a-z0-9])([A-Z])`)
	camelSplitRE2 = regexp.MustCompile(`([A-Z]+)([A-Z][a-z])`)
)

type scoredEntry struct {
	entry ToolCatalogEntry
	score float64
}

// tokenize 将文本拆为检索词：小写、非字母数字切分、snake_case / CamelCase 拆词。
func tokenize(s string) []string {
	seen := make(map[string]struct{})
	out := make([]string, 0)
	add := func(t string) {
		t = strings.ToLower(t)
		if t == "" {
			return
		}
		if _, ok := seen[t]; ok {
			return
		}
		seen[t] = struct{}{}
		out = append(out, t)
	}

	for _, word := range tokenFindRE.FindAllString(s, -1) {
		add(word)
		if strings.Contains(word, "_") {
			for _, part := range strings.Split(word, "_") {
				add(part)
			}
		}
		if hasCamelCase(word) {
			for _, part := range tokenFindRE.FindAllString(expandCamelCase(word), -1) {
				add(part)
			}
		}
	}
	return out
}

func hasCamelCase(s string) bool {
	var hasLower, hasUpper bool
	for _, r := range s {
		if r >= 'a' && r <= 'z' {
			hasLower = true
		}
		if r >= 'A' && r <= 'Z' {
			hasUpper = true
		}
	}
	return hasLower && hasUpper
}

func expandCamelCase(s string) string {
	s = camelSplitRE2.ReplaceAllString(s, "${1}_${2}")
	s = camelSplitRE1.ReplaceAllString(s, "${1}_${2}")
	return strings.ReplaceAll(s, "_", " ")
}

// buildDoc 拼接目录条目的可检索文本。
func buildDoc(entry ToolCatalogEntry) string {
	var b strings.Builder
	b.WriteString(entry.Name)
	if entry.Description != "" {
		b.WriteByte(' ')
		b.WriteString(entry.Description)
	}
	for _, h := range entry.SearchHints {
		if h == "" {
			continue
		}
		b.WriteByte(' ')
		b.WriteString(h)
	}
	for _, v := range entry.Bindings {
		if v == "" {
			continue
		}
		b.WriteByte(' ')
		b.WriteString(v)
	}
	return b.String()
}

// SearchCatalog 对 Available=true 的条目做 BM25 排序，返回前 limit 条。
func SearchCatalog(cat ToolCatalog, query string, limit int) []ToolCatalogEntry {
	ranked := rankCatalog(cat, query)
	if limit <= 0 || limit > len(ranked) {
		limit = len(ranked)
	}
	out := make([]ToolCatalogEntry, 0, limit)
	for i := 0; i < limit; i++ {
		out = append(out, ranked[i].entry)
	}
	return out
}

// DefaultAskUserGuardConfig 返回 ask_user / 文本回复守卫的默认配置。
func DefaultAskUserGuardConfig() AskUserGuardConfig {
	return AskUserGuardConfig{
		MinScore:    2.0,
		ExemptKinds: []string{"confirm", "select"},
	}
}

// MatchCredentialSolicitation 检测文本是否在向用户索取已由 catalog 提供的凭据/配置。
// 用于拦截模型纯文本回复（非 ask_user 工具调用）中的冗余索凭行为。
func MatchCredentialSolicitation(cat ToolCatalog, text string) (ToolCatalogEntry, bool) {
	text = strings.TrimSpace(text)
	if text == "" {
		return ToolCatalogEntry{}, false
	}
	cfg := DefaultAskUserGuardConfig()
	if match, ok := MatchAskUserIntent(cat, cfg, text, "text"); ok {
		return match, true
	}
	if !looksLikeCredentialSolicitation(text) {
		return ToolCatalogEntry{}, false
	}
	return fallbackBoundCredentialTool(cat)
}

func looksLikeCredentialSolicitation(text string) bool {
	lower := strings.ToLower(text)
	for _, phrase := range []string{
		"请提供", "请给出", "需要你提供", "请回复", "尚未保存", "连接信息", "连接串",
	} {
		if strings.Contains(text, phrase) {
			return true
		}
	}
	keywords := []string{"host", "端口", "port", "password", "密码", "webhook", "用户名", "qyapi.weixin", "mysql", "数据库"}
	hits := 0
	for _, kw := range keywords {
		if strings.Contains(lower, strings.ToLower(kw)) {
			hits++
		}
	}
	return hits >= 2
}

// FormatCredentialSolicitationRedirect 生成注入对话的纠正指令，引导模型改用已绑定工具。
func FormatCredentialSolicitationRedirect(match ToolCatalogEntry) string {
	return "【系统纠正】不要向用户索取数据库连接信息或企微 Webhook——这些已由 Agent 绑定。" +
		"请立即调用工具 " + match.Name + "（绑定 " + formatBindingsBrief(match.Bindings) + "）完成任务，禁止在回复中列出 host/端口/账号/密码/webhook。"
}

func formatBindingsBrief(bindings map[string]string) string {
	if len(bindings) == 0 {
		return "无"
	}
	parts := make([]string, 0, len(bindings))
	for _, key := range []string{"datasource_id", "channel_id", "channel_type", "type", "db_name", "mcp_server"} {
		if v := bindings[key]; v != "" {
			parts = append(parts, key+"="+v)
		}
	}
	if len(parts) == 0 {
		for k, v := range bindings {
			if v != "" {
				parts = append(parts, k+"="+v)
			}
		}
	}
	sort.Strings(parts)
	return strings.Join(parts, ", ")
}

// MatchAskUserIntent 判断 ask_user 是否应被守卫拦截；ok=true 表示应拦截并返回匹配工具。
func MatchAskUserIntent(cat ToolCatalog, cfg AskUserGuardConfig, prompt string, kind string) (ToolCatalogEntry, bool) {
	for _, k := range cfg.ExemptKinds {
		if k == kind {
			return ToolCatalogEntry{}, false
		}
	}
	for _, pat := range cfg.ExemptPatterns {
		if pat == "" {
			continue
		}
		if matched, err := regexp.MatchString(pat, prompt); err == nil && matched {
			return ToolCatalogEntry{}, false
		}
	}

	credentialAsk := looksLikeCredentialSolicitation(prompt)
	searchCat := cat
	if credentialAsk {
		searchCat = catalogWithBoundEntries(cat)
		if len(searchCat.Entries) == 0 {
			return ToolCatalogEntry{}, false
		}
	}

	ranked := rankCatalog(searchCat, prompt)
	if len(ranked) == 0 {
		if credentialAsk {
			if match, ok := fallbackBoundCredentialTool(cat); ok {
				return match, true
			}
		}
		return ToolCatalogEntry{}, false
	}
	top := ranked[0]
	minScore := cfg.MinScore
	if minScore <= 0 {
		minScore = 2.0
	}
	if top.score < minScore || top.entry.Name == "ask_user" {
		if credentialAsk {
			if match, ok := fallbackBoundCredentialTool(cat); ok {
				return match, true
			}
		}
		return ToolCatalogEntry{}, false
	}
	if credentialAsk && len(top.entry.Bindings) == 0 {
		if match, ok := fallbackBoundCredentialTool(cat); ok {
			return match, true
		}
		return ToolCatalogEntry{}, false
	}
	return top.entry, true
}

func catalogWithBoundEntries(cat ToolCatalog) ToolCatalog {
	out := make([]ToolCatalogEntry, 0)
	for _, e := range cat.Entries {
		if e.Available && len(e.Bindings) > 0 {
			out = append(out, e)
		}
	}
	return ToolCatalog{Entries: out, GeneratedAt: cat.GeneratedAt}
}

func fallbackBoundCredentialTool(cat ToolCatalog) (ToolCatalogEntry, bool) {
	for _, name := range []string{"execute_read", "send_to_wecom", "list_tables", "describe_table"} {
		for _, e := range cat.Entries {
			if e.Name == name && e.Available && len(e.Bindings) > 0 {
				return e, true
			}
		}
	}
	return ToolCatalogEntry{}, false
}

func rankCatalog(cat ToolCatalog, query string) []scoredEntry {
	queryTokens := tokenize(query)
	if len(queryTokens) == 0 {
		return nil
	}

	var docs []catalogDoc
	for _, e := range cat.Entries {
		if !e.Available {
			continue
		}
		tokens := tokenize(buildDoc(e))
		if len(tokens) == 0 {
			continue
		}
		tf := termFreq(tokens)
		docs = append(docs, catalogDoc{entry: e, tokens: tokens, tf: tf})
	}
	if len(docs) == 0 {
		return nil
	}

	df := documentFreq(docs)
	n := float64(len(docs))
	var totalLen float64
	for _, d := range docs {
		totalLen += float64(len(d.tokens))
	}
	avgLen := totalLen / n

	scored := make([]scoredEntry, 0, len(docs))
	for _, d := range docs {
		score := bm25Score(queryTokens, d.tf, len(d.tokens), avgLen, n, df)
		if score > 0 {
			scored = append(scored, scoredEntry{entry: d.entry, score: score})
		}
	}
	sort.Slice(scored, func(i, j int) bool {
		if scored[i].score == scored[j].score {
			return scored[i].entry.Name < scored[j].entry.Name
		}
		return scored[i].score > scored[j].score
	})
	return scored
}

func termFreq(tokens []string) map[string]int {
	tf := make(map[string]int, len(tokens))
	for _, t := range tokens {
		tf[t]++
	}
	return tf
}

type catalogDoc struct {
	entry  ToolCatalogEntry
	tokens []string
	tf     map[string]int
}

func documentFreq(docs []catalogDoc) map[string]int {
	df := make(map[string]int)
	for _, d := range docs {
		seen := make(map[string]struct{})
		for t := range d.tf {
			if _, ok := seen[t]; ok {
				continue
			}
			seen[t] = struct{}{}
			df[t]++
		}
	}
	return df
}

func bm25Score(query []string, tf map[string]int, docLen int, avgLen, n float64, df map[string]int) float64 {
	var score float64
	dl := float64(docLen)
	for _, term := range query {
		freq := float64(tf[term])
		if freq == 0 {
			continue
		}
		docFreq := float64(df[term])
		nDocs := math.Max(n, 5.0)
		idf := math.Log(1 + (nDocs-docFreq+0.5)/(docFreq+0.5))
		denom := freq + bm25K1*(1-bm25B+bm25B*dl/math.Max(avgLen, 1.0))
		score += idf * (freq * (bm25K1 + 1)) / denom
	}
	return score
}
