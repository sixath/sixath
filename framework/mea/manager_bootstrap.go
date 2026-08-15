package mea

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
)

// ErrNoObservableAcceptance is returned when BootstrapManager has no machine-checkable
// or text acceptance criteria.
var ErrNoObservableAcceptance = errors.New("mea: no observable acceptance checks")

// BootstrapManager is a non-LLM manager: one goal + structured checks and/or text acceptance.
type BootstrapManager struct {
	Goal       string
	Checks     []AcceptanceCheck
	Acceptance []string // text acceptance (M1 LLM path when Checks empty)
}

var _ Manager = BootstrapManager{}

// Decide creates a single pending requirement when needed and issues execute contracts
// for the first pending record. Empty Checks and Acceptance never yield execute.
func (m BootstrapManager) Decide(ctx context.Context, s TaskState) (decision string, contract *Contract, state TaskState, err error) {
	select {
	case <-ctx.Done():
		return "", nil, s, ctx.Err()
	default:
	}

	if len(m.Checks) == 0 && len(m.Acceptance) == 0 {
		return DecisionAsk, nil, s, ErrNoObservableAcceptance
	}

	goal := m.Goal
	if goal == "" {
		goal = s.Goal
	}
	state = s
	if state.Goal == "" {
		state.Goal = goal
	}

	if len(state.Records) == 0 {
		rec := TaskRecord{
			ID:      uuid.NewString(),
			Kind:    KindRequirement,
			Status:  StatusPending,
			Summary: goal,
		}
		state.Records = []TaskRecord{rec}
		return DecisionExecute, m.contractFor(state, rec.ID, goal), state, nil
	}

	var pendingID string
	hasBlocked := false
	for _, r := range state.Records {
		switch r.Status {
		case StatusPending:
			if pendingID == "" {
				pendingID = r.ID
			}
		case StatusBlocked:
			hasBlocked = true
		}
	}

	if pendingID != "" {
		return DecisionExecute, m.contractFor(state, pendingID, goal), state, nil
	}
	if hasBlocked {
		return DecisionBlocked, nil, state, nil
	}
	return DecisionDone, nil, state, nil
}

func (m BootstrapManager) contractFor(s TaskState, targetID, goal string) *Contract {
	checks := append([]AcceptanceCheck(nil), m.Checks...)
	acceptance := append([]string(nil), m.Acceptance...)
	if len(acceptance) == 0 {
		acceptance = acceptanceStrings(checks)
	}
	prior := make([]string, 0, len(s.Audits))
	for _, a := range s.Audits {
		if a.ID != "" {
			prior = append(prior, a.ID)
		}
	}
	return &Contract{
		Round:            len(s.Audits) + 1,
		Goal:             goal,
		Acceptance:       acceptance,
		AcceptanceChecks: checks,
		RelevantStateIDs: []string{targetID},
		PriorAuditIDs:    prior,
		TargetRecordID:   targetID,
	}
}

func acceptanceStrings(checks []AcceptanceCheck) []string {
	out := make([]string, 0, len(checks))
	for _, c := range checks {
		switch c.Type {
		case "path_exists":
			out = append(out, fmt.Sprintf("path_exists:%s", c.Path))
		case "file_contains":
			out = append(out, fmt.Sprintf("file_contains:%s:%s", c.Path, c.Pattern))
		case "json_path":
			out = append(out, fmt.Sprintf("json_path:%s:%s=%s", c.Path, c.JSONPath, c.Equals))
		default:
			out = append(out, c.Type)
		}
	}
	return out
}
