// Package cases 保存已结案的排障案例（症状 → 根因链 → 可复用的验证探针），供后续调查按症状召回。
// 案例由调查结束时自动生成草稿，只有经用户确认的案例才参与召回，避免错误结论污染后续调查。
package cases

import (
	"errors"
	"strings"
	"time"
)

// 案例状态。
const (
	StatusDraft     = "draft"
	StatusConfirmed = "confirmed"
)

// ErrNotFound 表示案例不存在。
var ErrNotFound = errors.New("case not found")

// ChainLink 为根因链中的一环。
type ChainLink struct {
	Kind      string `yaml:"kind" json:"kind"`
	Statement string `yaml:"statement" json:"statement"`
}

// Evidence 为结案时的关键证据原文。
type Evidence struct {
	Tool  string `yaml:"tool,omitempty" json:"tool,omitempty"`
	Quote string `yaml:"quote" json:"quote"`
	Note  string `yaml:"note,omitempty" json:"note,omitempty"`
}

// Probe 为可在新调查中复用的验证探针（同一工具、同类参数）。
type Probe struct {
	Tool   string         `yaml:"tool" json:"tool"`
	Args   map[string]any `yaml:"args,omitempty" json:"args,omitempty"`
	Expect string         `yaml:"expect,omitempty" json:"expect,omitempty"`
}

// Case 一个排障案例。
type Case struct {
	ID           string      `yaml:"id" json:"id"`
	Status       string      `yaml:"status" json:"status"`
	Title        string      `yaml:"title,omitempty" json:"title,omitempty"`
	Symptom      string      `yaml:"symptom" json:"symptom"`
	Signature    []string    `yaml:"signature,omitempty" json:"signature,omitempty"`
	Chain        []ChainLink `yaml:"chain,omitempty" json:"chain,omitempty"`
	Onset        string      `yaml:"onset,omitempty" json:"onset,omitempty"`
	LastGood     string      `yaml:"last_good,omitempty" json:"last_good,omitempty"`
	KeyEvidence  []Evidence  `yaml:"key_evidence,omitempty" json:"key_evidence,omitempty"`
	VerifyProbes []Probe     `yaml:"verify_probes,omitempty" json:"verify_probes,omitempty"`
	Fix          string      `yaml:"fix,omitempty" json:"fix,omitempty"`
	SessionID    string      `yaml:"session_id,omitempty" json:"session_id,omitempty"`
	CreatedAt    time.Time   `yaml:"created_at" json:"created_at"`
	ConfirmedAt  *time.Time  `yaml:"confirmed_at,omitempty" json:"confirmed_at,omitempty"`
	ConfirmedBy  string      `yaml:"confirmed_by,omitempty" json:"confirmed_by,omitempty"`
}

// Root 返回链条中 kind=root 的陈述（没有则为空）。
func (c Case) Root() string {
	for _, l := range c.Chain {
		if l.Kind == "root" {
			return l.Statement
		}
	}
	return ""
}

// Patch 为人工修正案例时可改的字段；nil 表示不改。
type Patch struct {
	Title        *string      `json:"title,omitempty"`
	Symptom      *string      `json:"symptom,omitempty"`
	Signature    *[]string    `json:"signature,omitempty"`
	Chain        *[]ChainLink `json:"chain,omitempty"`
	KeyEvidence  *[]Evidence  `json:"key_evidence,omitempty"`
	VerifyProbes *[]Probe     `json:"verify_probes,omitempty"`
	Fix          *string      `json:"fix,omitempty"`
}

func (p Patch) apply(c *Case) {
	if p.Title != nil {
		c.Title = strings.TrimSpace(*p.Title)
	}
	if p.Symptom != nil {
		c.Symptom = strings.TrimSpace(*p.Symptom)
	}
	if p.Signature != nil {
		c.Signature = append([]string(nil), (*p.Signature)...)
	}
	if p.Chain != nil {
		c.Chain = append([]ChainLink(nil), (*p.Chain)...)
	}
	if p.KeyEvidence != nil {
		c.KeyEvidence = append([]Evidence(nil), (*p.KeyEvidence)...)
	}
	if p.VerifyProbes != nil {
		c.VerifyProbes = append([]Probe(nil), (*p.VerifyProbes)...)
	}
	if p.Fix != nil {
		c.Fix = strings.TrimSpace(*p.Fix)
	}
}

// Hit 为一次检索命中。
type Hit struct {
	Case  Case    `json:"case"`
	Score float64 `json:"score"`
}

// Store 案例库。
type Store interface {
	// Search 按症状文本检索；includeDrafts=false 时只返回已确认案例。
	Search(query string, k int, includeDrafts bool) ([]Hit, error)
	Get(id string) (Case, error)
	// SaveDraft 保存为草稿（覆盖同 ID 草稿；已确认案例不可被草稿覆盖）。
	SaveDraft(c Case) (Case, error)
	Update(id string, p Patch) (Case, error)
	Confirm(id, by string) (Case, error)
	Discard(id string) error
	// List 按创建时间倒序；status 为空表示全部。
	List(status string) ([]Case, error)
}
