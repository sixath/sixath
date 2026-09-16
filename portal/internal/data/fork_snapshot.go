package data

import (
	"context"
	"errors"
	"sort"
	"strings"
	"unicode/utf8"

	"backend/internal/biz"

	"github.com/google/uuid"
	"github.com/sixath/framework/memory"
	"github.com/sixath/framework/turntrace"
	"gorm.io/gorm"
)

// ErrForkAnchorMissing is returned when the fork anchor is not in the active prefix.
var ErrForkAnchorMissing = errors.New("fork: anchor not in active prefix")

type ForkSnapshotInput struct {
	Parent *biz.ChatSession
	Anchor *biz.ChatMessage
}

type ForkSnapshotResult struct {
	Child             *biz.ChatSession
	CopiedMessages    int
	CopiedTraces      int
	CopiedMemoryUnits int
	CopiedMessageRows []*biz.ChatMessage // 供 FTS
}

type ForkDeps struct {
	Sessions biz.ChatSessionRepo
	Messages biz.ChatMessageRepo
	Traces   turntrace.Store
	Units    memory.SessionUnitsBackend
}

func ForkSnapshot(ctx context.Context, tx *gorm.DB, in ForkSnapshotInput) (ForkSnapshotResult, error) {
	return ForkSnapshotWithDeps(ctx, tx, in, ForkDeps{
		Sessions: &chatSessionRepo{db: tx},
		Messages: &chatMessageRepo{db: tx},
		Traces:   NewTurnTraceStore(tx),
		Units:    NewSessionUnitsBackend(tx),
	})
}

func ForkSnapshotWithDeps(ctx context.Context, tx *gorm.DB, in ForkSnapshotInput, deps ForkDeps) (ForkSnapshotResult, error) {
	if in.Parent == nil || in.Anchor == nil {
		return ForkSnapshotResult{}, errors.New("fork: parent and anchor required")
	}

	title := forkSessionTitle(in.Parent.Title)
	child, err := deps.Sessions.Create(ctx, in.Parent.UserID, in.Parent.AgentID, title, in.Parent.ID)
	if err != nil {
		return ForkSnapshotResult{}, err
	}

	all, err := deps.Messages.ListActiveOrdered(ctx, in.Parent.ID)
	if err != nil {
		return ForkSnapshotResult{}, err
	}
	idx := -1
	for i, msg := range all {
		if msg.ID == in.Anchor.ID {
			idx = i
			break
		}
	}
	if idx < 0 {
		return ForkSnapshotResult{}, ErrForkAnchorMissing
	}

	cloned := make([]*biz.ChatMessage, 0, idx+1)
	for _, msg := range all[:idx+1] {
		cp, err := deps.Messages.InsertClone(ctx, child.ID, msg)
		if err != nil {
			return ForkSnapshotResult{}, err
		}
		cloned = append(cloned, cp)
	}

	traces, err := deps.Traces.ListBySession(ctx, in.Parent.ID, 0)
	if err != nil {
		return ForkSnapshotResult{}, err
	}
	sort.Slice(traces, func(i, j int) bool {
		if traces[i].CreatedAt.Equal(traces[j].CreatedAt) {
			return traces[i].TurnSeq < traces[j].TurnSeq
		}
		return traces[i].CreatedAt.Before(traces[j].CreatedAt)
	})
	copiedTraces := 0
	for i := range traces {
		tr := traces[i]
		if tr.CreatedAt.After(in.Anchor.CreatedAt) {
			continue
		}
		tr.SessionID = child.ID
		tr.RequestID = uuid.NewString()
		tr.TurnSeq = 0
		if err := deps.Traces.Upsert(ctx, &tr); err != nil {
			return ForkSnapshotResult{}, err
		}
		copiedTraces++
	}

	units, err := deps.Units.List(ctx, memory.ListFilter{
		Scope:   memory.ScopeSession,
		ScopeID: in.Parent.ID,
		Status:  "active",
		Kind:    memory.KindFilterAny,
	})
	if err != nil {
		return ForkSnapshotResult{}, err
	}
	copiedUnits := 0
	for _, hit := range units {
		if _, err := deps.Units.Remember(ctx, memory.RememberInput{
			Scope:    memory.ScopeSession,
			ScopeID:  child.ID,
			AgentID:  in.Parent.AgentID,
			Action:   memory.ActionAdd,
			Content:  hit.Content,
			Metadata: cloneMemoryUnitMetadata(hit.Metadata),
		}); err != nil {
			return ForkSnapshotResult{}, err
		}
		copiedUnits++
	}

	return ForkSnapshotResult{
		Child:             child,
		CopiedMessages:    len(cloned),
		CopiedTraces:      copiedTraces,
		CopiedMemoryUnits: copiedUnits,
		CopiedMessageRows: cloned,
	}, nil
}

func forkSessionTitle(title string) string {
	title = strings.TrimSpace(title)
	out := "Fork"
	if title != "" {
		out = "Fork of " + title
	}
	if utf8.RuneCountInString(out) <= 256 {
		return out
	}
	return string([]rune(out)[:256])
}
