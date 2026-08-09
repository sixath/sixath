package turntrace

import (
	"context"
	"time"

	"github.com/sixath/framework/agent"
)

type Store interface {
	Upsert(ctx context.Context, t *agent.TurnTrace) error
	GetByRequest(ctx context.Context, sessionID, requestID string) (*agent.TurnTrace, error)
	ListBySession(ctx context.Context, sessionID string, limit int) ([]agent.TurnTrace, error)
	// DeactivateAfter soft-hides traces with created_at >= at (Rewind). Optional for Noop stores.
	DeactivateAfter(ctx context.Context, sessionID string, at time.Time) (requestIDs []string, err error)
	// ListByAgent returns active traces for an agent in [from, to], newest first.
	ListByAgent(ctx context.Context, agentID string, from, to time.Time, limit int) ([]agent.TurnTrace, error)
}

type TrajectoryExporter interface {
	Export(ctx context.Context, input any) error
}

type NoopExporter struct{}

func (NoopExporter) Export(context.Context, any) error { return nil }
