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

func TestRouteBest_shortTagEsDoesNotMatchAccess(t *testing.T) {
	metas := []SkillMeta{{
		Name:        "rca-sync-archive-migrate",
		Description: "实时存档迁移 union-archiver-manager",
		Tags:        []string{"es", "rca"},
	}}
	q := "需要看看access-service有没有收到游戏启动成功事件的时间和vm-manager有没有startGame成功"
	_, ok := RouteBest(q, metas, RouteOptions{MinScore: 5})
	if ok {
		t.Fatal("tag es must not match via substring of access")
	}
}

func TestTokenize_splitsCJKAndLatin(t *testing.T) {
	toks := tokenize("需要看看access-service有没有")
	if _, ok := toks["access"]; !ok {
		t.Fatalf("want access token, got %v", toks)
	}
	if _, ok := toks["service"]; !ok {
		t.Fatalf("want service token, got %v", toks)
	}
}

func TestRouteBest_runnerUpScore(t *testing.T) {
	metas := []SkillMeta{
		{Name: "alpha-skill", Description: "cluster ops", Tags: []string{"kubernetes"}},
		{Name: "beta-other", Description: "cluster ops", Tags: []string{"kubernetes"}},
	}
	m, ok := RouteBest("debug kubernetes pod crash", metas, RouteOptions{})
	if !ok {
		t.Fatal("both should pass min via tag kubernetes")
	}
	if m.RunnerUpScore < 5 {
		t.Fatalf("want runner-up >= 5, got %+v", m)
	}
}
