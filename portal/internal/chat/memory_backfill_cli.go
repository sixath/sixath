package chat

import (
	"fmt"
	"strings"
	"time"

	"github.com/sixath/framework/memory"
)

// BackfillCLIFlags is the parsed CLI surface for backfill-vectors.
type BackfillCLIFlags struct {
	Force      bool
	DryRun     bool
	Scopes     []memory.Scope // empty → NewUnitBackfiller defaults (session+user)
	BatchSize  int
	BatchSleep time.Duration
}

// ParseBackfillArgs maps CLI args (without program name) onto BackfillCLIFlags.
// Supported: --force, --dry-run, --scope session|user|all, --batch N, --sleep duration.
func ParseBackfillArgs(args []string) (BackfillCLIFlags, error) {
	out := BackfillCLIFlags{
		BatchSize:  50,
		BatchSleep: 200 * time.Millisecond,
	}
	scopeSet := false
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--force":
			out.Force = true
		case a == "--dry-run":
			out.DryRun = true
		case a == "--scope":
			if i+1 >= len(args) {
				return out, fmt.Errorf("memory: --scope requires a value")
			}
			i++
			scopes, err := parseBackfillScope(args[i])
			if err != nil {
				return out, err
			}
			out.Scopes = scopes
			scopeSet = true
		case strings.HasPrefix(a, "--scope="):
			scopes, err := parseBackfillScope(strings.TrimPrefix(a, "--scope="))
			if err != nil {
				return out, err
			}
			out.Scopes = scopes
			scopeSet = true
		case a == "--batch":
			if i+1 >= len(args) {
				return out, fmt.Errorf("memory: --batch requires a value")
			}
			i++
			n, err := parsePositiveInt(args[i], "--batch")
			if err != nil {
				return out, err
			}
			out.BatchSize = n
		case strings.HasPrefix(a, "--batch="):
			n, err := parsePositiveInt(strings.TrimPrefix(a, "--batch="), "--batch")
			if err != nil {
				return out, err
			}
			out.BatchSize = n
		case a == "--sleep":
			if i+1 >= len(args) {
				return out, fmt.Errorf("memory: --sleep requires a value")
			}
			i++
			d, err := time.ParseDuration(args[i])
			if err != nil {
				return out, fmt.Errorf("memory: --sleep: %w", err)
			}
			out.BatchSleep = d
		case strings.HasPrefix(a, "--sleep="):
			d, err := time.ParseDuration(strings.TrimPrefix(a, "--sleep="))
			if err != nil {
				return out, fmt.Errorf("memory: --sleep: %w", err)
			}
			out.BatchSleep = d
		case a == "--conf" || strings.HasPrefix(a, "--conf="):
			// handled by main's flag set; ignore here when args are pre-filtered
			if a == "--conf" {
				i++
			}
		default:
			return out, fmt.Errorf("memory: unknown backfill flag %q", a)
		}
	}
	if !scopeSet {
		out.Scopes = []memory.Scope{memory.ScopeSession, memory.ScopeUser}
	}
	return out, nil
}

func parseBackfillScope(v string) ([]memory.Scope, error) {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "session":
		return []memory.Scope{memory.ScopeSession}, nil
	case "user":
		return []memory.Scope{memory.ScopeUser}, nil
	case "all", "":
		return []memory.Scope{memory.ScopeSession, memory.ScopeUser}, nil
	default:
		return nil, fmt.Errorf("memory: --scope must be session|user|all, got %q", v)
	}
}

func parsePositiveInt(s, name string) (int, error) {
	var n int
	if _, err := fmt.Sscanf(s, "%d", &n); err != nil || n <= 0 {
		return 0, fmt.Errorf("memory: %s must be a positive int, got %q", name, s)
	}
	return n, nil
}

// ToBackfillConfig fills Units/Index/Embedder/EmbedTripped from the caller;
// flag fields come from BackfillCLIFlags.
func (f BackfillCLIFlags) ToBackfillConfig(units memory.SessionUnitsBackend, idx memory.UnitVectorIndex, emb memory.UnitEmbedder) memory.BackfillConfig {
	return memory.BackfillConfig{
		Units:        units,
		Index:        idx,
		Embedder:     emb,
		Force:        f.Force,
		DryRun:       f.DryRun,
		BatchSize:    f.BatchSize,
		BatchSleep:   f.BatchSleep,
		Scopes:       f.Scopes,
		EmbedTripped: memoryEmbedTripped,
	}
}
