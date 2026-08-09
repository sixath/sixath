package middleware

import (
	"strings"

	goahocorasick "github.com/anknown/ahocorasick"
	"github.com/sixath/framework/errs"
)

// FilterDecision 内容过滤决策。
type FilterDecision int

const (
	FilterAllow FilterDecision = iota
	FilterRedact
	FilterBlock
)

// FilterResult 过滤结果。
type FilterResult struct {
	Decision FilterDecision
	HitWords []string
	Redacted string
}

// AhoCorasickFilter 基于 AC 自动机的多模式匹配过滤器。
type AhoCorasickFilter struct {
	matcher *goahocorasick.Machine
}

// NewAhoCorasickFilter 构建过滤器；空词表始终放行。
func NewAhoCorasickFilter(words []string) *AhoCorasickFilter {
	var filtered []string
	for _, w := range words {
		w = strings.TrimSpace(w)
		if w != "" {
			filtered = append(filtered, w)
		}
	}
	if len(filtered) == 0 {
		return &AhoCorasickFilter{}
	}
	dict := make([][]rune, len(filtered))
	for i, w := range filtered {
		dict[i] = []rune(w)
	}
	m := new(goahocorasick.Machine)
	if err := m.Build(dict); err != nil {
		return &AhoCorasickFilter{}
	}
	return &AhoCorasickFilter{matcher: m}
}

// Check 按 role 检查文本（当前 input/output 规则相同，保留 role 供扩展）。
func (f *AhoCorasickFilter) Check(role, text string) FilterResult {
	_ = role
	if f == nil || f.matcher == nil || text == "" {
		return FilterResult{Decision: FilterAllow}
	}
	terms := f.matcher.MultiPatternSearch([]rune(text), false)
	if len(terms) == 0 {
		return FilterResult{Decision: FilterAllow}
	}
	seen := make(map[string]struct{}, len(terms))
	hits := make([]string, 0, len(terms))
	for _, t := range terms {
		if t == nil || len(t.Word) == 0 {
			continue
		}
		w := string(t.Word)
		if _, ok := seen[w]; ok {
			continue
		}
		seen[w] = struct{}{}
		hits = append(hits, w)
	}
	return FilterResult{Decision: FilterBlock, HitWords: hits}
}

// CheckInput 实现 ContentFilter。
func (f *AhoCorasickFilter) CheckInput(text string) error {
	return decisionToError(f.Check("user", text).Decision)
}

// CheckOutput 实现 ContentFilter。
func (f *AhoCorasickFilter) CheckOutput(text string) error {
	return decisionToError(f.Check("assistant", text).Decision)
}

func decisionToError(d FilterDecision) error {
	if d == FilterBlock {
		return errs.ErrContentBlocked
	}
	return nil
}
