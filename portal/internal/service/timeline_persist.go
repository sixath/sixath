package service

import (
	"encoding/json"
	"sort"
)

// TimelineNode is the persisted shape of a tool/model call node (camelCase JSON).
// Matches web/src/pages/timelineReducer.ts TimelineNode for zero-transform replay.
type TimelineNode struct {
	Kind         string `json:"kind"` // "tool" | "model"
	ID           string `json:"id,omitempty"`
	Step         int    `json:"step"`
	Seq          int    `json:"seq"`
	Phase        string `json:"phase"`
	ToolName     string `json:"toolName,omitempty"`
	Arguments    any    `json:"arguments,omitempty"`
	Result       any    `json:"result,omitempty"`
	Error        string `json:"error,omitempty"`
	Allowed      *bool  `json:"allowed,omitempty"`
	Decision     string `json:"decision,omitempty"`
	DurationMs   *int64 `json:"durationMs,omitempty"`
	Truncated    *bool  `json:"truncated,omitempty"`
	Mode         string `json:"mode,omitempty"`
	Model        string `json:"model,omitempty"`
	InputTokens  *int   `json:"inputTokens,omitempty"`
	OutputTokens *int   `json:"outputTokens,omitempty"`
	MessageCount *int   `json:"messageCount,omitempty"`
}

// TimelineAccumulator merges streaming tool_call / model_call events into a
// finalized timeline suitable for message metadata persistence.
type TimelineAccumulator struct {
	nodes []TimelineNode
	seq   int
}

func (a *TimelineAccumulator) nextSeq() int {
	a.seq++
	return a.seq
}

func (a *TimelineAccumulator) sort() {
	sort.SliceStable(a.nodes, func(i, j int) bool {
		if a.nodes[i].Step != a.nodes[j].Step {
			return a.nodes[i].Step < a.nodes[j].Step
		}
		return a.nodes[i].Seq < a.nodes[j].Seq
	})
}

// ApplyToolCall upserts a tool node by id.
func (a *TimelineAccumulator) ApplyToolCall(p *ToolCallPayload) {
	if a == nil || p == nil || p.ID == "" {
		return
	}
	for i := range a.nodes {
		n := &a.nodes[i]
		if n.Kind == "tool" && n.ID == p.ID {
			n.Phase = p.Phase
			n.ToolName = p.ToolName
			if p.Arguments != nil {
				n.Arguments = p.Arguments
			}
			if p.Result != nil {
				n.Result = p.Result
			}
			if p.Error != "" {
				n.Error = p.Error
			}
			allowed := p.Allowed
			n.Allowed = &allowed
			if p.Decision != "" {
				n.Decision = p.Decision
			}
			if p.DurationMS != 0 {
				d := p.DurationMS
				n.DurationMs = &d
			}
			if p.Truncated {
				t := true
				n.Truncated = &t
			}
			a.sort()
			return
		}
	}
	allowed := p.Allowed
	node := TimelineNode{
		Kind:      "tool",
		ID:        p.ID,
		Step:      p.Step,
		Seq:       a.nextSeq(),
		Phase:     p.Phase,
		ToolName:  p.ToolName,
		Arguments: p.Arguments,
		Result:    p.Result,
		Error:     p.Error,
		Allowed:   &allowed,
		Decision:  p.Decision,
	}
	if p.DurationMS != 0 {
		d := p.DurationMS
		node.DurationMs = &d
	}
	if p.Truncated {
		t := true
		node.Truncated = &t
	}
	a.nodes = append(a.nodes, node)
	a.sort()
}

