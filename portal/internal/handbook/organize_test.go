package handbook

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func orgFacts() (*Facts, map[string]*Card) {
	files := []File{
		{Path: "cmd/server/main.go", Lang: "go", Size: 1, Hash: hashOf("1")},
		{Path: "internal/order/store.go", Lang: "go", Size: 1, Hash: hashOf("2")},
		{Path: "internal/order/service.go", Lang: "go", Size: 1, Hash: hashOf("3")},
		{Path: "internal/pay/client.go", Lang: "go", Size: 1, Hash: hashOf("4")},
		{Path: "internal/pay/client_test.go", Lang: "go", Size: 1, Hash: hashOf("5"), Test: true},
		{Path: "README.md", Lang: "markdown", Size: 1, Hash: hashOf("6")},
	}
	cards := map[string]*Card{}
	for _, f := range files {
		if CardEligible(f) {
			cards[f.Path] = &Card{Purpose: "职责 " + f.Path, Hash: f.Hash}
		}
	}
	return &Facts{Files: files}, cards
}

func skeletonReply(assign map[string]string) func(string) string {
	return func(string) string {
		b, _ := json.Marshal(map[string]any{
			"stages": []map[string]string{
				{"id": "boot", "title": "启动", "summary": "进程启动"},
				{"id": "order", "title": "下单", "summary": "订单处理"},
				{"id": "pay", "title": "支付", "summary": "调用支付"},
				{"id": "Bad ID", "title": "非法"},
			},
			"assign": assign,
		})
		return string(b)
	}
}

func TestInferSkeleton_AssignsDirectories(t *testing.T) {
	f, cards := orgFacts()
	m := (&fakeModel{}).on("执行阶段", skeletonReply(map[string]string{
		"cmd/server": "boot", "internal/order": "order", "internal/pay": "nope",
	}))
	now := time.Unix(1000, 0).UTC()
	var u usage
	sk, err := inferSkeleton(context.Background(), m, "svc", f, cards, "c1", now, &u)
	if err != nil {
		t.Fatal(err)
	}
	if sk.FallbackAreas || sk.Commit != "c1" || !sk.BuiltAt.Equal(now) || sk.BaseFiles != 4 || sk.PromptVersion != LLMPromptVersion {
		t.Fatalf("skeleton meta %#v", sk)
	}
	ids := []string{}
	for _, s := range sk.Stages {
		ids = append(ids, s.ID)
	}
	if strings.Join(ids, ",") != "boot,order" {
		t.Fatalf("stages without files or with invalid ids must be dropped: %v", ids)
	}
	if sk.Files["internal/order/store.go"].Stage != "order" || sk.Files["internal/pay/client.go"].Stage != "" ||
		sk.Files["internal/order/store.go"].CardHash != hashOf("2") {
		t.Fatalf("files %#v", sk.Files)
	}
	if _, ok := sk.Files["internal/pay/client_test.go"]; ok {
		t.Fatal("tests are not organized")
	}
	if strings.Join(sk.TopDirs, ",") != "cmd,internal" {
		t.Fatalf("top dirs %v", sk.TopDirs)
	}
	if !strings.Contains(m.calls[0], "internal/order（2 个文件）") || !strings.Contains(m.calls[0], "职责 internal/order/store.go") {
		t.Fatalf("prompt must list dirs with purposes: %s", m.calls[0])
	}
}

func TestInferSkeleton_FallsBackToAreas(t *testing.T) {
	f, cards := orgFacts()
	m := (&fakeModel{}).on("执行阶段", func(string) string { return `{"stages":[{"id":"only","title":"一个"}],"assign":{}}` })
	var u usage
	sk, err := inferSkeleton(context.Background(), m, "svc", f, cards, "c1", time.Now(), &u)
	if err != nil {
		t.Fatal(err)
	}
	if !sk.FallbackAreas || len(sk.Stages) != 3 || sk.Files["internal/pay/client.go"].Stage != "internal-pay" {
		t.Fatalf("fallback skeleton %#v", sk)
	}
}

