package agent

import (
	"path/filepath"
	"strings"
)

const surrogateSourceGatePrompt = `禁止用 MEMORY / USER.md / workspace *.txt 顶替 code roots 里的源码。请改用 rca_grep / rca_read / rca_symbol 读 *.go（或仓库内真实源文件），再下结论。MEMORY 与摘录不是调用链证据。

Do not treat MEMORY.md or workspace .txt dumps as source. Use rca_* on code roots.

Missing: source files under code roots were not read; a memory/txt file was used instead.`

var surrogateToolNames = map[string]struct{}{
	"read_file":      {},
	"search_files":   {},
	"memory_get":     {},
	"memory_recall":  {},
	"memory_remember": {},
}

var codeClaimPhrases = []string{
	"源码", "代码", "流程", "函数", "调用", "写入", "映射", "handler",
	"整体流程", "完整流程", "会写", "会调用",
}

func EvaluateSurrogateSourceGate(records []ToolCallRecord, finalText string) EvidenceGateResult {
	usedSurrogate := false
	for _, rec := range records {
		if recordLooksLikeSurrogate(rec) {
			usedSurrogate = true
			break
		}
	}
	if !usedSurrogate {
		return EvidenceGateResult{Allow: true}
	}
	hasGo := hasRCAGoEvidence(records)
	cites := citesSurrogateInText(finalText)
	if hasGo && !cites {
		return EvidenceGateResult{Allow: true}
	}
	if !cites && !claimsCodeInText(finalText) {
		return EvidenceGateResult{Allow: true}
	}
	return EvidenceGateResult{
		Allow:  false,
		Action: "inject",
		Reason: "memory/txt used as source stand-in",
		Prompt: surrogateSourceGatePrompt,
	}
}

func recordLooksLikeSurrogate(rec ToolCallRecord) bool {
	if _, ok := surrogateToolNames[rec.ToolName]; !ok {
		return false
	}
	for _, p := range recordPaths(rec) {
		if isSurrogatePath(p) {
			return true
		}
	}
	return bodyLooksLikeSurrogate(rec.Result)
}

func recordPaths(rec ToolCallRecord) []string {
	var out []string
	if rec.Arguments != nil {
		for _, k := range []string{"path", "rel_path", "file"} {
			if s := strings.TrimSpace(anyString(rec.Arguments[k])); s != "" {
				out = append(out, s)
			}
		}
	}
	if m := toolResultMap(rec.Result); m != nil {
		for _, k := range []string{"path", "file"} {
			if s := strings.TrimSpace(anyString(m[k])); s != "" {
				out = append(out, s)
			}
		}
	}
	return out
}

func isSurrogatePath(p string) bool {
	slash := filepath.ToSlash(strings.ToLower(strings.TrimSpace(p)))
	base := filepath.Base(slash)
	if base == "memory.md" || base == "user.md" {
		return true
	}
	return strings.HasSuffix(slash, ".txt")
}

func bodyLooksLikeSurrogate(result any) bool {
	s := strings.ToLower(anyString(result))
	if s == "" {
		if b, ok := result.(map[string]any); ok {
			s = strings.ToLower(anyString(b["path"]) + " " + anyString(b["file"]))
		}
	}
	return strings.Contains(s, "memory.md") || strings.Contains(s, "user.md") || strings.Contains(s, ".txt")
}

func hasRCAGoEvidence(records []ToolCallRecord) bool {
	for _, rec := range records {
		if rec.Error != "" {
			continue
		}
		switch rec.ToolName {
		case toolRCARead, toolRCAGrep, "rca_glob":
		default:
			continue
		}
		m := toolResultMap(rec.Result)
		if m == nil {
			continue
		}
		for _, p := range []string{anyString(m["file"]), anyString(m["path"])} {
			if strings.HasSuffix(strings.ToLower(p), ".go") {
				return true
			}
		}
		if rec.ToolName == toolRCAGrep {
			if grepHasGoMatch(m) {
				return true
			}
		}
	}
	return false
}

func grepHasGoMatch(m map[string]any) bool {
	switch matches := m["matches"].(type) {
	case []any:
		for _, item := range matches {
			mm, ok := item.(map[string]any)
			if !ok {
				continue
			}
			p := anyString(mm["file"])
			if p == "" {
				p = anyString(mm["path"])
			}
			if strings.HasSuffix(strings.ToLower(p), ".go") {
				return true
			}
		}
	case []map[string]any:
		for _, mm := range matches {
			p := anyString(mm["file"])
			if p == "" {
				p = anyString(mm["path"])
			}
			if strings.HasSuffix(strings.ToLower(p), ".go") {
				return true
			}
		}
	}
	return false
}

func citesSurrogateInText(text string) bool {
	lower := strings.ToLower(text)
	if strings.Contains(lower, "memory.md") || strings.Contains(lower, "user.md") {
		return true
	}
	return strings.Contains(lower, ".txt")
}

func claimsCodeInText(text string) bool {
	for _, p := range codeClaimPhrases {
		if strings.Contains(text, p) {
			return true
		}
	}
	lower := strings.ToLower(text)
	return strings.Contains(lower, "function") || strings.Contains(lower, "handler") || strings.Contains(lower, "call chain")
}
