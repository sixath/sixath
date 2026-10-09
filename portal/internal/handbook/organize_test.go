package handbook

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
	if sk.Files["internal/order/store.go"].Stage != "order" || sk.Files["internal/order/store.go"].CardHash != hashOf("2") {
		t.Fatalf("files %#v", sk.Files)
	}
	if sk.Files["internal/pay/client.go"].Stage != "order" {
		t.Fatalf("a directory with an invalid stage takes its neighbours' majority stage: %#v", sk.Files)
	}
	if r := rebuildReason(sk, f, now, 30); r != "" {
		t.Fatalf("a fresh skeleton must not need a rebuild: %q", r)
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
	if !sk.FallbackAreas || sk.FallbackReason != FallbackFewStages || len(sk.Stages) != 3 || sk.Files["internal/pay/client.go"].Stage != "internal-pay" {
		t.Fatalf("fallback skeleton %#v", sk)
	}
}

func TestInferSkeleton_SubdirectoryInheritsParent(t *testing.T) {
	f, cards := orgFacts()
	f.Files = append(f.Files, File{Path: "internal/order/refund/refund.go", Lang: "go", Size: 1, Hash: hashOf("7")})
	m := (&fakeModel{}).on("执行阶段", skeletonReply(map[string]string{
		"cmd/server": "boot", "internal/order": "order", "internal/pay": "pay",
	}))
	now := time.Unix(1000, 0).UTC()
	var u usage
	sk, err := inferSkeleton(context.Background(), m, "svc", f, cards, "c1", now, &u)
	if err != nil {
		t.Fatal(err)
	}
	if sk.Files["internal/order/refund/refund.go"].Stage != "order" || sk.Files["internal/pay/client.go"].Stage != "pay" {
		t.Fatalf("files %#v", sk.Files)
	}
	if r := rebuildReason(sk, f, now, 30); r != "" {
		t.Fatalf("a fresh skeleton must not need a rebuild: %q", r)
	}
}

func TestInferSkeleton_TransportErrorPropagates(t *testing.T) {
	f, cards := orgFacts()
	boom := errors.New("boom")
	m := &fakeModel{failErr: boom}
	var u usage
	if _, err := inferSkeleton(context.Background(), m, "svc", f, cards, "c1", time.Now(), &u); !errors.Is(err, boom) || errors.Is(err, errBadReply) {
		t.Fatalf("err %v", err)
	}
}

func TestInferSkeleton_NoFilesSkipsModel(t *testing.T) {
	m := &fakeModel{}
	var u usage
	sk, err := inferSkeleton(context.Background(), m, "svc", &Facts{Files: []File{{Path: "README.md", Lang: "markdown", Size: 1}}}, nil, "c1", time.Now(), &u)
	if err != nil || m.callCount() != 0 || len(sk.Stages) != 0 || len(sk.Files) != 0 || sk.FallbackAreas {
		t.Fatalf("%#v %v calls=%d", sk, err, m.callCount())
	}
}

func TestInferSkeleton_ClampsAndDedupesStages(t *testing.T) {
	var files []File
	var stages []map[string]string
	assign := map[string]string{}
	for i := 0; i < 20; i++ {
		d, id := fmt.Sprintf("d%02d", i), fmt.Sprintf("s%02d", i)
		for k := 0; k <= i; k++ {
			files = append(files, File{Path: fmt.Sprintf("%s/f%02d.go", d, k), Lang: "go", Size: 1, Hash: hashOf("a")})
		}
		stages = append(stages, map[string]string{"id": id, "title": "标题\n 换行  " + id})
		assign[d] = id
	}
	stages = append(stages, map[string]string{"id": "s00", "title": "重复"})
	reply, _ := json.Marshal(map[string]any{"stages": stages, "assign": assign})
	m := (&fakeModel{}).on("执行阶段", func(string) string { return string(reply) })
	f := &Facts{Files: files}
	var u usage
	sk, err := inferSkeleton(context.Background(), m, "svc", f, nil, "c1", time.Now(), &u)
	if err != nil {
		t.Fatal(err)
	}
	if sk.FallbackAreas || len(sk.Stages) != maxStages {
		t.Fatalf("stages %#v", sk.Stages)
	}
	seen := map[string]bool{}
	for _, s := range sk.Stages {
		if seen[s.ID] || s.ID < "s05" {
			t.Fatalf("stages must be unique and the largest ones: %#v", sk.Stages)
		}
		seen[s.ID] = true
		if strings.ContainsAny(s.Title, "\n") || strings.Contains(s.Title, "  ") {
			t.Fatalf("title whitespace must collapse: %q", s.Title)
		}
	}
	if sk.Files["d00/f00.go"].Stage != "" || sk.Files["d19/f00.go"].Stage != "s19" {
		t.Fatalf("files of dropped stages become unassigned: %#v", sk.Files["d00/f00.go"])
	}
}

