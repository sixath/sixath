package mea

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
	"github.com/sixath/framework/agent"
)

// Auditor verifies execution against a contract without mutating the environment.
type Auditor interface {
	Audit(ctx context.Context, s TaskState, c Contract, o ExecutionReport) (AuditReport, error)
}

// RulesAuditor runs structured acceptance checks under WorkDir (read-only).
type RulesAuditor struct {
	WorkDir string
}

var _ Auditor = RulesAuditor{}

// Audit evaluates c.AcceptanceChecks against the filesystem under WorkDir.
// It never runs shell commands. Path escape yields incomplete + integrity violation.
func (a RulesAuditor) Audit(ctx context.Context, _ TaskState, c Contract, o ExecutionReport) (AuditReport, error) {
	report := AuditReport{
		ID:         uuid.NewString(),
		Round:      c.Round,
		Completion: CompletionComplete,
		Integrity:  IntegrityClean,
	}

	if len(c.AcceptanceChecks) == 0 {
		report.Completion = CompletionIncomplete
		report.Integrity = IntegritySuspect
		report.Evidence = append(report.Evidence, Evidence{
			Type:    "acceptance",
			Excerpt: "no acceptance checks",
		})
		return report, nil
	}

	for _, check := range c.AcceptanceChecks {
		select {
		case <-ctx.Done():
			return AuditReport{}, ctx.Err()
		default:
		}

		if check.Type == "trace_hit_status" || check.Type == "empty_hit_speak" {
			ok, excerpt, checkErr := a.runTraceCheck(check, o)
			if checkErr != nil {
				report.Completion = CompletionIncomplete
				if report.Integrity == IntegrityClean {
					report.Integrity = IntegritySuspect
				}
				report.Evidence = append(report.Evidence, Evidence{
					Type:    check.Type,
					Ref:     check.Path,
					Excerpt: checkErr.Error(),
				})
				continue
			}
			if !ok {
				report.Completion = CompletionIncomplete
				if report.Integrity == IntegrityClean {
					report.Integrity = IntegritySuspect
				}
				report.Evidence = append(report.Evidence, Evidence{
					Type:    check.Type,
					Ref:     check.Path,
					Excerpt: excerpt,
				})
				continue
			}
			report.Evidence = append(report.Evidence, Evidence{
				Type:    check.Type,
				Ref:     check.Path,
				Excerpt: "pass",
			})
			continue
		}

		abs, err := a.resolvePath(check.Path)
		if err != nil {
			report.Completion = CompletionIncomplete
			report.Integrity = IntegrityViolation
			report.Evidence = append(report.Evidence, Evidence{
				Type:    check.Type,
				Ref:     check.Path,
				Excerpt: err.Error(),
			})
			report.ProposedUpdates = nil
			return report, nil
		}

		ok, excerpt, checkErr := a.runCheck(check, abs)
		if checkErr != nil {
			report.Completion = CompletionIncomplete
			if report.Integrity == IntegrityClean {
				report.Integrity = IntegritySuspect
			}
			report.Evidence = append(report.Evidence, Evidence{
				Type:    check.Type,
				Ref:     check.Path,
				Excerpt: checkErr.Error(),
			})
			continue
		}
		if !ok {
			report.Completion = CompletionIncomplete
			if report.Integrity == IntegrityClean {
				report.Integrity = IntegritySuspect
			}
			report.Evidence = append(report.Evidence, Evidence{
				Type:    check.Type,
				Ref:     check.Path,
				Excerpt: excerpt,
			})
			continue
		}
		report.Evidence = append(report.Evidence, Evidence{
			Type:    check.Type,
			Ref:     check.Path,
			Excerpt: "pass",
		})
	}

	if report.Completion == CompletionComplete && report.Integrity == IntegrityClean {
		if c.TargetRecordID != "" {
			report.ProposedUpdates = []ProposedUpdate{{
				RecordID: c.TargetRecordID,
				Status:   StatusCompleted,
				Summary:  "acceptance checks passed",
			}}
		}
	} else {
		report.ProposedUpdates = nil
		if report.Completion == CompletionComplete {
			report.Completion = CompletionIncomplete
		}
	}

	return report, nil
}

func (a RulesAuditor) resolvePath(rel string) (string, error) {
	if rel == "" {
		return "", fmt.Errorf("empty path")
	}
	if strings.Contains(rel, "..") {
		return "", fmt.Errorf("path escape: %q", rel)
	}
	work, err := filepath.Abs(a.WorkDir)
	if err != nil {
		return "", fmt.Errorf("workdir: %w", err)
	}
	joined := filepath.Join(work, rel)
	abs, err := filepath.Abs(joined)
	if err != nil {
		return "", err
	}
	sep := string(os.PathSeparator)
	prefix := work
	if !strings.HasSuffix(prefix, sep) {
		prefix += sep
	}
	if abs != work && !strings.HasPrefix(abs, prefix) {
		return "", fmt.Errorf("path escape: %q outside workdir", rel)
	}
	return abs, nil
}

