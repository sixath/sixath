package harness

import (
	"github.com/sixath/framework/internal/anyx"
)

// Usage 表示 token 用量（优先于 Metadata 中的 token_* 字段）。
type Usage struct {
	InputTokens  int64
	OutputTokens int64
}

// Normalize 将 typed 字段与 Metadata map 双向同步（typed 优先）。
func (r *Request) Normalize() {
	if r == nil {
		return
	}
	if r.Metadata == nil {
		r.Metadata = make(map[string]any)
	}
	if r.AgentName == "" {
		r.AgentName = metaString(r.Metadata, MetaAgentName)
	} else {
		r.Metadata[MetaAgentName] = r.AgentName
	}
	if r.UserID == "" {
		r.UserID = metaString(r.Metadata, MetaUserID)
	} else {
		r.Metadata[MetaUserID] = r.UserID
	}
	if r.ModelName == "" {
		r.ModelName = metaString(r.Metadata, MetaModelName)
	} else {
		r.Metadata[MetaModelName] = r.ModelName
	}
	if r.SystemPrompt == "" {
		r.SystemPrompt = metaString(r.Metadata, MetaSystem)
	} else if r.SystemPrompt != "" {
		r.Metadata[MetaSystem] = r.SystemPrompt
	}
	if r.Temperature == 0 {
		if v, ok := r.Metadata[MetaTemperature].(float64); ok {
			r.Temperature = float32(v)
		}
	} else {
		r.Metadata[MetaTemperature] = r.Temperature
	}
}

// EffectiveAgentName 返回 agent 名称（默认 "default"）。
func (r *Request) EffectiveAgentName() string {
	if r == nil {
		return "default"
	}
	r.Normalize()
	if r.AgentName != "" {
		return r.AgentName
	}
	return "default"
}

// SyncUsageToMetadata 将 Usage 写回 Metadata（兼容旧消费者）。
func (resp *Response) SyncUsageToMetadata() {
	if resp == nil {
		return
	}
	if resp.Metadata == nil {
		resp.Metadata = make(map[string]any)
	}
	if resp.Usage.InputTokens > 0 || resp.Metadata[MetaTokenInput] != nil {
		resp.Metadata[MetaTokenInput] = resp.Usage.InputTokens
	}
	if resp.Usage.OutputTokens > 0 || resp.Metadata[MetaTokenOutput] != nil {
		resp.Metadata[MetaTokenOutput] = resp.Usage.OutputTokens
	}
}

// FillUsageFromMetadata 当 Usage 为零时从 Metadata 解析。
func (resp *Response) FillUsageFromMetadata() {
	if resp == nil {
		return
	}
	if resp.Metadata == nil {
		return
	}
	if resp.Usage.InputTokens == 0 {
		if in, ok := anyx.Int64FromAny(resp.Metadata[MetaTokenInput]); ok {
			resp.Usage.InputTokens = in
		}
	}
	if resp.Usage.OutputTokens == 0 {
		if out, ok := anyx.Int64FromAny(resp.Metadata[MetaTokenOutput]); ok {
			resp.Usage.OutputTokens = out
		}
	}
}

func metaString(m map[string]any, key string) string {
	if m == nil {
		return ""
	}
	s, _ := m[key].(string)
	return s
}