// ApplyModelCall upserts a model node by step.
func (a *TimelineAccumulator) ApplyModelCall(p *ModelCallPayload) {
	if a == nil || p == nil {
		return
	}
	for i := range a.nodes {
		n := &a.nodes[i]
		if n.Kind == "model" && n.Step == p.Step {
			n.Phase = p.Phase
			if p.Mode != "" {
				n.Mode = p.Mode
			}
			if p.Model != "" {
				n.Model = p.Model
			}
			if p.InputTokens != 0 {
				v := p.InputTokens
				n.InputTokens = &v
			}
			if p.OutputTokens != 0 {
				v := p.OutputTokens
				n.OutputTokens = &v
			}
			if p.MessageCount != 0 {
				v := p.MessageCount
				n.MessageCount = &v
			}
			a.sort()
			return
		}
	}
	node := TimelineNode{
		Kind:  "model",
		Step:  p.Step,
		Seq:   a.nextSeq(),
		Phase: p.Phase,
		Mode:  p.Mode,
		Model: p.Model,
	}
	if p.InputTokens != 0 {
		v := p.InputTokens
		node.InputTokens = &v
	}
	if p.OutputTokens != 0 {
		v := p.OutputTokens
		node.OutputTokens = &v
	}
	if p.MessageCount != 0 {
		v := p.MessageCount
		node.MessageCount = &v
	}
	a.nodes = append(a.nodes, node)
	a.sort()
}

// Finalize marks in-flight nodes as interrupted and returns a copy.
func (a *TimelineAccumulator) Finalize() []TimelineNode {
	if a == nil || len(a.nodes) == 0 {
		return nil
	}
	out := make([]TimelineNode, len(a.nodes))
	for i, n := range a.nodes {
		if n.Kind == "tool" && n.Phase == "started" {
			n.Phase = "interrupted"
		}
		if n.Kind == "model" && n.Phase == "invoked" {
			n.Phase = "interrupted"
		}
		out[i] = n
	}
	return out
}

// MetadataWithTimeline builds message metadata map, or nil when timeline is empty.
func MetadataWithTimeline(nodes []TimelineNode) map[string]any {
	if len(nodes) == 0 {
		return nil
	}
	// Convert to []any so structpb.NewStruct / JSON round-trip stay generic.
	arr := make([]any, len(nodes))
	for i, n := range nodes {
		arr[i] = timelineNodeToMap(n)
	}
	return map[string]any{"timeline": arr}
}

func timelineNodeToMap(n TimelineNode) map[string]any {
	m := map[string]any{
		"kind":  n.Kind,
		"step":  n.Step,
		"seq":   n.Seq,
		"phase": n.Phase,
	}
	if n.ID != "" {
		m["id"] = n.ID
	}
	if n.ToolName != "" {
		m["toolName"] = n.ToolName
	}
	if n.Arguments != nil {
		m["arguments"] = jsonRoundTripAny(n.Arguments)
	}
	if n.Result != nil {
		m["result"] = jsonRoundTripAny(n.Result)
	}
	if n.Error != "" {
		m["error"] = n.Error
	}
	if n.Allowed != nil {
		m["allowed"] = *n.Allowed
	}
	if n.Decision != "" {
		m["decision"] = n.Decision
	}
	if n.DurationMs != nil {
		m["durationMs"] = *n.DurationMs
	}
	if n.Truncated != nil {
		m["truncated"] = *n.Truncated
	}
	if n.Mode != "" {
		m["mode"] = n.Mode
	}
	if n.Model != "" {
		m["model"] = n.Model
	}
	if n.InputTokens != nil {
		m["inputTokens"] = *n.InputTokens
	}
	if n.OutputTokens != nil {
		m["outputTokens"] = *n.OutputTokens
	}
	if n.MessageCount != nil {
		m["messageCount"] = *n.MessageCount
	}
	return m
}

// jsonRoundTripAny converts structs (e.g. *tool.QuerySpillStub) into JSON maps
// so structpb.NewStruct can persist timeline metadata.
func jsonRoundTripAny(v any) any {
	if v == nil {
		return nil
	}
	b, err := json.Marshal(v)
	if err != nil {
		return v
	}
	var out any
	if err := json.Unmarshal(b, &out); err != nil {
		return v
	}
	return out
}