func TestInferSkeleton_CollapsesLargePrompt(t *testing.T) {
	long := strings.Repeat("x", 90)
	var files []File
	assign := map[string]string{}
	for i := 0; i < 2000; i++ {
		top := fmt.Sprintf("s/m%d/p/q%d", i%10, i%20)
		files = append(files, File{Path: fmt.Sprintf("%s/leaf%04d%s/f.go", top, i, long), Lang: "go", Size: 1, Hash: hashOf("b")})
		assign[top] = []string{"even", "odd"}[i%2]
	}
	reply, _ := json.Marshal(map[string]any{
		"stages": []map[string]string{{"id": "even", "title": "偶"}, {"id": "odd", "title": "奇"}},
		"assign": assign,
	})
	m := (&fakeModel{}).on("执行阶段", func(string) string { return string(reply) })
	f := &Facts{Files: files}
	if p := skeletonPrompt("svc", f, eligibleDirs(f, nil), 0); len(p) <= skeletonInputBudget {
		t.Fatalf("fixture must exceed the budget uncollapsed: %d", len(p))
	}
	var u usage
	sk, err := inferSkeleton(context.Background(), m, "svc", f, nil, "c1", time.Now(), &u)
	if err != nil {
		t.Fatal(err)
	}
	if sk.FallbackAreas || len(sk.Stages) != 2 {
		t.Fatalf("collapsed prompt must not fall back: %#v", sk.Stages)
	}
	if !strings.Contains(m.calls[0], "s/m0/p/q0（100 个文件）") || strings.Contains(m.calls[0], "leaf") {
		t.Fatalf("prompt must list collapsed dirs: %.300s", m.calls[0])
	}
	if got := sk.Files[files[3].Path].Stage; got != "odd" {
		t.Fatalf("files inherit the collapsed dir's stage: %q", got)
	}
}

func TestInferSkeleton_CollapsesToReplyCapacity(t *testing.T) {
	var files []File
	assign := map[string]string{}
	for i := 0; i < 500; i++ {
		top := fmt.Sprintf("top%02d", i%50)
		files = append(files, File{Path: fmt.Sprintf("%s/sub%03d/f.go", top, i), Lang: "go", Size: 1, Hash: hashOf("d")})
		assign[top] = []string{"even", "odd"}[i%2]
	}
	reply, _ := json.Marshal(map[string]any{
		"stages": []map[string]string{{"id": "even", "title": "偶"}, {"id": "odd", "title": "奇"}},
		"assign": assign,
	})
	m := (&fakeModel{}).on("执行阶段", func(string) string { return string(reply) })
	f := &Facts{Files: files}
	if p := skeletonPrompt("svc", f, eligibleDirs(f, nil), 0); len(p) > skeletonInputBudget {
		t.Fatalf("fixture must fit the byte budget uncollapsed so only the dir limit forces collapsing: %d", len(p))
	}
	var u usage
	sk, err := inferSkeleton(context.Background(), m, "svc", f, nil, "c1", time.Now(), &u)
	if err != nil {
		t.Fatal(err)
	}
	if sk.FallbackAreas || len(sk.Stages) != 2 || sk.Files["top01/sub001/f.go"].Stage != "odd" {
		t.Fatalf("skeleton %#v", sk.Stages)
	}
	if !strings.Contains(m.calls[0], "- top00（10 个文件）") || strings.Contains(m.calls[0], "sub") {
		t.Fatalf("prompt must list top-level dirs: %.300s", m.calls[0])
	}
	if len(m.maxTokens) != 1 || m.maxTokens[0] > 8192 || m.maxTokens[0] != skeletonMaxTokens(50) {
		t.Fatalf("max tokens %v", m.maxTokens)
	}
}

func TestInferSkeleton_TooLargeFallsBack(t *testing.T) {
	long := strings.Repeat("y", 100)
	var files []File
	for i := 0; i < 1500; i++ {
		files = append(files, File{Path: fmt.Sprintf("t%04d%s/x%s/f.go", i, long, long), Lang: "go", Size: 1, Hash: hashOf("c")})
	}
	f := &Facts{Files: files}
	m := &fakeModel{}
	var u usage
	sk, err := inferSkeleton(context.Background(), m, "svc", f, nil, "c1", time.Now(), &u)
	if err != nil {
		t.Fatal(err)
	}
	if m.callCount() != 0 || !sk.FallbackAreas || sk.FallbackReason != FallbackTooLarge {
		t.Fatalf("calls=%d %v %q", m.callCount(), sk.FallbackAreas, sk.FallbackReason)
	}
	if len(sk.Stages) != maxStages || sk.Stages[maxStages-1].ID != "other" || sk.Stages[maxStages-1].Title != "其他" {
		t.Fatalf("fallback must clamp to %d stages with other: %d", maxStages, len(sk.Stages))
	}
	if r := rebuildReason(sk, f, time.Now(), 30); r != "" {
		t.Fatalf("fresh fallback must not need a rebuild: %q", r)
	}
}

