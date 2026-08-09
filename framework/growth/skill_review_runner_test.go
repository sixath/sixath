package growth

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestSkillReviewRunner_memoryOnlyClearsMemory(t *testing.T) {
	var skillC, memC bool
	deps := RunnerDeps{
		ClearGrowthPending: func(ctx context.Context, sessionID string, cs, cm bool) error {
			skillC = cs
			memC = cm
			return nil
		},
		ProposeSkillPatches: func(ctx context.Context, job ReviewJob, _, _ string) ([]Patch, error) {
			_ = ctx
			_ = job
			return nil, nil
		},
	}
	r := &SkillReviewRunner{deps: deps}
	job := ReviewJob{
		SessionID:     "m2",
		WorkspaceRoot: t.TempDir(),
		PendingSkill:  false,
		PendingMemory: true,
	}
	if err := r.Run(context.Background(), job); err != nil {
		t.Fatal(err)
	}
	if skillC || !memC {
		t.Fatalf("clear skill=%v mem=%v", skillC, memC)
	}
}

// T1（spec §8）：假 LLM 固定 patch → 写盘 + skills index generation bump。
func TestSkillReviewRunner_T1FakeLLMPatchToFSAndIndexGen(t *testing.T) {
	workspace := t.TempDir()
	if err := os.MkdirAll(filepath.Join(workspace, "skills"), 0o755); err != nil {
		t.Fatal(err)
	}
	before := DefaultSkillsIndexTracker.Generation(workspace)
	deps := RunnerDeps{
		ClearGrowthPending: func(ctx context.Context, sessionID string, cs, cm bool) error {
			return nil
		},
		ProposeSkillPatches: func(ctx context.Context, job ReviewJob, _, _ string) ([]Patch, error) {
			return []Patch{{
				Path:    "skills/t1/SKILL.md",
				Op:      OpCreate,
				Content: "---\nname: t1\ndescription: phase2 T1\n---\n# t1",
			}}, nil
		},
	}
	r := &SkillReviewRunner{deps: deps}
	job := ReviewJob{SessionID: "t1", WorkspaceRoot: workspace, PendingSkill: true}
	if err := r.Run(context.Background(), job); err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(workspace, "skills", "t1", "SKILL.md")
	if _, err := os.Stat(want); err != nil {
		t.Fatalf("patch not on disk: %v", err)
	}
	after := DefaultSkillsIndexTracker.Generation(workspace)
	if after != before+1 {
		t.Fatalf("generation before=%d after=%d", before, after)
	}
}

func TestSkillReviewRunner_invalidatesCacheAfterApply(t *testing.T) {
	var invalidated string
	deps := RunnerDeps{
		ClearGrowthPending: func(ctx context.Context, sessionID string, cs, cm bool) error {
			return nil
		},
		ProposeSkillPatches: func(ctx context.Context, job ReviewJob, _, _ string) ([]Patch, error) {
			return []Patch{{Path: "skills/test/SKILL.md", Op: OpCreate, Content: "---\nname: test\ndescription: x\n---\n# test"}}, nil
		},
		InvalidateSkillsCache: func(ctx context.Context, workspace string) {
			invalidated = workspace
		},
	}
	r := &SkillReviewRunner{deps: deps}
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "skills"), 0o755); err != nil {
		t.Fatal(err)
	}
	job := ReviewJob{
		SessionID:     "s3",
		WorkspaceRoot: dir,
		PendingSkill:  true,
	}
	if err := r.Run(context.Background(), job); err != nil {
		t.Fatal(err)
	}
	if invalidated != dir {
		t.Fatalf("expected cache invalidation for %q, got %q", dir, invalidated)
	}
}

func TestSkillReviewRunner_combinedReview(t *testing.T) {
	var skillC, memC bool
	var invalidated string
	var memNotified string
	deps := RunnerDeps{
		ClearGrowthPending: func(ctx context.Context, sessionID string, cs, cm bool) error {
			if cs {
				skillC = true
			}
			if cm {
				memC = true
			}
			return nil
		},
		ProposeCombinedReview: func(ctx context.Context, job ReviewJob, _, _ string) ([]Patch, bool, error) {
			return []Patch{{Path: "skills/combined/SKILL.md", Op: OpCreate, Content: "---\nname: combined\ndescription: x\n---\n# combined"}}, true, nil
		},
		InvalidateSkillsCache: func(ctx context.Context, workspace string) {
			invalidated = workspace
		},
		MemoryNotify: func(ctx context.Context, sessionID string) {
			memNotified = sessionID
		},
	}
	r := &SkillReviewRunner{deps: deps}
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "skills"), 0o755); err != nil {
		t.Fatal(err)
	}
	job := ReviewJob{
		SessionID:     "c1",
		WorkspaceRoot: dir,
		PendingSkill:  true,
		PendingMemory: true,
	}
	if err := r.Run(context.Background(), job); err != nil {
		t.Fatal(err)
	}
	if !skillC || !memC {
		t.Fatalf("expected both cleared, skill=%v mem=%v", skillC, memC)
	}
	if invalidated != dir {
		t.Fatalf("expected cache invalidation for %q, got %q", dir, invalidated)
	}
	if memNotified != "c1" {
		t.Fatalf("expected memory notify for c1, got %q", memNotified)
	}
}

