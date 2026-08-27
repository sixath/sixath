package chat

import (
	"fmt"
	"log"
	"strings"

	"github.com/sixath/framework/mea"
)

func AutoChecks(goal string) (out []mea.AcceptanceCheck) {
	defer func() {
		if rec := recover(); rec != nil {
			out = nil
			log.Printf("mea_autochecks=error recovered=%v", rec)
		}
	}()
	goal = strings.TrimSpace(goal)
	if goal == "" || !ShouldApplyEvidenceGate(nil, goal) {
		return nil
	}
	return []mea.AcceptanceCheck{
		{Type: "trace_hit_status"},
		{Type: "empty_hit_speak"},
	}
}

func ResolveAcceptanceChecks(fence []mea.AcceptanceCheck, fenceOK bool, goal string) []mea.AcceptanceCheck {
	if fenceOK {
		return fence
	}
	return AutoChecks(goal)
}

func traceOnlyChecks(checks []mea.AcceptanceCheck) bool {
	if len(checks) == 0 {
		return false
	}
	for _, c := range checks {
		switch c.Type {
		case "trace_hit_status", "empty_hit_speak":
		default:
			return false
		}
	}
	return true
}

func ShouldUseMEA(enabled bool, workspace string, checks []mea.AcceptanceCheck, acceptance []string) bool {
	if !enabled {
		return false
	}
	if len(checks) == 0 && len(acceptance) == 0 {
		return false
	}
	if strings.TrimSpace(workspace) != "" {
		return true
	}
	return traceOnlyChecks(checks) && len(acceptance) == 0
}

func MEAAcceptancePrompt(checks []mea.AcceptanceCheck, acceptance []string) string {
	var b strings.Builder
	if traceOnlyChecks(checks) && len(acceptance) == 0 {
		b.WriteString("本轮用 ES/SQL/grep 调查。验收读工具 JSON 的 hit_status 与终答，不要为过检去 write_file。")
		return b.String()
	}
	if len(checks) > 0 {
		b.WriteString("[MEA acceptance — produce environment state that passes these checks]\n")
		for _, ck := range checks {
			fmt.Fprintf(&b, "- type=%s path=%s pattern=%s json_path=%s equals=%s\n",
				ck.Type, ck.Path, ck.Pattern, ck.JSONPath, ck.Equals)
		}
		return strings.TrimSuffix(b.String(), "\n")
	}
	if len(acceptance) > 0 {
		b.WriteString("[MEA acceptance — satisfy these observable criteria]\n")
		for _, line := range acceptance {
			fmt.Fprintf(&b, "- %s\n", line)
		}
		return strings.TrimSuffix(b.String(), "\n")
	}
	return ""
}