func TestSkeletonMaxTokensScales(t *testing.T) {
	if skeletonMaxTokens(10) != 1200 || skeletonMaxTokens(100000) != 8192 || skeletonMaxTokens(skeletonMaxDirs) > 8192 {
		t.Fatal(skeletonMaxTokens(10), skeletonMaxTokens(100000), skeletonMaxTokens(skeletonMaxDirs))
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
	if a := sk.Files["internal/order/service.go"]; a.Hash != hashOf("3") || a.CardHash != hashOf("0") {
		t.Fatalf("changed file records its new hash and keeps its old card: %#v", a)
	}
	if a := sk.Files["internal/order/refund/refund.go"]; a.Hash != hashOf("7") || a.CardHash != hashOf("7") {
		t.Fatalf("new file %#v", a)
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
	again, changedAgain := updateSkeleton(sk, f, "c3", now)
	if len(again) != 0 || len(changedAgain) != 0 {
		t.Fatalf("a file keeping an old card is not changed again: %v %v", again, changedAgain)
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
	sk = base()
	sk.FallbackAreas, sk.FallbackReason = true, FallbackBadReply
	if r := rebuildReason(sk, f, now, 30); r != "" {
		t.Fatalf("recent bad-reply fallback: %q", r)
	}
	sk.BuiltAt = now.Add(-25 * time.Hour)
	if r := rebuildReason(sk, f, now, 30); r != "fallback_retry" {
		t.Fatalf("fallback_retry: %q", r)
	}
	sk.FallbackReason = FallbackTooLarge
	if r := rebuildReason(sk, f, now, 30); r != "" {
		t.Fatalf("too-large fallback is not retried early: %q", r)
	}
	sk = base()
	sk.ChangedPaths = []string{"a", "b", "c"}
	if r := rebuildReason(sk, f, now, 30); r != "changes" {
		t.Fatalf("changed paths: %q", r)
	}
}

func TestUpdateSkeleton_RemovesEmptiedStages(t *testing.T) {
	f, _ := orgFacts()
	sk := &Skeleton{PromptVersion: LLMPromptVersion, Stages: []Stage{{ID: "order"}, {ID: "gone"}},
		Files: map[string]FileAssign{
			"internal/order/store.go": {Stage: "order", CardHash: hashOf("2")},
			"internal/gone/x.go":      {Stage: "gone", CardHash: hashOf("9")},
		}}
	affected, _ := updateSkeleton(sk, f, "c2", time.Now())
	if len(sk.Stages) != 1 || sk.Stages[0].ID != "order" || affected["gone"] {
		t.Fatalf("stages %#v affected %v", sk.Stages, affected)
	}
}

func TestUpdateSkeleton_RootFilesAndNilMaps(t *testing.T) {
	f := &Facts{Files: []File{
		{Path: "main.go", Lang: "go", Size: 1, Hash: hashOf("1")},
		{Path: "util.go", Lang: "go", Size: 1, Hash: hashOf("2")},
		{Path: "internal/a/a.go", Lang: "go", Size: 1, Hash: hashOf("3")},
	}}
	sk := &Skeleton{PromptVersion: LLMPromptVersion}
	updateSkeleton(sk, f, "c1", time.Now())
	if len(sk.Files) != 3 || sk.Files["main.go"].Stage != "" {
		t.Fatalf("nil maps: %#v", sk.Files)
	}
	sk = &Skeleton{PromptVersion: LLMPromptVersion, BuiltAt: time.Now(), BaseFiles: 3, TopDirs: []string{"internal"},
		Stages: []Stage{{ID: "boot"}, {ID: "core"}},
		Files: map[string]FileAssign{
			"main.go":         {Stage: "boot", CardHash: hashOf("1")},
			"internal/a/a.go": {Stage: "core", CardHash: hashOf("3")},
		}}
	updateSkeleton(sk, f, "c2", time.Now())
	if sk.Files["util.go"].Stage != "boot" {
		t.Fatalf("a new root file takes the root files' stage: %#v", sk.Files["util.go"])
	}
	sk.ChangedPaths, sk.ChangedSinceRebuild = nil, 0
	if r := rebuildReason(sk, f, time.Now(), 30); r != "" {
		t.Fatalf("root files are not a new top-level directory: %q", r)
	}
}

func TestUpdateSkeleton_CountsDistinctChangedPaths(t *testing.T) {
	f, _ := orgFacts()
	sk := &Skeleton{PromptVersion: LLMPromptVersion, Stages: []Stage{{ID: "s"}}, Files: map[string]FileAssign{}}
	for _, file := range eligibleFiles(f) {
		sk.Files[file.Path] = FileAssign{Stage: "s", CardHash: file.Hash}
	}
	for _, h := range []string{"a", "b"} {
		f.Files[0].Hash = hashOf(h)
		updateSkeleton(sk, f, "c", time.Now())
	}
	if sk.ChangedSinceRebuild != 1 || strings.Join(sk.ChangedPaths, ",") != "cmd/server/main.go" {
		t.Fatalf("%d %v", sk.ChangedSinceRebuild, sk.ChangedPaths)
	}
}
