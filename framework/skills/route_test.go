package skills

import "testing"

func TestRouteBest_nameInQuery(t *testing.T) {
	metas := []SkillMeta{
		{Name: "archive-move-ops", Description: "archive migration logs"},
		{Name: "mysql-employees", Description: "sql analysis"},
	}
	m, ok := RouteBest("help me with archive-move-ops flow", metas, RouteOptions{})
	if !ok {
		t.Fatal("expected match")
	}
	if m.Name != "archive-move-ops" {
		t.Fatalf("want archive-move-ops, got %s score=%d", m.Name, m.Score)
	}
}

func TestRouteBest_tagHit(t *testing.T) {
	metas := []SkillMeta{
		{Name: "foo", Description: "x", Tags: []string{"kubernetes"}},
		{Name: "bar", Description: "y"},
	}
	m, ok := RouteBest("debug kubernetes pod crash", metas, RouteOptions{})
	if !ok || m.Name != "foo" {
		t.Fatalf("expected foo, got ok=%v name=%s", ok, m.Name)
	}
}

func TestRouteBest_belowThreshold(t *testing.T) {
	metas := []SkillMeta{{Name: "unrelated-skill", Description: "nothing"}}
	_, ok := RouteBest("hello", metas, RouteOptions{MinScore: 20})
	if ok {
		t.Fatal("expected no match")
	}
}
