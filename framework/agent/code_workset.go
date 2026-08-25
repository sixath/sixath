package agent

import (
	"fmt"
	"sort"
	"strings"

	"github.com/sixath/framework/model"
	"github.com/sixath/framework/tool"
)

const codeWorksetMaxRunes = 1200

// CodeWorkset is a short structured card of the current code working set.
type CodeWorkset struct {
	Files     []string `json:"files,omitempty"`
	Functions []string `json:"functions,omitempty"`
	Callers   []string `json:"callers,omitempty"`
	Callees   []string `json:"callees,omitempty"`
	Open      []string `json:"open_questions,omitempty"`
}

func (w CodeWorkset) empty() bool {
	return len(w.Files) == 0 && len(w.Functions) == 0 && len(w.Callers) == 0 && len(w.Callees) == 0 && len(w.Open) == 0
}

// CollectCodeWorkset builds a compact working-set card from this turn's tool results.
func CollectCodeWorkset(records []ToolCallRecord) CodeWorkset {
	files := map[string]struct{}{}
	funcs := map[string]struct{}{}
	callers := map[string]struct{}{}
	callees := map[string]struct{}{}
	readFunc := false
	scannedInbound := false
	inboundEmpty := false

	for _, rec := range records {
		m := toolResultMap(rec.Result)
		if m == nil {
			continue
		}
		switch rec.ToolName {
		case toolRCARead:
			if f := strings.TrimSpace(anyString(m["file"])); f != "" {
				files[f] = struct{}{}
			}
			for _, fn := range parseControlFlowField(m["control_flow"]) {
				readFunc = true
				label := fmt.Sprintf("%s@%s:%d-%d", fn.Function, fn.File, fn.StartLine, fn.EndLine)
				if fn.Function == "" {
					label = fmt.Sprintf("%s:%d-%d", fn.File, fn.StartLine, fn.EndLine)
				}
				funcs[label] = struct{}{}
				if fn.File != "" {
					files[fn.File] = struct{}{}
				}
			}
			collectWorksetCallGraph(m["call_graph"], callees, files)
		case toolRCAGrep:
			collectWorksetMatchFiles(m, files)
		case "rca_symbol":
			if anyString(m["action"]) == "references" {
				scannedInbound = true
				if b, _ := m["inbound_empty"].(bool); b {
					inboundEmpty = true
				}
			}
			collectWorksetCallers(m, callers, files)
		}
	}

	var open []string
	if readFunc && !scannedInbound {
		open = append(open, "scan inbound callers (rca_symbol action=references)")
	}
	if inboundEmpty {
		open = append(open, "inbound_empty: no in-root callers")
	}

	return CodeWorkset{
		Files:     sortedKeys(files),
		Functions: sortedKeys(funcs),
		Callers:   sortedKeys(callers),
		Callees:   sortedKeys(callees),
		Open:      open,
	}
}

func collectWorksetMatchFiles(m map[string]any, files map[string]struct{}) {
	switch matches := m["matches"].(type) {
	case []any:
		for _, item := range matches {
			mm, ok := item.(map[string]any)
			if !ok {
				continue
			}
			if f := strings.TrimSpace(anyString(mm["file"])); f != "" {
				files[f] = struct{}{}
			}
		}
	case []map[string]any:
		for _, mm := range matches {
			if f := strings.TrimSpace(anyString(mm["file"])); f != "" {
				files[f] = struct{}{}
			}
		}
	}
}

func collectWorksetCallers(m map[string]any, callers, files map[string]struct{}) {
	add := func(path string) {
		path = strings.TrimSpace(path)
		if path == "" {
			return
		}
		callers[path] = struct{}{}
	}
	switch cs := m["callers"].(type) {
	case []any:
		for _, item := range cs {
			mm, ok := item.(map[string]any)
			if !ok {
				continue
			}
			if p := anyString(mm["path"]); p != "" {
				add(p)
			}
			if f := anyString(mm["file"]); f != "" {
				files[f] = struct{}{}
			}
		}
	case []map[string]any:
		for _, mm := range cs {
			if p := anyString(mm["path"]); p != "" {
				add(p)
			}
			if f := anyString(mm["file"]); f != "" {
				files[f] = struct{}{}
			}
		}
	}
}

