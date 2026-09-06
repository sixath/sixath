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

func TestTokenize_cjkNGrams(t *testing.T) {
	toks := tokenize("这个流水卸载存档失败的原因")
	if _, ok := toks["卸载"]; !ok {
		t.Fatalf("want 2-gram 卸载, got %v", toks)
	}
	if _, ok := toks["存档"]; !ok {
		t.Fatalf("want 2-gram 存档, got %v", toks)
	}
	if _, ok := toks["卸载存"]; !ok {
		t.Fatalf("want 3-gram 卸载存, got %v", toks)
	}
}

func TestRouteBest_archiveUnloadPrefersInstanceLog(t *testing.T) {
	metas := []SkillMeta{
		{
			Name:        "scheduling-flow-trace",
			Description: "通过 ES 日志、Jaeger 链路追踪和数据库，端到端排查两套调度系统的失败原因。当用户提供 trace_id / flow_id 并询问调度失败、分配 VM 失败、资源占用、锁定超时等问题时使用。",
		},
		{
			Name:        "vm-xagent-log-search",
			Description: "进入云游戏实例查看 cgvmagent 日志。用于卸载存档失败、存档失败、实例错误。用户只给 flow_id 也必须立刻进实例。",
			Tags:        []string{"卸载存档", "存档失败", "实例错误", "实例日志"},
		},
	}
	q := "9999_zjvplfx19vdv 这个流水卸载存档失败的原因"
	m, ok := RouteBest(q, metas, RouteOptions{MinScore: 5})
	if !ok {
		t.Fatal("expected a route match")
	}
	if m.Name != "vm-xagent-log-search" {
		t.Fatalf("want vm-xagent-log-search for instance archive error, got %s score=%d runner=%d", m.Name, m.Score, m.RunnerUpScore)
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
