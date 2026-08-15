package mea

import "time"

const (
	KindRequirement = "requirement"
	KindArtifact    = "artifact"
	KindFact        = "fact"

	StatusPending   = "pending"
	StatusCompleted = "completed"
	StatusBlocked   = "blocked"
	StatusUntrusted = "untrusted"

	CompletionComplete   = "complete"
	CompletionIncomplete = "incomplete"
	CompletionBlocked    = "blocked"

	IntegrityClean     = "clean"
	IntegritySuspect   = "suspect"
	IntegrityViolation = "violation"

	DecisionExecute = "execute"
	DecisionDone    = "done"
	DecisionBlocked = "blocked"
	DecisionAsk     = "ask"
)

type TaskRecord struct {
	ID           string   `json:"id"`
	Kind         string   `json:"kind"`
	Status       string   `json:"status"`
	Summary      string   `json:"summary"`
	EvidenceRefs []string `json:"evidence_refs,omitempty"`
}

type AcceptanceCheck struct {
	Type     string `json:"type"` // path_exists | file_contains | json_path
	Path     string `json:"path,omitempty"`
	Pattern  string `json:"pattern,omitempty"` // file_contains
	JSONPath string `json:"json_path,omitempty"`
	Equals   string `json:"equals,omitempty"`
}

type Contract struct {
	Round            int               `json:"round"`
	Goal             string            `json:"goal"`
	Acceptance       []string          `json:"acceptance"`
	AcceptanceChecks []AcceptanceCheck `json:"acceptance_checks,omitempty"`
	Boundaries       []string          `json:"boundaries,omitempty"`
	RelevantStateIDs []string          `json:"relevant_state_ids,omitempty"`
	PriorAuditIDs    []string          `json:"prior_audit_ids,omitempty"`
	ToolHint         string            `json:"tool_hint,omitempty"`
	TargetRecordID   string            `json:"target_record_id,omitempty"`
}

type ExecutionReport struct {
	Round            int      `json:"round"`
	Summary          string   `json:"summary"`
	ArtifactsTouched []string `json:"artifacts_touched,omitempty"`
	Issues           []string `json:"issues,omitempty"`
	ClaimComplete    bool     `json:"claim_complete,omitempty"` // ignored for state writes
}

type ProposedUpdate struct {
	RecordID string `json:"record_id,omitempty"`
	Kind     string `json:"kind,omitempty"`
	Status   string `json:"status"`
	Summary  string `json:"summary,omitempty"`
}

type Evidence struct {
	Type    string `json:"type"`
	Ref     string `json:"ref,omitempty"`
	Excerpt string `json:"excerpt,omitempty"`
}

type AuditReport struct {
	ID              string           `json:"id"`
	Round           int              `json:"round"`
	Completion      string           `json:"completion"`
	Integrity       string           `json:"integrity"`
	ProposedUpdates []ProposedUpdate `json:"proposed_updates,omitempty"`
	Evidence        []Evidence       `json:"evidence,omitempty"`
}

type TaskState struct {
	Version   int           `json:"version"`
	SessionID string        `json:"session_id"`
	AgentID   string        `json:"agent_id"`
	Goal      string        `json:"goal"`
	Records   []TaskRecord  `json:"records"`
	Audits    []AuditReport `json:"audits,omitempty"`
	UpdatedAt time.Time     `json:"updated_at"`
}
