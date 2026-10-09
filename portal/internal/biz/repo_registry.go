package biz

import (
	"context"
	"errors"
	"path/filepath"
	"time"
)

const (
	RepoStatusActive   = "active"
	RepoStatusMissing  = "missing"
	RepoStatusArchived = "archived"

	RepoSyncRegistryOnly   = "registry_only"
	HandbookStatusNone     = "none"
	HandbookStatusBuilding = "building"
	HandbookStatusReady    = "ready"
	HandbookStatusFailed   = "failed"

	RepoGroupDir    = "dir"
	RepoGroupTag    = "tag"
	RepoGroupManual = "manual"

	RepoMemberSourceManual = "manual"
	RepoMemberSourceRule   = "rule"
	RepoMemberActive       = "active"

	RepoTargetRepo  = "repo"
	RepoTargetGroup = "repo_group"

	RepoBindingInclude = "include"
	RepoBindingExclude = "exclude"
)

var (
	ErrRepoNotFound       = errors.New("repo registry: not found")
	ErrInvalidRepo        = errors.New("repo registry: invalid repository")
	ErrInvalidRepoBinding = errors.New("repo registry: invalid binding")
	ErrInvalidRepoGroup   = errors.New("repo registry: invalid group")
	ErrRepoScanRunning    = errors.New("repo registry: scan already running")
	ErrRepoGroupInUse     = errors.New("repo registry: group is bound by agents")
)

type Repository struct {
	ID              string         `json:"id"`
	CodeRoot        string         `json:"code_root"`
	RelPath         string         `json:"rel_path"`
	Name            string         `json:"name"`
	Description     string         `json:"description"`
	Tags            []string       `json:"tags"`
	GitRemote       string         `json:"git_remote"`
	GitBranch       string         `json:"git_branch"`
	HeadCommit      string         `json:"head_commit"`
	SyncMode        string         `json:"sync_mode"`
	Status          string         `json:"status"`
	HandbookStatus  string         `json:"handbook_status"`
	HandbookCommit  string         `json:"handbook_commit"`
	HandbookVersion int            `json:"handbook_version"`
	HandbookStats   map[string]any `json:"handbook_stats,omitempty"`
	// HandbookLeaseUntil is set while a build holds the lease; a past value means it is stuck.
	HandbookLeaseUntil *time.Time `json:"handbook_lease_until,omitempty"`
	OwnerID            string     `json:"owner_id"`
	LastScannedAt      *time.Time `json:"last_scanned_at,omitempty"`
	CreatedAt          time.Time  `json:"created_at"`
	UpdatedAt          time.Time  `json:"updated_at"`
}

// AbsPath is the repository root on disk.
func (r *Repository) AbsPath() string {
	return filepath.Join(r.CodeRoot, filepath.FromSlash(r.RelPath))
}

type RepoGroupRule struct {
	CodeRoot  string   `json:"code_root,omitempty"`
	RelPrefix string   `json:"rel_prefix,omitempty"`
	AllOf     []string `json:"all_of,omitempty"`
	AnyOf     []string `json:"any_of,omitempty"`
}

type RepoGroup struct {
	ID           string         `json:"id"`
	Name         string         `json:"name"`
	Kind         string         `json:"kind"`
	Rule         *RepoGroupRule `json:"rule,omitempty"`
	AutoApplyNew bool           `json:"auto_apply_new"`
	OwnerID      string         `json:"owner_id"`
	CreatedAt    time.Time      `json:"created_at"`
	UpdatedAt    time.Time      `json:"updated_at"`
}

// DirPath is the absolute directory of a dir group; empty for other kinds.
func (g *RepoGroup) DirPath() string {
	if g.Kind != RepoGroupDir || g.Rule == nil {
		return ""
	}
	return filepath.Join(g.Rule.CodeRoot, filepath.FromSlash(g.Rule.RelPrefix))
}

type AgentRepoBinding struct {
	AgentID    string   `json:"agent_id"`
	TargetKind string   `json:"target_kind"`
	TargetID   string   `json:"target_id"`
	Mode       string   `json:"mode"`
	SubPaths   []string `json:"sub_paths,omitempty"`
	Priority   int      `json:"priority"`
	CreatedBy  string   `json:"created_by,omitempty"`
}