func (a RulesAuditor) runCheck(check AcceptanceCheck, abs string) (ok bool, excerpt string, err error) {
	switch check.Type {
	case "path_exists":
		_, statErr := os.Stat(abs)
		if statErr != nil {
			if os.IsNotExist(statErr) {
				return false, "path does not exist", nil
			}
			return false, "", statErr
		}
		return true, "", nil

	case "file_contains":
		b, readErr := os.ReadFile(abs)
		if readErr != nil {
			if os.IsNotExist(readErr) {
				return false, "file does not exist", nil
			}
			return false, "", readErr
		}
		if !strings.Contains(string(b), check.Pattern) {
			return false, fmt.Sprintf("pattern %q not found", check.Pattern), nil
		}
		return true, "", nil

	case "json_path":
		b, readErr := os.ReadFile(abs)
		if readErr != nil {
			if os.IsNotExist(readErr) {
				return false, "file does not exist", nil
			}
			return false, "", readErr
		}
		var obj map[string]any
		if unmarshalErr := json.Unmarshal(b, &obj); unmarshalErr != nil {
			return false, "invalid json object", nil
		}
		key := check.JSONPath
		if strings.HasPrefix(key, "$.") {
			key = strings.TrimPrefix(key, "$.")
		}
		if key == "" || strings.Contains(key, ".") || strings.Contains(key, "[") {
			return false, fmt.Sprintf("unsupported json_path %q (single-segment key only)", check.JSONPath), nil
		}
		val, exists := obj[key]
		if !exists {
			return false, fmt.Sprintf("key %q missing", key), nil
		}
		got := jsonValueString(val)
		if got != check.Equals {
			return false, fmt.Sprintf("got %q want %q", got, check.Equals), nil
		}
		return true, "", nil

	case "command_exit":
		return false, "command_exit not supported in M0 rules auditor", nil

	default:
		return false, fmt.Sprintf("unknown check type %q", check.Type), nil
	}
}

func (a RulesAuditor) runTraceCheck(check AcceptanceCheck, o ExecutionReport) (ok bool, excerpt string, err error) {
	switch check.Type {
	case "trace_hit_status":
		for _, h := range o.ToolHits {
			if h.Error != "" || h.Blocked {
				continue
			}
			switch h.ToolName {
			case "es_log_query", "execute_read", "rca_grep":
			default:
				continue
			}
			switch h.HitStatus {
			case "hits", "empty", "error":
				return true, "pass", nil
			}
		}
		return false, "no stamped es_log_query/execute_read/rca_grep hit_status", nil
	case "empty_hit_speak":
		got := agent.EvaluateEmptyHitSpeakGate(runTraceFromHits(o.ToolHits), o.FinalText)
		if !got.Allow {
			ex := got.Reason
			if got.Prompt != "" {
				ex = got.Prompt
			}
			return false, ex, nil
		}
		return true, "pass", nil
	default:
		return false, fmt.Sprintf("unknown check type %q", check.Type), nil
	}
}

func runTraceFromHits(hits []ToolHit) *agent.RunTrace {
	tr := &agent.RunTrace{}
	for _, h := range hits {
		res := map[string]any{}
		if h.HitStatus != "" {
			res["hit_status"] = h.HitStatus
		}
		if h.QueriedIndex != "" {
			res["queried_index"] = h.QueriedIndex
		}
		if h.Repo != "" {
			res["repo"] = h.Repo
		}
		tr.ToolCalls = append(tr.ToolCalls, agent.ToolCallRecord{
			ToolName: h.ToolName,
			Result:   res,
			Error:    h.Error,
			Blocked:  h.Blocked,
		})
	}
	return tr
}

func jsonValueString(v any) string {
	switch x := v.(type) {
	case nil:
		return "null"
	case bool:
		if x {
			return "true"
		}
		return "false"
	case float64:
		// encoding/json numbers are float64
		if x == float64(int64(x)) {
			return fmt.Sprintf("%d", int64(x))
		}
		return fmt.Sprintf("%v", x)
	case string:
		return x
	default:
		b, err := json.Marshal(x)
		if err != nil {
			return fmt.Sprintf("%v", x)
		}
		return string(b)
	}
}