func TestUpdateSkeleton_Incremental(t *testing.T) {
	f, _ := orgFacts()
	sk := &Skeleton{PromptVersion: LLMPromptVersion, Stages: []Stage{{ID: "order"}, {ID: "pay"}}, BaseFiles: 4, TopDirs: []string{"cmd", "internal"},
		Files: map[string]FileAssign{
			"internal/order/store.go":   {Stage: "order", CardHash: hashOf("2")},
			"internal/order/service.go": {Stage: "order", CardHash: hashOf("0")},
			"internal/pay/client.go":    {Stage: "pay", CardHash: hashOf("4")},
			"internal/gone/x.go":        {Stage: "pay", CardHash: hashOf("9")},
		}}
	f.Files = append(f.Files, File{Path: "internal/order/refund/refund.go", Lang: "go", Size: 1, Hash: hashOf("7")})
	now := time.Unix(2000, 0).UTC()
	affected, changed := updateSkeleton(sk, f, "c2", now)
	if _, ok := sk.Files["internal/gone/x.go"]; ok {
		t.Fatal("removed file must be dropped")
	}
	if sk.Files["internal/order/service.go"].CardHash != hashOf("3") {
		t.Fatal("changed file must point at its new hash")
	}
	if sk.Files["internal/order/refund/refund.go"].Stage != "order" {
		t.Fatalf("new file must inherit the majority stage of its nearest directory: %#v", sk.Files["internal/order/refund/refund.go"])
	}
	if sk.Files["cmd/server/main.go"].Stage != "" {
		t.Fatal("a file with no organized neighbour stays unassigned")
	}
	if !affected["order"] || !affected["pay"] || len(changed) != 4 {
		t.Fatalf("affected %v changed %v", affected, changed)
	}
	if sk.ChangedSinceRebuild != 4 || sk.Commit != "c2" || !sk.UpdatedAt.Equal(now) {
		t.Fatalf("meta %#v", sk)
	}
}

func TestRebuildReason(t *testing.T) {
	f, _ := orgFacts()
	now := time.Unix(100*86400, 0).UTC()
	base := func() *Skeleton {
		return &Skeleton{PromptVersion: LLMPromptVersion, BuiltAt: now.Add(-time.Hour), BaseFiles: 10, TopDirs: []string{"cmd", "internal"},
			Files: map[string]FileAssign{"cmd/server/main.go": {Stage: "s"}, "internal/order/store.go": {Stage: "s"}, "internal/order/service.go": {Stage: "s"}, "internal/pay/client.go": {Stage: "s"}}}
	}
	if r := rebuildReason(nil, f, now, 30); r != "none" {
		t.Fatalf("nil: %q", r)
	}
	sk := base()
	if r := rebuildReason(sk, f, now, 30); r != "" {
		t.Fatalf("fresh: %q", r)
	}
	sk.PromptVersion = "old"
	if r := rebuildReason(sk, f, now, 30); r != "prompt" {
		t.Fatalf("prompt: %q", r)
	}
	sk = base()
	sk.BuiltAt = now.Add(-31 * 24 * time.Hour)
	if r := rebuildReason(sk, f, now, 30); r != "age" {
		t.Fatalf("age: %q", r)
	}
	sk = base()
	sk.ChangedSinceRebuild = 3
	if r := rebuildReason(sk, f, now, 30); r != "changes" {
		t.Fatalf("changes: %q", r)
	}
	sk = base()
	sk.TopDirs = []string{"internal"}
	if r := rebuildReason(sk, f, now, 30); r != "topdir" {
		t.Fatalf("topdir: %q", r)
	}
	sk = base()
	sk.Files["internal/pay/client.go"] = FileAssign{}
	if r := rebuildReason(sk, f, now, 30); r != "unassigned" {
		t.Fatalf("unassigned: %q", r)
	}
}
