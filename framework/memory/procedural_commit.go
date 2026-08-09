package memory

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// ProceduralCommitInput is the gated write for kind=procedural (P3-E).
type ProceduralCommitInput struct {
	AgentID      string
	AgentName    string // optional; pilot match
	SessionID    string
	UserID       string
	PilotAgents  []string
	Signal       FailureSignal
	Binding      ProceduralBinding
	SupportCount int
	EntryID      string
}

// CommitProceduralRepair persists a procedural unit after five-gate checks.
// Skips D2 semantic conflict and vector indexing.
func (f *Facade) CommitProceduralRepair(ctx context.Context, in ProceduralCommitInput) (MemoryHit, error) {
	if f == nil || f.session == nil {
		return MemoryHit{}, errors.New("memory: session backend not configured")
	}
	if !IsPilotAgent(in.PilotAgents, in.AgentID, in.AgentName) {
		return MemoryHit{}, fmt.Errorf("%w: agent not in pilot_agents", ErrProceduralCommitRejected)
	}
	if strings.TrimSpace(in.Signal.Code) == "" {
		return MemoryHit{}, fmt.Errorf("%w: missing failure evidence", ErrProceduralCommitRejected)
	}
	b, err := ValidateProceduralBinding(in.Binding, nil)
	if err != nil {
		return MemoryHit{}, fmt.Errorf("%w: %v", ErrProceduralCommitRejected, err)
	}
	if b.TriggerCode != "" && b.TriggerCode != in.Signal.Code {
		return MemoryHit{}, fmt.Errorf("%w: binding trigger_code mismatch", ErrProceduralCommitRejected)
	}
	sessionID := strings.TrimSpace(in.SessionID)
	if sessionID == "" {
		sessionID = strings.TrimSpace(in.Signal.SessionID)
	}
	if sessionID == "" {
		return MemoryHit{}, fmt.Errorf("%w: session_id required", ErrProceduralCommitRejected)
	}
	entryID := strings.TrimSpace(in.EntryID)
	if entryID == "" {
		entryID = EntryIDForBinding(b)
	}
	content := FormatBindingSuggest(b)
	meta := map[string]any{
		"kind":                 KindProcedural,
		"source":               MetaSourceProceduralRepair,
		MetaProceduralStatus:   ProceduralStatusActive,
		MetaProceduralEntryID:  entryID,
		MetaFailureCode:        in.Signal.Code,
		MetaSupportCount:       in.SupportCount,
		"task_family":          ResolveTaskFamily(in.AgentID, in.AgentName),
		"binding_action_kind":  b.ActionKind,
		"binding_skill_id":     b.SkillID,
		"binding_mode":         b.Mode,
		"binding_tool_names":   b.ToolNames,
		"binding_trigger_code": b.TriggerCode,
		"binding_trigger_query": b.TriggerQuery,
	}
	rememberIn := RememberInput{
		Scope:    ScopeSession,
		ScopeID:  sessionID,
		AgentID:  strings.TrimSpace(in.AgentID),
		Action:   ActionAdd,
		Content:  content,
		Metadata: meta,
	}
	// Bypass Remember()'s procedural block via internal path.
	return f.commitProceduralAdd(ctx, rememberIn)
}

func (f *Facade) commitProceduralAdd(ctx context.Context, in RememberInput) (MemoryHit, error) {
	if f.skipIfActiveContentHash(ctx, in) {
		// Merge gate: existing same content — treat as success no-op.
		return MemoryHit{}, nil
	}
	hit, err := f.session.Remember(ctx, in)
	if err != nil {
		return MemoryHit{}, err
	}
	// No vector / graph index for procedural in P3-E pilot.
	return hit, nil
}
