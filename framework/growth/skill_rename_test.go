package growth

import "testing"

func TestExtractSkillRenamesFromPatches(t *testing.T) {
	batch := []Patch{{
		Path: "skills/foo/SKILL.md",
		Op:   OpPatch,
		Old:  "---\nname: daily-report\ndescription: old\n---\n",
		New:  "---\nname: daily-report-v2\ndescription: new\n---\n",
	}}
	got := ExtractSkillRenamesFromPatches(batch)
	if got["daily-report"] != "daily-report-v2" {
		t.Fatalf("renames = %#v", got)
	}
}

func TestExtractSkillRenamesFromPatches_ignoresNonSkillMD(t *testing.T) {
	batch := []Patch{{
		Path: "skills/foo/run.sh",
		Op:   OpPatch,
		Old:  "a",
		New:  "b",
	}}
	if len(ExtractSkillRenamesFromPatches(batch)) != 0 {
		t.Fatal("expected empty renames")
	}
}

func TestRewriteSkillExecutePayload(t *testing.T) {
	renames := map[string]string{"daily-report": "daily-report-v2"}
	newPayload, changed := RewriteSkillExecutePayload("daily-report/scripts/run.sh", renames)
	if !changed || newPayload != "daily-report-v2/scripts/run.sh" {
		t.Fatalf("got %q changed=%v", newPayload, changed)
	}
}

func TestRewriteSkillExecutePayload_noMatch(t *testing.T) {
	renames := map[string]string{"other": "x"}
	_, changed := RewriteSkillExecutePayload("daily-report/scripts/run.sh", renames)
	if changed {
		t.Fatal("expected unchanged")
	}
}
