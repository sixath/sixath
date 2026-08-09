package growth

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestParsePatchBatchJSON_empty(t *testing.T) {
	p, err := ParsePatchBatchJSON([]byte(""))
	if err != nil || len(p) != 0 {
		t.Fatalf("got %v %#v", err, p)
	}
}

func TestParsePatchBatchJSON_create(t *testing.T) {
	raw := `[{"path":"a/b.txt","op":"create","content":"hello"}]`
	p, err := ParsePatchBatchJSON([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	if len(p) != 1 || p[0].Op != OpCreate || p[0].Path != "a/b.txt" || p[0].Content != "hello" {
		t.Fatalf("%#v", p)
	}
}

func TestParsePatchBatchJSON_patchOp(t *testing.T) {
	raw := `[{"path":"x","op":"patch","old":"a","new":"b"}]`
	p, err := ParsePatchBatchJSON([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	if p[0].Old != "a" || p[0].New != "b" {
		t.Fatal(p)
	}
}

func TestParsePatchBatchJSON_invalidOp(t *testing.T) {
	raw := `[{"path":"x","op":"nope"}]`
	_, err := ParsePatchBatchJSON([]byte(raw))
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestSkillReviewRunner_applyAndClearSkill(t *testing.T) {
	root := t.TempDir()
	skillsDir := filepath.Join(root, "skills")
	if err := os.MkdirAll(skillsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	var clearSkill, clearMem bool
	deps := RunnerDeps{
		ClearGrowthPending: func(ctx context.Context, sessionID string, cs, cm bool) error {
			_ = ctx
			if sessionID != "sid1" {
				t.Fatalf("session %q", sessionID)
			}
			clearSkill = cs
			clearMem = cm
			return nil
		},
		Transcript: func(ctx context.Context, sessionID string) (string, error) {
			return "T", nil
		},
		ProposeSkillPatches: func(ctx context.Context, job ReviewJob, transcript, summary string) ([]Patch, error) {
			if transcript != "T" {
				t.Fatalf("transcript %q", transcript)
			}
			if summary == "" {
				t.Fatal("expected non-empty summary")
			}
			return []Patch{{
				Path:    filepath.Join("skills", "demo", "SKILL.md"),
				Op:      OpCreate,
				Content: "---\nname: demo\ndescription: d\n---\nbody\n",
			}}, nil
		},
	}
	r := &SkillReviewRunner{deps: deps}
	job := ReviewJob{
		SessionID:     "sid1",
		WorkspaceKey:  root,
		WorkspaceRoot: root,
		PendingSkill:  true,
		PendingMemory: false,
	}
	if err := r.Run(context.Background(), job); err != nil {
		t.Fatal(err)
	}
	if !clearSkill || clearMem {
		t.Fatalf("clear flags skill=%v mem=%v", clearSkill, clearMem)
	}
	p := filepath.Join(root, "skills", "demo", "SKILL.md")
	if _, err := os.Stat(p); err != nil {
		t.Fatal(err)
	}
}
