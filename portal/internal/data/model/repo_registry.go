package model

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"time"
)

// Repository is one git repository discovered under a code root.
type Repository struct {
	ID              string      `gorm:"column:id;primaryKey;size:36"`
	CodeRoot        string      `gorm:"column:code_root;size:255;not null;uniqueIndex:uk_repo_root_rel"`
	RelPath         string      `gorm:"column:rel_path;size:512;not null;uniqueIndex:uk_repo_root_rel"`
	Name            string      `gorm:"column:name;size:256;not null;default:''"`
	Description     string      `gorm:"column:description;type:text"`
	Tags            JSONStrings `gorm:"column:tags;type:json"`
	GitRemote       string      `gorm:"column:git_remote;size:512;not null;default:''"`
	GitBranch       string      `gorm:"column:git_branch;size:256;not null;default:''"`
	HeadCommit      string      `gorm:"column:head_commit;size:64;not null;default:''"`
	SyncMode        string      `gorm:"column:sync_mode;size:16;not null;default:registry_only"`
	Status          string      `gorm:"column:status;size:16;not null;default:active;index:idx_repo_status"`
	HandbookStatus  string      `gorm:"column:handbook_status;size:16;not null;default:none"`
	HandbookCommit  string      `gorm:"column:handbook_commit;size:64;not null;default:''"`
	HandbookVersion int         `gorm:"column:handbook_version;not null;default:0"`
	HandbookStats   JSONObject  `gorm:"column:handbook_stats;type:json"`
	OwnerID         string      `gorm:"column:owner_id;size:36;not null;default:''"`
	LastScannedAt   *time.Time  `gorm:"column:last_scanned_at"`
	CreatedAt       time.Time   `gorm:"column:created_at;not null"`
	UpdatedAt       time.Time   `gorm:"column:updated_at;not null"`
}

func (Repository) TableName() string { return "repositories" }

// RepoGroup is a bindable set of repositories (dir / tag / manual).
type RepoGroup struct {
	ID              string         `gorm:"column:id;primaryKey;size:36"`
	Name            string         `gorm:"column:name;size:256;not null"`
	Kind            string         `gorm:"column:kind;size:16;not null;index:idx_rg_kind"`
	Rule            *RepoGroupRule `gorm:"column:rule;type:json"`
	AutoApplyNew    bool           `gorm:"column:auto_apply_new;not null;default:true"`
	HandbookStatus  string         `gorm:"column:handbook_status;size:16;not null;default:none"`
	HandbookVersion int            `gorm:"column:handbook_version;not null;default:0"`
	OwnerID         string         `gorm:"column:owner_id;size:36;not null;default:''"`
	CreatedAt       time.Time      `gorm:"column:created_at;not null"`
	UpdatedAt       time.Time      `gorm:"column:updated_at;not null"`
}

func (RepoGroup) TableName() string { return "repo_groups" }

// RepoGroupRule is the JSON rule of dir / tag groups.
type RepoGroupRule struct {
	CodeRoot  string   `json:"code_root,omitempty"`
	RelPrefix string   `json:"rel_prefix,omitempty"`
	AllOf     []string `json:"all_of,omitempty"`
	AnyOf     []string `json:"any_of,omitempty"`
}

func (r RepoGroupRule) Value() (driver.Value, error) { return json.Marshal(r) }
func (r *RepoGroupRule) Scan(v any) error            { return scanJSON(v, r) }

type RepoGroupMember struct {
	GroupID   string    `gorm:"column:group_id;primaryKey;size:36"`
	RepoID    string    `gorm:"column:repo_id;primaryKey;size:36;index:idx_rgm_repo"`
	Source    string    `gorm:"column:source;size:16;not null"`
	State     string    `gorm:"column:state;size:16;not null;default:active"`
	CreatedAt time.Time `gorm:"column:created_at;not null"`
}

func (RepoGroupMember) TableName() string { return "repo_group_members" }

type AgentRepoBinding struct {
	AgentID    string      `gorm:"column:agent_id;primaryKey;size:36"`
	TargetKind string      `gorm:"column:target_kind;primaryKey;size:16;index:idx_arb_target,priority:1"`
	TargetID   string      `gorm:"column:target_id;primaryKey;size:256;index:idx_arb_target,priority:2"`
	Mode       string      `gorm:"column:mode;size:16;not null;default:include"`
	SubPaths   JSONStrings `gorm:"column:sub_paths;type:json"`
	Rule       JSONObject  `gorm:"column:rule;type:json"`
	Priority   int         `gorm:"column:priority;not null;default:0"`
	CreatedBy  string      `gorm:"column:created_by;size:128;not null;default:''"`
	CreatedAt  time.Time   `gorm:"column:created_at;not null"`
	UpdatedAt  time.Time   `gorm:"column:updated_at;not null"`
}

func (AgentRepoBinding) TableName() string { return "agent_repo_bindings" }

type AgentEffectiveRepo struct {
	AgentID    string          `gorm:"column:agent_id;primaryKey;size:36"`
	RepoID     string          `gorm:"column:repo_id;primaryKey;size:36;index:idx_aer_repo"`
	Via        RepoBindingRefs `gorm:"column:via;type:json"`
	SubPaths   JSONStrings     `gorm:"column:sub_paths;type:json"`
	ComputedAt time.Time       `gorm:"column:computed_at;not null"`
}

func (AgentEffectiveRepo) TableName() string { return "agent_effective_repos" }

// RepoBindingRef records which binding made a repo effective.
type RepoBindingRef struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
}

type RepoBindingRefs []RepoBindingRef

func (r RepoBindingRefs) Value() (driver.Value, error) { return jsonValue(r) }
func (r *RepoBindingRefs) Scan(v any) error            { return scanJSON(v, r) }

// JSONStrings is a JSON array of strings; nil is stored as NULL.
type JSONStrings []string

func (s JSONStrings) Value() (driver.Value, error) { return jsonValue(s) }
func (s *JSONStrings) Scan(v any) error            { return scanJSON(v, s) }

// JSONObject is a free-form JSON object; nil is stored as NULL.
type JSONObject map[string]any

func (o JSONObject) Value() (driver.Value, error) { return jsonValue(o) }
func (o *JSONObject) Scan(v any) error            { return scanJSON(v, o) }

func jsonValue[T any](v T) (driver.Value, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	if string(b) == "null" {
		return nil, nil
	}
	return string(b), nil
}

func scanJSON(value any, dst any) error {
	switch v := value.(type) {
	case nil:
		return nil
	case []byte:
		if len(v) == 0 {
			return nil
		}
		return json.Unmarshal(v, dst)
	case string:
		if v == "" {
			return nil
		}
		return json.Unmarshal([]byte(v), dst)
	default:
		return fmt.Errorf("scan json: unsupported type %T", value)
	}
}
