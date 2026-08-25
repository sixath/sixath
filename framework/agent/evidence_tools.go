package agent

// IsBoundEvidenceTool reports whether name is a bound evidence tool (spec §4.3).
func IsBoundEvidenceTool(name string) bool {
	switch name {
	case "es_log_query", "jaeger_trace", "execute_read", "list_tables", "describe_table",
		"rca_read", "rca_grep", "rca_glob", "rca_symbol":
		return true
	default:
		return false
	}
}

// IsSkillsFamilyToolName reports whether name is a skills-family tool.
func IsSkillsFamilyToolName(name string) bool {
	switch name {
	case "skills_list", "load_skill", "skill_view", "skill_manage", "read_skill_file", "execute_skill_script":
		return true
	default:
		return false
	}
}

// HasSuccessfulBoundEvidence is true when this turn already ran a bound evidence tool
// with Error=="" and !Blocked. Nil or empty traces are false.
func HasSuccessfulBoundEvidence(trace *RunTrace) bool {
	if trace == nil {
		return false
	}
	for _, rec := range trace.ToolCalls {
		if rec.Error != "" || rec.Blocked {
			continue
		}
		if IsBoundEvidenceTool(rec.ToolName) {
			return true
		}
	}
	return false
}