func TestSkillReviewRunner_combinedFallsBackOnError(t *testing.T) {
	var skillC, memC bool
	deps := RunnerDeps{
		ClearGrowthPending: func(ctx context.Context, sessionID string, cs, cm bool) error {
			if cs {
				skillC = true
			}
			if cm {
				memC = true
			}
			return nil
		},
		ProposeCombinedReview: func(ctx context.Context, job ReviewJob, _, _ string) ([]Patch, bool, error) {
			return nil, false, errIgnored
		},
		ProposeSkillPatches: func(ctx context.Context, job ReviewJob, _, _ string) ([]Patch, error) {
			return nil, nil
		},
		MemoryNotify: func(ctx context.Context, sessionID string) {},
	}
	r := &SkillReviewRunner{deps: deps}
	job := ReviewJob{
		SessionID:     "cf",
		WorkspaceRoot: t.TempDir(),
		PendingSkill:  true,
		PendingMemory: true,
	}
	if err := r.Run(context.Background(), job); err != nil {
		t.Fatalf("combined error should degrade to sequential: %v", err)
	}
	if !skillC || !memC {
		t.Fatalf("expected sequential fallback, skill=%v mem=%v", skillC, memC)
	}
}

func TestSkillReviewRunner_combinedFallbackWhenNil(t *testing.T) {
	// When ProposeCombinedReview is nil but both flags set, falls back to sequential.
	var skillC, memC bool
	deps := RunnerDeps{
		ClearGrowthPending: func(ctx context.Context, sessionID string, cs, cm bool) error {
			if cs {
				skillC = true
			}
			if cm {
				memC = true
			}
			return nil
		},
		ProposeSkillPatches: func(ctx context.Context, job ReviewJob, _, _ string) ([]Patch, error) {
			return nil, nil
		},
		MemoryNotify: func(ctx context.Context, sessionID string) {},
	}
	r := &SkillReviewRunner{deps: deps}
	job := ReviewJob{
		SessionID:     "f1",
		WorkspaceRoot: t.TempDir(),
		PendingSkill:  true,
		PendingMemory: true,
	}
	if err := r.Run(context.Background(), job); err != nil {
		t.Fatal(err)
	}
	if !skillC || !memC {
		t.Fatalf("expected both cleared via fallback, skill=%v mem=%v", skillC, memC)
	}
}

// B1: 记忆状态注入到 prompt 摘要。
func TestSkillReviewRunner_MemoryStateInjectedIntoSummary(t *testing.T) {
	var captured string
	deps := RunnerDeps{
		ClearGrowthPending: func(ctx context.Context, sessionID string, cs, cm bool) error {
			return nil
		},
		ProposeSkillPatches: func(ctx context.Context, job ReviewJob, _, summary string) ([]Patch, error) {
			captured = summary
			return nil, nil
		},
		MemoryState: func(ctx context.Context, sessionID string) (string, error) {
			return "dirty=2 last_sync=2026-05-18T10:00:00Z", nil
		},
	}
	r := &SkillReviewRunner{deps: deps}
	job := ReviewJob{SessionID: "m1", WorkspaceRoot: t.TempDir(), PendingSkill: true}
	if err := r.Run(context.Background(), job); err != nil {
		t.Fatal(err)
	}
	if !contains(captured, "Memory state") || !contains(captured, "dirty=2") {
		t.Fatalf("memory state not injected, got: %q", captured)
	}
}

// B1: MemoryState 报错时不应阻断 run。
func TestSkillReviewRunner_MemoryStateErrorIgnored(t *testing.T) {
	deps := RunnerDeps{
		ClearGrowthPending: func(ctx context.Context, sessionID string, cs, cm bool) error {
			return nil
		},
		ProposeSkillPatches: func(ctx context.Context, job ReviewJob, _, _ string) ([]Patch, error) {
			return nil, nil
		},
		MemoryState: func(ctx context.Context, sessionID string) (string, error) {
			return "", errIgnored
		},
	}
	r := &SkillReviewRunner{deps: deps}
	job := ReviewJob{SessionID: "m2", WorkspaceRoot: t.TempDir(), PendingSkill: true}
	if err := r.Run(context.Background(), job); err != nil {
		t.Fatalf("MemoryState error should be swallowed: %v", err)
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

var errIgnored = errIgnoredT{}

type errIgnoredT struct{}

func (errIgnoredT) Error() string { return "ignored" }