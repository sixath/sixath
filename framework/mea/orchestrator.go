package mea

import (
	"context"
	"errors"
	"fmt"
	"time"
)

const defaultMaxRounds = 25

// ReasonMaxRounds is returned by Orchestrator.Run when MaxRounds is exhausted.
const ReasonMaxRounds = "max_rounds"

// Manager decides the next MEA action from TaskState.
type Manager interface {
	Decide(ctx context.Context, s TaskState) (decision string, contract *Contract, state TaskState, err error)
}

// Executor performs the contracted work and may mutate the environment.
type Executor interface {
	Execute(ctx context.Context, s TaskState, c Contract) (ExecutionReport, error)
}

// ExecutorFunc adapts a function to Executor.
type ExecutorFunc func(ctx context.Context, s TaskState, c Contract) (ExecutionReport, error)

// Execute implements Executor.
func (f ExecutorFunc) Execute(ctx context.Context, s TaskState, c Contract) (ExecutionReport, error) {
	return f(ctx, s, c)
}

// Store persists TaskState between rounds.
type Store interface {
	Save(state TaskState) error
	Load(sessionID string) (TaskState, error)
}

// Orchestrator runs Manager → Executor → Auditor → ApplyAudit → Save loops.
type Orchestrator struct {
	Store     Store
	Manager   Manager
	Executor  Executor
	Auditor   Auditor
	MaxRounds int // default 25 if <= 0
}

// RunInput identifies a MEA session.
type RunInput struct {
	SessionID string
	AgentID   string
	Goal      string
}

// Run loads or initializes state and loops until a terminal decision or max rounds.
// Terminal reasons: DecisionAsk, DecisionDone, DecisionBlocked, or ReasonMaxRounds.
func (o *Orchestrator) Run(ctx context.Context, in RunInput) (final TaskState, reason string, err error) {
	if o == nil {
		return TaskState{}, "", errors.New("mea: nil orchestrator")
	}
	if o.Store == nil {
		return TaskState{}, "", errors.New("mea: nil store")
	}
	if o.Manager == nil {
		return TaskState{}, "", errors.New("mea: nil manager")
	}
	if o.Executor == nil {
		return TaskState{}, "", errors.New("mea: nil executor")
	}
	if o.Auditor == nil {
		return TaskState{}, "", errors.New("mea: nil auditor")
	}
	if in.SessionID == "" {
		return TaskState{}, "", errors.New("mea: empty session id")
	}

	maxRounds := o.MaxRounds
	if maxRounds <= 0 {
		maxRounds = defaultMaxRounds
	}

	state, loadErr := o.Store.Load(in.SessionID)
	if loadErr != nil {
		if !errors.Is(loadErr, ErrNotFound) {
			return TaskState{}, "", loadErr
		}
		state = TaskState{
			Version:   1,
			SessionID: in.SessionID,
			AgentID:   in.AgentID,
			Goal:      in.Goal,
			UpdatedAt: time.Now().UTC(),
		}
	}
	if state.Goal == "" {
		state.Goal = in.Goal
	}
	if state.AgentID == "" {
		state.AgentID = in.AgentID
	}
	if state.SessionID == "" {
		state.SessionID = in.SessionID
	}

	for round := 0; round < maxRounds; round++ {
		select {
		case <-ctx.Done():
			return state, "", ctx.Err()
		default:
		}

		decision, contract, next, decideErr := o.Manager.Decide(ctx, state)
		state = next

		switch decision {
		case DecisionAsk, DecisionDone, DecisionBlocked:
			if saveErr := o.Store.Save(state); saveErr != nil {
				return state, decision, saveErr
			}
			if decideErr != nil && !errors.Is(decideErr, ErrNoObservableAcceptance) {
				return state, decision, decideErr
			}
			return state, decision, nil
		}

		if decideErr != nil {
			return state, "", decideErr
		}
		if decision != DecisionExecute {
			return state, "", fmt.Errorf("mea: unknown decision %q", decision)
		}
		if contract == nil {
			return state, "", errors.New("mea: execute decision without contract")
		}
		c := *contract
		if c.Round <= 0 {
			c.Round = len(state.Audits) + 1
		}

		report, execErr := o.Executor.Execute(ctx, state, c)
		if execErr != nil {
			return state, "", execErr
		}
		if report.Round == 0 {
			report.Round = c.Round
		}

		audit, auditErr := o.Auditor.Audit(ctx, state, c, report)
		if auditErr != nil {
			return state, "", auditErr
		}
		if audit.Round == 0 {
			audit.Round = c.Round
		}

		state = ApplyAudit(state, audit)
		if saveErr := o.Store.Save(state); saveErr != nil {
			return state, "", saveErr
		}
	}

	if saveErr := o.Store.Save(state); saveErr != nil {
		return state, ReasonMaxRounds, saveErr
	}
	return state, ReasonMaxRounds, nil
}
