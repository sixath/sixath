package growth

import (
	"encoding/json"
	"fmt"
	"strings"
)

type patchJSON struct {
	Path    string `json:"path"`
	Op      string `json:"op"`
	Content string `json:"content"`
	Old     string `json:"old"`
	New     string `json:"new"`
}

// ParsePatchBatchJSON 将 JSON 数组解析为 []Patch（供假 LLM / 后续真 LLM 输出）。
func ParsePatchBatchJSON(data []byte) ([]Patch, error) {
	s := strings.TrimSpace(string(data))
	if s == "" || s == "null" {
		return nil, nil
	}
	var raw []patchJSON
	if err := json.Unmarshal([]byte(s), &raw); err != nil {
		return nil, fmt.Errorf("growth: parse patch json: %w", err)
	}
	out := make([]Patch, 0, len(raw))
	for i, r := range raw {
		op := Op(strings.TrimSpace(r.Op))
		out = append(out, Patch{
			Path:    strings.TrimSpace(r.Path),
			Op:      op,
			Content: r.Content,
			Old:     r.Old,
			New:     r.New,
		})
		if err := validatePatchOp(out[len(out)-1]); err != nil {
			return nil, fmt.Errorf("growth: patch[%d]: %w", i, err)
		}
	}
	return out, nil
}