func collectWorksetCallGraph(v any, callees, files map[string]struct{}) {
	if v == nil {
		return
	}
	switch cg := v.(type) {
	case *tool.CallGraph:
		if cg == nil {
			return
		}
		for _, e := range cg.Edges {
			if e.To != "" {
				callees[e.To] = struct{}{}
			}
		}
		for _, n := range cg.Nodes {
			if n.File != "" {
				files[n.File] = struct{}{}
			}
		}
	case tool.CallGraph:
		collectWorksetCallGraph(&cg, callees, files)
	case map[string]any:
		switch edges := cg["edges"].(type) {
		case []any:
			for _, item := range edges {
				mm, ok := item.(map[string]any)
				if !ok {
					continue
				}
				if to := strings.TrimSpace(anyString(mm["to"])); to != "" {
					callees[to] = struct{}{}
				}
			}
		case []map[string]any:
			for _, mm := range edges {
				if to := strings.TrimSpace(anyString(mm["to"])); to != "" {
					callees[to] = struct{}{}
				}
			}
		}
		switch nodes := cg["nodes"].(type) {
		case []any:
			for _, item := range nodes {
				mm, ok := item.(map[string]any)
				if !ok {
					continue
				}
				if f := strings.TrimSpace(anyString(mm["file"])); f != "" {
					files[f] = struct{}{}
				}
			}
		case []map[string]any:
			for _, mm := range nodes {
				if f := strings.TrimSpace(anyString(mm["file"])); f != "" {
					files[f] = struct{}{}
				}
			}
		}
	}
}

func formatCodeWorkset(w CodeWorkset) string {
	var b strings.Builder
	b.WriteString("[code_workset]\n")
	if len(w.Files) > 0 {
		b.WriteString("files: ")
		b.WriteString(strings.Join(w.Files, ", "))
		b.WriteByte('\n')
	}
	if len(w.Functions) > 0 {
		b.WriteString("functions: ")
		b.WriteString(strings.Join(w.Functions, ", "))
		b.WriteByte('\n')
	}
	if len(w.Callers) > 0 {
		b.WriteString("callers: ")
		b.WriteString(strings.Join(w.Callers, ", "))
		b.WriteByte('\n')
	}
	if len(w.Callees) > 0 {
		b.WriteString("callees: ")
		b.WriteString(strings.Join(w.Callees, ", "))
		b.WriteByte('\n')
	}
	if len(w.Open) > 0 {
		b.WriteString("open_questions: ")
		b.WriteString(strings.Join(w.Open, "; "))
		b.WriteByte('\n')
	}
	s := strings.TrimSuffix(b.String(), "\n")
	runes := []rune(s)
	if len(runes) > codeWorksetMaxRunes {
		return string(runes[:codeWorksetMaxRunes])
	}
	return s
}

func upsertCodeWorksetMessage(msgs []model.Message, ws CodeWorkset) []model.Message {
	out := make([]model.Message, 0, len(msgs)+1)
	for _, m := range msgs {
		if isCodeWorksetMessage(m) {
			continue
		}
		out = append(out, m)
	}
	if ws.empty() {
		return out
	}
	card := model.Message{
		Role:    "system",
		Content: formatCodeWorkset(ws),
		Metadata: map[string]any{
			model.MetadataKeySixathOrigin: model.OriginCodeWorkset,
		},
	}
	head := 0
	for head < len(out) && strings.EqualFold(out[head].Role, "system") {
		head++
	}
	with := make([]model.Message, 0, len(out)+1)
	with = append(with, out[:head]...)
	with = append(with, card)
	with = append(with, out[head:]...)
	return with
}

func isCodeWorksetMessage(m model.Message) bool {
	if m.Metadata != nil {
		if v, ok := m.Metadata[model.MetadataKeySixathOrigin].(string); ok && v == model.OriginCodeWorkset {
			return true
		}
	}
	return strings.HasPrefix(strings.TrimSpace(m.Content), "[code_workset]")
}

func sortedKeys(m map[string]struct{}) []string {
	if len(m) == 0 {
		return nil
	}
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
