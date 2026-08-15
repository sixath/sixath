package mea

import "time"

// ApplyAudit appends audit to state and applies gated ProposedUpdates.
// StatusCompleted is applied only when completion==complete and integrity==clean.
// incomplete/blocked audits may set pending/blocked/untrusted on matching records.
// ExecutionReport must never write TaskState; there is no ApplyExecutionReport.
func ApplyAudit(state TaskState, audit AuditReport) TaskState {
	out := state
	out.Records = copyRecords(state.Records)
	out.Audits = append(append([]AuditReport(nil), state.Audits...), audit)
	out.UpdatedAt = time.Now().UTC()

	canComplete := audit.Completion == CompletionComplete && audit.Integrity == IntegrityClean
	allowNonComplete := audit.Completion == CompletionIncomplete || audit.Completion == CompletionBlocked

	for _, u := range audit.ProposedUpdates {
		if u.RecordID == "" {
			continue
		}
		for i := range out.Records {
			if out.Records[i].ID != u.RecordID {
				continue
			}
			switch u.Status {
			case StatusCompleted:
				if !canComplete {
					continue
				}
				applyProposedFields(&out.Records[i], u)
				out.Records[i].EvidenceRefs = append(out.Records[i].EvidenceRefs, audit.ID)
			case StatusPending, StatusBlocked, StatusUntrusted:
				if !canComplete && !allowNonComplete {
					continue
				}
				applyProposedFields(&out.Records[i], u)
			}
		}
	}
	return out
}

func applyProposedFields(rec *TaskRecord, u ProposedUpdate) {
	rec.Status = u.Status
	if u.Summary != "" {
		rec.Summary = u.Summary
	}
	if u.Kind != "" {
		rec.Kind = u.Kind
	}
}

func copyRecords(in []TaskRecord) []TaskRecord {
	if in == nil {
		return nil
	}
	out := make([]TaskRecord, len(in))
	for i := range in {
		out[i] = in[i]
		if in[i].EvidenceRefs != nil {
			out[i].EvidenceRefs = append([]string(nil), in[i].EvidenceRefs...)
		}
	}
	return out
}
