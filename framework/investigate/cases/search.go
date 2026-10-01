package cases

import (
	"math"
	"sort"
	"strings"
	"unicode"
)

// tokenize 把文本切成检索词：拉丁/数字按连续片段（小写），汉字按二元组，单个汉字也保留。
func tokenize(s string) []string {
	var out []string
	var word []rune
	var han []rune
	flushWord := func() {
		if len(word) >= 2 {
			out = append(out, strings.ToLower(string(word)))
		}
		word = word[:0]
	}
	flushHan := func() {
		switch {
		case len(han) == 1:
			out = append(out, string(han))
		case len(han) > 1:
			for i := 0; i+1 < len(han); i++ {
				out = append(out, string(han[i:i+2]))
			}
		}
		han = han[:0]
	}
	for _, r := range s {
		switch {
		case unicode.Is(unicode.Han, r):
			flushWord()
			han = append(han, r)
		case unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_':
			flushHan()
			word = append(word, r)
		default:
			flushWord()
			flushHan()
		}
	}
	flushWord()
	flushHan()
	return out
}

// caseText 用于检索的文本；签名与症状加权（重复计入）。
func caseText(c Case) []string {
	var toks []string
	add := func(s string, w int) {
		t := tokenize(s)
		for i := 0; i < w; i++ {
			toks = append(toks, t...)
		}
	}
	add(c.Title, 1)
	add(c.Symptom, 2)
	add(strings.Join(c.Signature, " "), 3)
	for _, l := range c.Chain {
		add(l.Statement, 1)
	}
	return toks
}

const minMatchedTerms = 3

// rank 用 BM25 给候选案例打分，返回得分 > 0 的前 k 个。
func rank(query string, docs []Case, k int) []Hit {
	q := tokenize(query)
	if len(q) == 0 || len(docs) == 0 {
		return nil
	}
	const k1, b = 1.2, 0.75
	tfs := make([]map[string]int, len(docs))
	lens := make([]int, len(docs))
	df := map[string]int{}
	total := 0
	for i, d := range docs {
		toks := caseText(d)
		tf := map[string]int{}
		for _, t := range toks {
			tf[t]++
		}
		for t := range tf {
			df[t]++
		}
		tfs[i], lens[i] = tf, len(toks)
		total += len(toks)
	}
	avg := float64(total) / float64(len(docs))
	if avg == 0 {
		avg = 1
	}
	seen := map[string]bool{}
	var terms []string
	for _, t := range q {
		if !seen[t] {
			seen[t] = true
			terms = append(terms, t)
		}
	}
	// 只共享一两个常见词（如"失败"）的案例不算相似。
	minMatched := minMatchedTerms
	if len(terms) < minMatched {
		minMatched = len(terms)
	}
	n := float64(len(docs))
	var hits []Hit
	for i, d := range docs {
		score := 0.0
		matched := 0
		for _, t := range terms {
			f := float64(tfs[i][t])
			if f == 0 {
				continue
			}
			matched++
			idf := math.Log(1 + (n-float64(df[t])+0.5)/(float64(df[t])+0.5))
			score += idf * f * (k1 + 1) / (f + k1*(1-b+b*float64(lens[i])/avg))
		}
		if score > 0 && matched >= minMatched {
			hits = append(hits, Hit{Case: d, Score: math.Round(score*1000) / 1000})
		}
	}
	sort.SliceStable(hits, func(a, b int) bool { return hits[a].Score > hits[b].Score })
	if k > 0 && len(hits) > k {
		hits = hits[:k]
	}
	return hits
}