type BindingRef struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
}

type AgentEffectiveRepo struct {
	AgentID    string       `json:"agent_id"`
	RepoID     string       `json:"repo_id"`
	Via        []BindingRef `json:"via"`
	SubPaths   []string     `json:"sub_paths,omitempty"`
	ComputedAt time.Time    `json:"computed_at"`
}

type RepoFilter struct {
	CodeRoot string
	Status   string
	Query    string
	GroupID  string
}

type RepoMetaPatch struct {
	Name        *string   `json:"name"`
	Description *string   `json:"description"`
	Tags        *[]string `json:"tags"`
	OwnerID     *string   `json:"owner_id"`
	Status      *string   `json:"status"`
}

// HandbookBuildResult is the outcome of one handbook build. Commit and Version are written
// only when Status is ready; a failed build keeps serving the previous version.
type HandbookBuildResult struct {
	Status  string
	Commit  string
	Version int
	Stats   map[string]any
}

// RepoRegistryRepo persists repositories, groups and agent bindings.
type RepoRegistryRepo interface {
	// UpsertScannedRepository inserts by (code_root, rel_path) or refreshes git fields and
	// last_scanned_at; user-edited fields are kept, and archived repos stay archived.
	UpsertScannedRepository(ctx context.Context, r *Repository) (*Repository, error)
	ListRepositories(ctx context.Context, f RepoFilter) ([]*Repository, error)
	GetRepositoriesByIDs(ctx context.Context, ids []string) (map[string]*Repository, error)
	SetRepositoryStatus(ctx context.Context, id, status string) error
	// MarkRepositoryMissingIfActive sets status=missing only when the row is still active
	// and reports whether it changed; archived and already-missing rows are left alone.
	MarkRepositoryMissingIfActive(ctx context.Context, id string) (bool, error)
	UpdateRepositoryMeta(ctx context.Context, id string, p RepoMetaPatch) (*Repository, error)

	// UpsertDirGroup returns the dir group for (codeRoot, relPrefix), creating it with a stable id.
	UpsertDirGroup(ctx context.Context, codeRoot, relPrefix string) (*RepoGroup, error)
	CreateGroup(ctx context.Context, g *RepoGroup) (*RepoGroup, error)
	ListGroups(ctx context.Context, kind string) ([]*RepoGroup, error)
	GetGroupsByIDs(ctx context.Context, ids []string) (map[string]*RepoGroup, error)
	// DeleteGroup removes the group, its members and bindings that target it.
	DeleteGroup(ctx context.Context, id string) error
	ReplaceGroupMembers(ctx context.Context, groupID, source string, repoIDs []string) error
	ListActiveGroupMembers(ctx context.Context, groupIDs []string) (map[string][]string, error)

	ListAgentBindings(ctx context.Context, agentID string) ([]*AgentRepoBinding, error)
	ReplaceAgentBindings(ctx context.Context, agentID string, bs []*AgentRepoBinding) error
	ListAgentIDsWithBindings(ctx context.Context) ([]string, error)
	ListAgentIDsBoundToGroup(ctx context.Context, groupID string) ([]string, error)
	ReplaceEffectiveRepos(ctx context.Context, agentID string, rows []*AgentEffectiveRepo) error
	ListEffectiveRepos(ctx context.Context, agentID string) ([]*AgentEffectiveRepo, error)
	ListAgentIDsByEffectiveRepo(ctx context.Context, repoID string) ([]string, error)

	// ClaimHandbookBuild marks the repo as building until leaseUntil unless another build
	// holds a lease that has not expired at now. On success it returns a fresh lease token
	// that the holder must present to finish or release the build.
	ClaimHandbookBuild(ctx context.Context, id string, now, leaseUntil time.Time) (token string, ok bool, err error)
	// FinishHandbookBuild releases the lease held by token and records the outcome;
	// ErrHandbookLeaseLost when the lease was taken over or already released.
	FinishHandbookBuild(ctx context.Context, id, token string, res HandbookBuildResult) error
	// ReleaseHandbookBuild releases the lease held by token and restores status without
	// recording an outcome; ErrHandbookLeaseLost when token no longer holds the lease.
	ReleaseHandbookBuild(ctx context.Context, id, token, status string) error
}
