package chat

import (
	"context"
	"errors"

	"github.com/sixath/framework/mea"
	"github.com/sixath/framework/model"
)

// RulesMEAInput is the thin Portal entry for a rules / cascade MEA run.
type RulesMEAInput struct {
	SessionID         string
	AgentID           string
	AgentMEAEnabled   bool // runtime_tools.mea_enabled from Agent UI
	Goal              string
	WorkDir           string
	Checks            []mea.AcceptanceCheck
	Acceptance        []string    // text acceptance (M1 LLM path when Checks empty)
	AuditorModel      model.Model // optional; when set, CascadeAuditor with LLMAuditor
	Executor          mea.Executor // required when running (not skipped)
}

// RulesMEAResult is returned by RunRulesMEA.
type RulesMEAResult struct {
	Skipped bool
	Reason  string // "disabled" when skipped; else orchestrator reason
	State   mea.TaskState
}

// RunRulesMEA runs a BootstrapManager + Rules/Cascade Auditor orchestrator when MEA is
// enabled for the agent (UI flag, global env, or pilot list).
func RunRulesMEA(ctx context.Context, in RulesMEAInput) (RulesMEAResult, error) {
	if !MEAEnabledForAgent(in.AgentID, in.AgentMEAEnabled) {
		return RulesMEAResult{Skipped: true, Reason: "disabled"}, nil
	}
	if in.Executor == nil {
		return RulesMEAResult{}, errors.New("mea: nil executor")
	}
	store, err := MEAFileStore()
	if err != nil {
		return RulesMEAResult{}, err
	}
	var auditor mea.Auditor
	if in.AuditorModel != nil {
		auditor = mea.CascadeAuditor{
			Rules: mea.RulesAuditor{WorkDir: in.WorkDir},
			LLM: mea.LLMAuditor{
				Model:   in.AuditorModel,
				WorkDir: in.WorkDir,
			},
		}
	} else {
		auditor = mea.RulesAuditor{WorkDir: in.WorkDir}
	}
	orch := mea.Orchestrator{
		Store: store,
		Manager: mea.BootstrapManager{
			Goal:       in.Goal,
			Checks:     in.Checks,
			Acceptance: in.Acceptance,
		},
		Executor:  in.Executor,
		Auditor:   auditor,
		MaxRounds: 25,
	}
	state, reason, err := orch.Run(ctx, mea.RunInput{
		SessionID: in.SessionID,
		AgentID:   in.AgentID,
		Goal:      in.Goal,
	})
	if err != nil {
		return RulesMEAResult{Reason: reason, State: state}, err
	}
	return RulesMEAResult{Reason: reason, State: state}, nil
}
