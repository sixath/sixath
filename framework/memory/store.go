package memory

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
)

type Scope string

const (
	ScopeUser    Scope = "user"
	ScopeSession Scope = "session"
	ScopeAgent   Scope = "agent"
)

var (
	ErrScopeNotEnabled    = errors.New("memory: scope not enabled")
	ErrNotSupported       = errors.New("memory: not supported")
	ErrEmptyQueryRejected = errors.New("memory: empty query rejected")
	// ErrProceduralRememberBlocked rejects bare metadata.kind=procedural (use CommitProceduralRepair).
	ErrProceduralRememberBlocked = errors.New("memory: procedural remember blocked (episode boundary)")
	// ErrProceduralCommitRejected is returned when commit gates fail.
	ErrProceduralCommitRejected = errors.New("memory: procedural commit rejected")
)

// Unit kind values (memory_units.kind / metadata.kind).
const (
	KindFact        = "fact"
	KindProcedural  = "procedural"
	MetaSourceProceduralRepair = "procedural_repair"
	MetaProceduralStatus       = "procedural_status"
	MetaProceduralEntryID      = "procedural_entry_id"
	MetaFailureCode            = "failure_code"
	MetaSupportCount           = "support_count"
)

// KindFactOnly is the default Recall/List filter: exclude procedural.
const (
	KindFilterFactOnly   = ""            // default
	KindFilterProcedural = KindProcedural
	KindFilterAny        = "any"
)

type RememberAction string

const (
	ActionAdd     RememberAction = "add"
	ActionReplace RememberAction = "replace"
	ActionRemove  RememberAction = "remove"
)

type RememberInput struct {
	Scope         Scope
	ScopeID       string // scope=session 时必须 = session_id；scope=agent 时忽略（以 AgentID 为准）
	AgentID       string // scope=agent 时必须；scope=session 时可选（写入 agent_id 列便于过滤）
	WorkspaceRoot string // scope=agent 文件后端必需
	Action        RememberAction
	Content       string
	OldText       string // 仅 scope=agent 的 replace/remove（按正文唯一定位）
	UnitID        string // 仅 scope=session 的 replace/remove（按 id）
	Target        string // 仅 scope=agent：memory → MEMORY.md；user_file → USER.md
	Metadata      map[string]any
}

type RecallSource string

const (
	SourceUnits      RecallSource = "units"      // session 默认
	SourceTranscript RecallSource = "transcript" // 原 session_search
	SourceFiles      RecallSource = "files"      // agent 默认
)

type RecallQuery struct {
	Query         string
	Scope         Scope
	ScopeID       string
	AgentID       string
	SessionID     string // transcript 排除当前会话等
	Source        RecallSource
	Limit         int
	MinScore      float64
	WorkspaceRoot string // agent 文件后端
	// Kind filters units: "" = fact-only (exclude procedural); "procedural"; "any".
	Kind string
	// AnchorWindow is ±N messages for transcript SearchAnchored (0 = manager default).
	AnchorWindow int
	// IncludeTools controls FTS role filter for transcript; nil defaults to true.
	IncludeTools *bool
	// ExcludeCurrent omits q.SessionID from transcript results; nil defaults to true.
	ExcludeCurrent *bool
}

// IncludeToolsOrDefault returns whether transcript search includes tool roles (default true).
func (q RecallQuery) IncludeToolsOrDefault() bool {
	if q.IncludeTools == nil {
		return true
	}
	return *q.IncludeTools
}

// ExcludeCurrentOrDefault returns whether the current session is excluded (default true).
func (q RecallQuery) ExcludeCurrentOrDefault() bool {
	if q.ExcludeCurrent == nil {
		return true
	}
	return *q.ExcludeCurrent
}

type MemoryHit struct {
	Scope    Scope
	Source   RecallSource
	ID       string // unit id or synthetic
	Path     string // agent file path if any
	Content  string
	Score    float64
	Metadata map[string]any
}

type ListFilter struct {
	Scope   Scope
	ScopeID string // session_id when ScopeSession
	AgentID string
	Status  string // 默认 "active"；空表示默认
	Limit   int
	Offset  int
	// Kind same semantics as RecallQuery.Kind.
	Kind string
}

type GetRef struct {
	Scope         Scope
	ID            string // session unit id
	Path          string // agent relative path
	AgentID       string
	ScopeID       string
	WorkspaceRoot string
}

type MemoryStore interface {
	Remember(ctx context.Context, in RememberInput) (MemoryHit, error)
	Recall(ctx context.Context, q RecallQuery) ([]MemoryHit, error)
	Get(ctx context.Context, ref GetRef) (MemoryHit, error)
	List(ctx context.Context, filter ListFilter) ([]MemoryHit, error)
	Delete(ctx context.Context, ref GetRef) error
}

// UnitPatcher is an optional capability for in-place unit updates (same ID, no supersede).
// Facade implements this; callers should type-assert from MemoryStore when needed.
type UnitPatcher interface {
	// PatchUnit updates metadata and optionally content in place; must NOT change unit ID or supersede.
	// content=nil leaves content unchanged; metadata=nil leaves metadata unchanged; non-nil metadata replaces stored metadata.
	PatchUnit(ctx context.Context, ref GetRef, content *string, metadata map[string]any) error
}

type SessionUnitsBackend interface {
	Remember(ctx context.Context, in RememberInput) (MemoryHit, error)
	Recall(ctx context.Context, q RecallQuery) ([]MemoryHit, error)
	Get(ctx context.Context, ref GetRef) (MemoryHit, error)
	List(ctx context.Context, filter ListFilter) ([]MemoryHit, error)
	Delete(ctx context.Context, ref GetRef) error
	// PatchUnit updates metadata and optionally content in place; must NOT change unit ID or supersede.
	// content=nil leaves content unchanged; metadata=nil leaves metadata unchanged; non-nil metadata replaces stored metadata.
	PatchUnit(ctx context.Context, ref GetRef, content *string, metadata map[string]any) error
}

type AgentWorkspaceBackend interface {
	Remember(ctx context.Context, in RememberInput) (MemoryHit, error)
	Recall(ctx context.Context, q RecallQuery) ([]MemoryHit, error)
	Get(ctx context.Context, ref GetRef) (MemoryHit, error)
}

type TranscriptBackend interface {
	Recall(ctx context.Context, q RecallQuery) ([]MemoryHit, error)
}

// ContentHash returns SHA-256 of UTF-8 content as lowercase hex.
func ContentHash(content string) string {
	sum := sha256.Sum256([]byte(content))
	return hex.EncodeToString(sum[:])
}
