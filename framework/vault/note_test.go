package vault

import "testing"

func TestParseNoteFrontmatter(t *testing.T) {
	raw := "---\ntype: experience\nstatus: promoted\ntags: [es, 流水]\nsource: session:abc\ncreated: 2026-10-14\n---\n\n# 标题\n正文..."
	n, err := parseNote(raw, "/v/ops/es-flow.md")
	if err != nil {
		t.Fatal(err)
	}
	if n.Type != "experience" || n.Status != StatusPromoted || len(n.Tags) != 2 {
		t.Fatalf("bad note: %+v", n)
	}
	if n.Source != "session:abc" {
		t.Fatalf("bad source: %s", n.Source)
	}
	if n.Body == "" {
		t.Fatal("body should not be empty")
	}
}

func TestParseNoteStatusInferredFromZone(t *testing.T) {
	raw := "---\ntype: case\n---\n\nbody"
	n, err := parseNote(raw, "/v/_agent_notes/x.md")
	if err != nil {
		t.Fatal(err)
	}
	if n.Status != StatusPending {
		t.Fatalf("pending zone should infer pending status, got %s", n.Status)
	}
}

func TestZoneOf(t *testing.T) {
	v := &Vault{Root: "/v"}
	cases := map[string]Zone{
		"/v/_agent_notes/a.md": zonePending,
		"/v/ops/a.md":          zonePromoted,
		"/v/mine/a.md":         zoneMine,
		"/v/shared/a.md":       zoneShared,
		"/v/other/a.md":        zoneUnknown,
	}
	for path, want := range cases {
		if got := v.zoneOf(path); got != want {
			t.Fatalf("zoneOf(%s)=%v want %v", path, got, want)
		}
	}
}
