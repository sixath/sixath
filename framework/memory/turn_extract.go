package memory

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

const maxTurnFactBytes = 2048

// TurnInput is one user/assistant exchange for post-turn extraction.
type TurnInput struct {
	UserID           string
	SessionID        string
	AgentID          string
	UserMessage      string
	AssistantMessage string
}

// CandidateFact is a structured fact proposed by an Extractor.
type CandidateFact struct {
	Content string
	Scope   Scope // user | session only
}

// Extractor turns a conversation turn into candidate memory facts.
type Extractor interface {
	Extract(ctx context.Context, in TurnInput) ([]CandidateFact, error)
}

// Extract result kinds for observability (Prometheus / logs).
const (
	ExtractResultDisabled   = "disabled"
	ExtractResultEmptyInput = "empty_input"
	ExtractResultParseFail  = "parse_fail"
	ExtractResultModelFail  = "model_fail"
	ExtractResultError      = "error"
	ExtractResultSuccess    = "success" // extractor OK; Written may be 0
)

// Drop reasons (finite enum — safe as metric labels).
const (
	DropEmpty         = "empty"
	DropTooLong       = "too_long"
	DropInvalidScope  = "invalid_scope"
	DropMissingScope  = "missing_scope_id"
	DropHashDedupe    = "hash_dedupe"
	DropMaxFacts      = "max_facts"
	DropRememberSkip  = "remember_skip" // empty hit id (e.g. conflict ignore)
)

// ExtractStats is the per-turn extraction funnel for metrics and structured logs.
type ExtractStats struct {
	Result     string
	Candidates int
	Written    int
	Drops      map[string]int
	ParseFail  bool
	Duration   time.Duration
}

// Pipeline runs optional turn extraction into MemoryStore units.
type Pipeline struct {
	Store     MemoryStore
	Extractor Extractor
	Enabled   bool
	MaxFacts  int // default 5
}

// AddFromTurn extracts facts and Remember(add)s them after content_hash dedupe.
// Returns the number of units written. Extractor errors are returned to the caller (fail-open upstream).
func (p *Pipeline) AddFromTurn(ctx context.Context, in TurnInput) (int, error) {
	st, err := p.AddFromTurnWithStats(ctx, in)
	return st.Written, err
}

// AddFromTurnWithStats is like AddFromTurn but returns funnel stats for observability.
func (p *Pipeline) AddFromTurnWithStats(ctx context.Context, in TurnInput) (st ExtractStats, err error) {
	start := time.Now()
	st.Drops = map[string]int{}
	defer func() { st.Duration = time.Since(start) }()

	if p == nil || !p.Enabled || p.Store == nil || p.Extractor == nil {
		st.Result = ExtractResultDisabled
		return st, nil
	}
	userMsg := strings.TrimSpace(in.UserMessage)
	asstMsg := strings.TrimSpace(in.AssistantMessage)
	if userMsg == "" && asstMsg == "" {
		st.Result = ExtractResultEmptyInput
		return st, nil
	}

	facts, err := p.Extractor.Extract(ctx, in)
	if err != nil {
		if errors.Is(err, ErrExtractParse) {
			st.Result = ExtractResultParseFail
			st.ParseFail = true
			return st, err
		}
		st.Result = ExtractResultModelFail
		return st, err
	}
	st.Candidates = len(facts)

	maxFacts := p.MaxFacts
	if maxFacts <= 0 {
		maxFacts = 5
	}

	for _, fact := range facts {
		if st.Written >= maxFacts {
			st.Drops[DropMaxFacts]++
			continue
		}
		content := strings.TrimSpace(fact.Content)
		if content == "" {
			st.Drops[DropEmpty]++
			continue
		}
		if len(content) > maxTurnFactBytes {
			st.Drops[DropTooLong]++
			continue
		}
		scope := fact.Scope
		if scope != ScopeSession && scope != ScopeUser {
			st.Drops[DropInvalidScope]++
			continue
		}
		scopeID := ""
		meta := map[string]any{"source": "turn_extract", "kind": "fact"}
		switch scope {
		case ScopeSession:
			scopeID = strings.TrimSpace(in.SessionID)
			if scopeID == "" {
				st.Drops[DropMissingScope]++
				continue
			}
		case ScopeUser:
			scopeID = strings.TrimSpace(in.UserID)
			if scopeID == "" {
				st.Drops[DropMissingScope]++
				continue
			}
			if sid := strings.TrimSpace(in.SessionID); sid != "" {
				meta["source_session_id"] = sid
			}
		}
		if hasActiveContentHash(ctx, p.Store, scope, scopeID, ContentHash(content)) {
			st.Drops[DropHashDedupe]++
			continue
		}
		hit, remErr := p.Store.Remember(ctx, RememberInput{
			Scope:    scope,
			ScopeID:  scopeID,
			AgentID:  strings.TrimSpace(in.AgentID),
			Action:   ActionAdd,
			Content:  content,
			Metadata: meta,
		})
		if remErr != nil {
			st.Result = ExtractResultError
			return st, fmt.Errorf("memory: turn extract remember: %w", remErr)
		}
		if hit.ID == "" {
			st.Drops[DropRememberSkip]++
			continue
		}
		st.Written++
	}
	st.Result = ExtractResultSuccess
	return st, nil
}

func hasActiveContentHash(ctx context.Context, store MemoryStore, scope Scope, scopeID, hash string) bool {
	list, err := store.List(ctx, ListFilter{Scope: scope, ScopeID: scopeID, Status: "active"})
	if err != nil {
		return false
	}
	for _, hit := range list {
		if hit.Metadata == nil {
			continue
		}
		if h, ok := hit.Metadata["content_hash"].(string); ok && h == hash {
			return true
		}
	}
	return false
}
