package growth

import (
	"fmt"
	"path/filepath"
	"strings"
)

// Op describes what to do with a path in a patch batch.
type Op string

const (
	OpCreate Op = "create"
	OpPatch  Op = "patch"
	OpDelete Op = "delete"
)

// Patch is a single filesystem intent item (validation only; no I/O).
type Patch struct {
	Path    string
	Op      Op
	Content string // full file body for OpCreate
	Old     string // required non-empty for OpPatch
	New     string // replacement for OpPatch
}

// ValidatePatchBatch checks that every patch uses a safe path under workspaceRoot
// and satisfies op-specific constraints. An empty batch is valid (nil).
func ValidatePatchBatch(workspaceRoot string, batch []Patch) error {
	if len(batch) == 0 {
		return nil
	}
	rootAbs, err := filepath.Abs(workspaceRoot)
	if err != nil {
		return fmt.Errorf("growth: resolve workspace root: %w", err)
	}
	rootClean := filepath.Clean(rootAbs)

	for i := range batch {
		p := batch[i]
		if err := validatePatchPath(rootClean, p.Path); err != nil {
			return fmt.Errorf("growth: patch[%d] path %q: %w", i, p.Path, err)
		}
		if err := validatePatchOp(p); err != nil {
			return fmt.Errorf("growth: patch[%d] op %q: %w", i, p.Op, err)
		}
	}
	return nil
}

func validatePatchPath(rootClean string, p string) error {
	if strings.TrimSpace(p) == "" {
		return fmt.Errorf("empty path")
	}

	var full string
	if filepath.IsAbs(p) {
		absP, err := filepath.Abs(p)
		if err != nil {
			return fmt.Errorf("resolve absolute path: %w", err)
		}
		full = filepath.Clean(absP)
	} else {
		full = filepath.Clean(filepath.Join(rootClean, p))
	}

	if full != rootClean && !strings.HasPrefix(full, rootClean+string(filepath.Separator)) {
		return fmt.Errorf("path resolves outside workspace (root %q, resolved %q)", rootClean, full)
	}
	return nil
}

func validatePatchOp(p Patch) error {
	switch p.Op {
	case OpCreate, OpPatch, OpDelete:
		if p.Op == OpPatch && strings.TrimSpace(p.Old) == "" {
			return fmt.Errorf("patch op requires non-empty Old")
		}
		return nil
	default:
		if p.Op == "" {
			return fmt.Errorf("missing op (expected %q, %q, or %q)", OpCreate, OpPatch, OpDelete)
		}
		return fmt.Errorf("invalid op %q (expected %q, %q, or %q)", p.Op, OpCreate, OpPatch, OpDelete)
	}
}
