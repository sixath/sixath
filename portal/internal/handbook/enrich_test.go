package handbook

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sixath/framework/model"
)

type enrichRepo struct {
	root  string
	facts *Facts
}

func writeRepoFile(t *testing.T, root, rel, content string) File {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte(content))
	lang := "go"
	if strings.HasSuffix(rel, ".md") {
		lang = "markdown"
	}
	return File{Path: rel, Lang: lang, Size: int64(len(content)), Lines: strings.Count(content, "\n"), Hash: hex.EncodeToString(sum[:]), Test: strings.HasSuffix(rel, "_test.go")}
}

func newEnrichRepo(t *testing.T) *enrichRepo {
	root := t.TempDir()
	f := &Facts{Symbols: map[string][]Symbol{}}
	for rel, content := range map[string]string{
		"cmd/server/main.go":          "package main\nfunc main() {}\n",
		"internal/order/store.go":     "package order\nfunc Get() {}\n",
		"internal/order/service.go":   "package order\nfunc Place() {}\n",
		"internal/pay/client.go":      "package pay\nfunc Charge() {}\n",
		"internal/pay/client_test.go": "package pay\n",
		"README.md":                   "# svc\n",
	} {
		f.Files = append(f.Files, writeRepoFile(t, root, rel, content))
	}
	f.Symbols["internal/order/store.go"] = []Symbol{{Kind: "func", Name: "Get", Line: 2, EndLine: 2}}
	f.Registers = []RegisterHit{{Kind: RegTable, Name: "orders", Access: AccessWrite, Path: "internal/order/store.go", Line: 2}}
	f.Packages = []GoPackage{{Dir: "cmd/server", Name: "main", Main: true}}
	return &enrichRepo{root: root, facts: f}
}

func happyModel() *fakeModel {
	return (&fakeModel{}).
		on("文件卡片", func(user string) string {
			return `{"purpose":"职责","role":"service","functions":[{"name":"Get","summary":"查询"}]}`
		}).
		on("执行阶段", skeletonReply(map[string]string{"cmd/server": "boot", "internal/order": "order", "internal/pay": "pay"})).
		// before "阶段说明": the overview prompt quotes stage summaries
		on("仓库总览", func(string) string { return `{"overview":"总览文本"}` }).
		on("阶段说明", func(string) string { return `{"summary":"阶段说明文本"}` }).
		on("用途", func(string) string { return `{"notes":{"table:orders":"订单主表"}}` })
}

func (r *enrichRepo) input(m model.Model, cache LLMCache, opts EnrichOptions) EnrichInput {
	if opts.RetryBackoff == 0 {
		opts.RetryBackoff = time.Millisecond
	}
	return EnrichInput{RelPath: "svc", Root: r.root, Commit: "c1", ModelName: "p/m", Facts: r.facts, Cache: cache, Model: m, Opts: opts, Now: time.Unix(5000, 0).UTC()}
}

func countCalls(m *fakeModel, marker string) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	n := 0
	for _, c := range m.calls {
		if strings.Contains(c, marker) {
			n++
		}
	}
	return n
}

// cardMarker occurs in every card user prompt and in no other prompt.
const cardMarker = "为下面的文件生成文件卡片"

func TestEnrich_FullRunCompletes(t *testing.T) {
	r := newEnrichRepo(t)
	cache := LLMCache{Dir: t.TempDir()}
	m := happyModel()
	res, err := Enrich(context.Background(), r.input(m, cache, EnrichOptions{}))
	if err != nil {
		t.Fatal(err)
	}
	if res.State != LLMStateComplete || res.CardsTotal != 4 || res.CardsDone != 4 || res.CardsNew != 4 || !res.SkeletonRebuilt ||
		res.RebuildReason != "none" || res.Stages != 3 || !res.Changed || res.TokensIn == 0 {
		t.Fatalf("result %#v", res)
	}
	sk, _ := cache.Skeleton()
	if sk == nil || sk.Overview != "总览文本" || sk.RegisterNotes["table:orders"] != "订单主表" || sk.Stages[0].Summary != "阶段说明文本" {
		t.Fatalf("skeleton %#v", sk)
	}
	for _, f := range r.facts.Files {
		if card, _ := cache.Card(f.Hash); card == nil && CardEligible(f) {
			t.Fatalf("card of %s not cached", f.Path)
		}
	}

	calls := m.callCount()
	res, err = Enrich(context.Background(), r.input(m, cache, EnrichOptions{}))
	if err != nil || res.CardsNew != 0 || res.SkeletonRebuilt || res.Changed || res.State != LLMStateComplete {
		t.Fatalf("a rerun with nothing changed must be a no-op: %#v %v", res, err)
	}
	if m.callCount() != calls {
		t.Fatalf("rerun made %d model calls", m.callCount()-calls)
	}
}

func TestEnrich_BudgetLeavesPartial(t *testing.T) {
	r := newEnrichRepo(t)
	cache := LLMCache{Dir: t.TempDir()}
	m := happyModel()
	res, err := Enrich(context.Background(), r.input(m, cache, EnrichOptions{MaxCardsPerRun: 2, Concurrency: 4}))
	if err != nil || res.State != LLMStatePartial || res.CardsDone != 2 || res.CardsNew != 2 || !res.Changed {
		t.Fatalf("%#v %v", res, err)
	}
	if n := countCalls(m, cardMarker); n != 2 {
		t.Fatalf("concurrent workers must not exceed the budget, got %d card calls", n)
	}
	if sk, _ := cache.Skeleton(); sk != nil {
		t.Fatal("no synthesis before all cards were attempted")
	}
	res, err = Enrich(context.Background(), r.input(happyModel(), cache, EnrichOptions{MaxCardsPerRun: 2}))
	if err != nil || res.State != LLMStateComplete || res.CardsDone != 4 {
		t.Fatalf("second run must finish: %#v %v", res, err)
	}
}

func TestEnrich_SkippedFilesDoNotUseBudget(t *testing.T) {
	r := newEnrichRepo(t)
	if err := os.WriteFile(filepath.Join(r.root, "cmd", "server", "main.go"), []byte("package main // moved on\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := happyModel()
	res, err := Enrich(context.Background(), r.input(m, LLMCache{Dir: t.TempDir()}, EnrichOptions{MaxCardsPerRun: 3}))
	if err != nil || res.State != LLMStateComplete || res.CardsDone != 3 || countCalls(m, cardMarker) != 3 {
		t.Fatalf("a file that cannot be read spends no budget: %#v %v", res, err)
	}
}

func TestEnrich_DedupesIdenticalContent(t *testing.T) {
	r := newEnrichRepo(t)
	r.facts.Files = append(r.facts.Files, writeRepoFile(t, r.root, "internal/pay/client_copy.go", "package pay\nfunc Charge() {}\n"))
	cache := LLMCache{Dir: t.TempDir()}
	m := happyModel()
	res, err := Enrich(context.Background(), r.input(m, cache, EnrichOptions{MaxCardsPerRun: 4}))
	if err != nil || res.State != LLMStateComplete || res.CardsTotal != 5 || res.CardsDone != 5 || res.CardsNew != 5 {
		t.Fatalf("the budget counts model calls, the result counts files: %#v %v", res, err)
	}
	if n := countCalls(m, cardMarker); n != 4 {
		t.Fatalf("one card call per distinct content, got %d", n)
	}
	sk, _ := cache.Skeleton()
	if sk == nil || sk.Files["internal/pay/client_copy.go"].Stage != "pay" {
		t.Fatalf("skeleton %#v", sk)
	}
}

func TestEnrich_IncrementalAfterChange(t *testing.T) {
	r := newEnrichRepo(t)
	cache := LLMCache{Dir: t.TempDir()}
	if _, err := Enrich(context.Background(), r.input(happyModel(), cache, EnrichOptions{})); err != nil {
		t.Fatal(err)
	}
	sk0, _ := cache.Skeleton()
	sk0.BaseFiles = 100 // one changed file of four would otherwise exceed the 20% rebuild threshold
	if err := cache.PutSkeleton(sk0); err != nil {
		t.Fatal(err)
	}
	for i, f := range r.facts.Files {
		if f.Path == "internal/order/service.go" {
			r.facts.Files[i] = writeRepoFile(t, r.root, f.Path, "package order\nfunc Place() { /* v2 */ }\n")
		}
	}
	m := happyModel()
	in := r.input(m, cache, EnrichOptions{})
	in.Commit = "c2"
	res, err := Enrich(context.Background(), in)
	if err != nil || res.CardsNew != 1 || res.SkeletonRebuilt || !res.Changed || res.State != LLMStateComplete {
		t.Fatalf("%#v %v", res, err)
	}
	if n := countCalls(m, "阶段："); n != 1 {
		t.Fatalf("only the affected stage is rewritten, got %d stage calls", n)
	}
	sk, _ := cache.Skeleton()
	if sk.Commit != "c2" || sk.ChangedSinceRebuild != 1 {
		t.Fatalf("skeleton %#v", sk)
	}
}

func TestEnrich_RebuildReasons(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*Skeleton)
		want   string
	}{
		{"prompt", func(sk *Skeleton) { sk.PromptVersion = "old" }, "prompt"},
		{"age", func(sk *Skeleton) { sk.BuiltAt = sk.BuiltAt.Add(-31 * 24 * time.Hour) }, "age"},
		{"fallback_retry", func(sk *Skeleton) {
			sk.FallbackAreas, sk.FallbackReason = true, FallbackBadReply
			sk.BuiltAt = sk.BuiltAt.Add(-25 * time.Hour)
		}, "fallback_retry"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newEnrichRepo(t)
			cache := LLMCache{Dir: t.TempDir()}
			if _, err := Enrich(context.Background(), r.input(happyModel(), cache, EnrichOptions{})); err != nil {
				t.Fatal(err)
			}
			sk0, _ := cache.Skeleton()
			tc.mutate(sk0)
			if err := cache.PutSkeleton(sk0); err != nil {
				t.Fatal(err)
			}
			res, err := Enrich(context.Background(), r.input(happyModel(), cache, EnrichOptions{}))
			if err != nil || !res.SkeletonRebuilt || res.RebuildReason != tc.want || res.State != LLMStateComplete || !res.Changed {
				t.Fatalf("%#v %v", res, err)
			}
			sk, _ := cache.Skeleton()
			if sk.FallbackAreas || sk.PromptVersion != LLMPromptVersion || len(sk.ChangedPaths) != 0 || sk.RegisterNotes["table:orders"] != "订单主表" {
				t.Fatalf("skeleton %#v", sk)
			}
		})
	}
}

func TestEnrich_SkipsFilesChangedOnDisk(t *testing.T) {
	r := newEnrichRepo(t)
	if err := os.WriteFile(filepath.Join(r.root, "internal", "pay", "client.go"), []byte("package pay // moved on\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := Enrich(context.Background(), r.input(happyModel(), LLMCache{Dir: t.TempDir()}, EnrichOptions{}))
	if err != nil || res.CardsDone != 3 || res.State != LLMStateComplete {
		t.Fatalf("a file whose content no longer matches the facts is skipped: %#v %v", res, err)
	}
}

func TestEnrich_ModelFailuresAbort(t *testing.T) {
	r := newEnrichRepo(t)
	for _, rel := range []string{"internal/pay/refund.go", "internal/pay/notify.go"} {
		r.facts.Files = append(r.facts.Files, writeRepoFile(t, r.root, rel, "package pay\n// "+rel+"\n"))
	}
	m := &fakeModel{failErr: errors.New("503 upstream")}
	res, err := Enrich(context.Background(), r.input(m, LLMCache{Dir: t.TempDir()}, EnrichOptions{Concurrency: 1}))
	if !errors.Is(err, ErrModelUnavailable) || res.State != LLMStateFailed {
		t.Fatalf("want ErrModelUnavailable, got %v %#v", err, res)
	}
	if n := m.callCount(); n != maxConsecutiveCallErrors {
		t.Fatalf("aborted after %d calls", n)
	}
}

func TestEnrich_SingleTransientErrorRetriesNextRun(t *testing.T) {
	r := newEnrichRepo(t)
	cache := LLMCache{Dir: t.TempDir()}
	if _, err := Enrich(context.Background(), r.input(happyModel(), cache, EnrichOptions{})); err != nil {
		t.Fatal(err)
	}
	sk0, _ := cache.Skeleton()
	sk0.BaseFiles = 100
	if err := cache.PutSkeleton(sk0); err != nil {
		t.Fatal(err)
	}
	const p = "internal/order/service.go"
	for i, f := range r.facts.Files {
		if f.Path == p {
			r.facts.Files[i] = writeRepoFile(t, r.root, p, "package order\n// v2\n")
		}
	}
	m := &fakeModel{failErr: errors.New("503 upstream")}
	in := r.input(m, cache, EnrichOptions{})
	in.Commit = "c2"
	res, err := Enrich(context.Background(), in)
	if err != nil || res.State != LLMStatePartial || res.CardTransportErrors != 1 || m.callCount() != 1 {
		t.Fatalf("one transient error leaves the run partial: %#v %v", res, err)
	}
	if sk, _ := cache.Skeleton(); sk.Commit != "c1" {
		t.Fatalf("no synthesis while a card can still be retried: %#v", sk)
	}
	in.Model = happyModel()
	res, err = Enrich(context.Background(), in)
	if err != nil || res.State != LLMStateComplete || res.CardsNew != 1 || res.CardsDone != 4 {
		t.Fatalf("the next run completes: %#v %v", res, err)
	}
	if sk, _ := cache.Skeleton(); sk.Commit != "c2" || sk.Files[p].CardHash != sk.Files[p].Hash {
		t.Fatalf("skeleton %#v", sk)
	}
}

func TestEnrich_CardCacheWriteFailureStopsRun(t *testing.T) {
	r := newEnrichRepo(t)
	cache := LLMCache{Dir: t.TempDir()}
	m := happyModel()
	var once sync.Once
	m.rules[0] = fakeRule{"文件卡片", func(string) string {
		// a file where the cards directory belongs makes every card write fail
		once.Do(func() { _ = os.WriteFile(filepath.Join(cache.Dir, "cards"), nil, 0o644) })
		return `{"purpose":"职责"}`
	}}
	res, err := Enrich(context.Background(), r.input(m, cache, EnrichOptions{Concurrency: 1}))
	if err == nil || errors.Is(err, ErrModelUnavailable) || res.State != LLMStateFailed || res.CardsNew != 0 {
		t.Fatalf("a cache write failure is an IO error, not a model error: %#v %v", res, err)
	}
	if n := countCalls(m, cardMarker); n != 1 {
		t.Fatalf("the run stops at the first failed write, got %d card calls", n)
	}
	if sk, _ := cache.Skeleton(); sk != nil {
		t.Fatal("no synthesis after a failed run")
	}
}

func TestEnrich_BadRepliesDoNotBlockSynthesis(t *testing.T) {
	r := newEnrichRepo(t)
	m := happyModel()
	m.rules[0] = fakeRule{"文件卡片", func(user string) string {
		if strings.Contains(user, "internal/pay/client.go") {
			return "无法回答"
		}
		return `{"purpose":"职责"}`
	}}
	cache := LLMCache{Dir: t.TempDir()}
	res, err := Enrich(context.Background(), r.input(m, cache, EnrichOptions{}))
	if err != nil || res.State != LLMStateComplete || res.CardErrors != 1 || res.CardsDone != 3 {
		t.Fatalf("%#v %v", res, err)
	}

	calls := m.callCount()
	res, err = Enrich(context.Background(), r.input(m, cache, EnrichOptions{}))
	if err != nil || res.State != LLMStateComplete || res.CardErrors != 1 || res.Changed || m.callCount() != calls {
		t.Fatalf("a failed card is not retried until its content changes: %#v %v (%d calls)", res, err, m.callCount()-calls)
	}

	res, err = Enrich(context.Background(), r.input(happyModel(), cache, EnrichOptions{Full: true}))
	if err != nil || res.State != LLMStateComplete || res.CardErrors != 0 || res.CardsDone != 4 || res.CardsNew != 1 {
		t.Fatalf("a full run retries failed cards: %#v %v", res, err)
	}
	if failed, _ := cache.FailedCards(); len(failed) != 0 {
		t.Fatalf("failed set %v", failed)
	}
}

func TestEnrich_FailedSetFollowsFacts(t *testing.T) {
	r := newEnrichRepo(t)
	cache := LLMCache{Dir: t.TempDir()}
	gone := hashOf("gone")
	if err := cache.PutFailedCards(map[string]bool{gone: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := Enrich(context.Background(), r.input(happyModel(), cache, EnrichOptions{})); err != nil {
		t.Fatal(err)
	}
	if failed, _ := cache.FailedCards(); failed[gone] {
		t.Fatalf("hashes no longer in the facts are dropped: %v", failed)
	}
}

func TestEnrich_DeadlineLeavesPartial(t *testing.T) {
	r := newEnrichRepo(t)
	ctx, cancel := context.WithCancel(context.Background())
	m := happyModel()
	m.rules[0] = fakeRule{"文件卡片", func(string) string { cancel(); return `{"purpose":"职责"}` }}
	res, err := Enrich(ctx, r.input(m, LLMCache{Dir: t.TempDir()}, EnrichOptions{Concurrency: 1}))
	if err != nil || res.State != LLMStatePartial {
		t.Fatalf("a cancelled run reports partial progress without error: %#v %v", res, err)
	}
	if n := countCalls(m, cardMarker); n != 1 {
		t.Fatalf("no model call starts after cancellation, got %d card calls", n)
	}
}

// cardOf matches the card prompt of one file.
func cardOf(path string) func(string) bool {
	return func(p string) bool { return strings.Contains(p, cardMarker) && strings.Contains(p, "文件："+path) }
}

func TestEnrich_CardTransportErrorsRetryNextRun(t *testing.T) {
	r := newEnrichRepo(t)
	cache := LLMCache{Dir: t.TempDir()}
	m := &flakyModel{fakeModel: happyModel(), match: cardOf("internal/pay/client.go"), fails: func(n int) bool { return n == 1 }}
	res, err := Enrich(context.Background(), r.input(m, cache, EnrichOptions{Concurrency: 1}))
	if err != nil || res.State != LLMStatePartial || res.CardTransportErrors != 1 || res.CardErrors != 0 || res.CardsDone != 3 {
		t.Fatalf("%#v %v", res, err)
	}
	if failed, _ := cache.FailedCards(); len(failed) != 0 {
		t.Fatalf("transport errors are not remembered as failed: %v", failed)
	}
	res, err = Enrich(context.Background(), r.input(m, cache, EnrichOptions{Concurrency: 1}))
	if err != nil || res.State != LLMStateComplete || res.CardsDone != 4 || res.CardsNew != 1 || m.n != 2 {
		t.Fatalf("retried next run: %#v %v (calls %d)", res, err, m.n)
	}
}

func TestEnrich_StaleCardKeptUntilNewCardExists(t *testing.T) {
	r := newEnrichRepo(t)
	cache := LLMCache{Dir: t.TempDir()}
	if _, err := Enrich(context.Background(), r.input(happyModel(), cache, EnrichOptions{})); err != nil {
		t.Fatal(err)
	}
	sk0, _ := cache.Skeleton()
	sk0.BaseFiles = 100
	if err := cache.PutSkeleton(sk0); err != nil {
		t.Fatal(err)
	}
	const p = "internal/order/service.go"
	oldHash := sk0.Files[p].CardHash
	var changed File
	for i, f := range r.facts.Files {
		if f.Path == p {
			changed = writeRepoFile(t, r.root, p, "package order\n// v2\n")
			r.facts.Files[i] = changed
		}
	}
	m := happyModel()
	m.rules[0] = fakeRule{"文件卡片", func(string) string { return "无法回答" }}
	in := r.input(m, cache, EnrichOptions{})
	in.Commit = "c2"
	res, err := Enrich(context.Background(), in)
	if err != nil || res.State != LLMStateComplete || res.CardErrors != 1 {
		t.Fatalf("%#v %v", res, err)
	}
	sk, _ := cache.Skeleton()
	if a := sk.Files[p]; a.CardHash != oldHash || a.Hash != changed.Hash {
		t.Fatalf("the skeleton keeps pointing at the old card: %#v", a)
	}
	if card, _ := cache.Card(oldHash); card == nil {
		t.Fatal("prune must keep the card the skeleton points at")
	}
	if layer, err := LoadLLMLayer(cache, r.facts); err != nil || layer.Stale[p] == nil {
		t.Fatalf("the old card renders as stale: %v", err)
	}

	// a card of the new content shows up (e.g. written by a later run of the same content)
	if err := cache.PutCard(changed.Hash, &Card{Purpose: "新职责", Hash: changed.Hash, PromptVersion: LLMPromptVersion}); err != nil {
		t.Fatal(err)
	}
	stageCalls := countCalls(m, "阶段：")
	in.Commit = "c3"
	res, err = Enrich(context.Background(), in)
	if err != nil || res.CardsNew != 0 || res.CardErrors != 0 || !res.Changed {
		t.Fatalf("%#v %v", res, err)
	}
	sk, _ = cache.Skeleton()
	if a := sk.Files[p]; a.CardHash != changed.Hash {
		t.Fatalf("a new card replaces the old one: %#v", a)
	}
	if n := countCalls(m, "阶段：") - stageCalls; n != 1 {
		t.Fatalf("the stage that gained a card is rewritten once, got %d", n)
	}
}

func TestEnrich_OverviewFollowsStageSet(t *testing.T) {
	r := newEnrichRepo(t)
	cache := LLMCache{Dir: t.TempDir()}
	if _, err := Enrich(context.Background(), r.input(happyModel(), cache, EnrichOptions{})); err != nil {
		t.Fatal(err)
	}
	sk0, _ := cache.Skeleton()
	sk0.BaseFiles = 100
	if err := cache.PutSkeleton(sk0); err != nil {
		t.Fatal(err)
	}
	var kept []File
	for _, f := range r.facts.Files {
		if f.Path != "internal/pay/client.go" {
			kept = append(kept, f)
		}
	}
	r.facts.Files = kept
	m := happyModel()
	res, err := Enrich(context.Background(), r.input(m, cache, EnrichOptions{}))
	if err != nil || res.Stages != 2 || res.SkeletonRebuilt {
		t.Fatalf("%#v %v", res, err)
	}
	if countCalls(m, "阶段：") != 0 || countCalls(m, "各阶段概要") != 1 {
		t.Fatalf("removing a stage rewrites only the overview: %q", m.calls)
	}
}

func TestEnrich_PruneFailureDoesNotFailRun(t *testing.T) {
	r := newEnrichRepo(t)
	cache := LLMCache{Dir: t.TempDir()}
	orphanDir := filepath.Join(cache.Dir, "cards", "00")
	orphan := filepath.Join(orphanDir, strings.Repeat("0", 64)+"-"+LLMPromptVersion+".json")
	if err := os.MkdirAll(orphanDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(orphan, []byte("{}"), 0o444); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(orphanDir, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(orphanDir, 0o755); _ = os.Chmod(orphan, 0o644) })
	// an open handle blocks removal on Windows, the read-only directory elsewhere
	fh, err := os.Open(orphan)
	if err != nil {
		t.Fatal(err)
	}
	res, err := Enrich(context.Background(), r.input(happyModel(), cache, EnrichOptions{}))
	_ = fh.Close()
	if _, statErr := os.Stat(orphan); statErr != nil {
		t.Skip("this platform let the orphan card be removed")
	}
	if err != nil || res.State != LLMStateComplete || res.PruneError == nil || !res.Changed || res.Stages != 3 {
		t.Fatalf("pruning is best effort: %#v %v", res, err)
	}
}

func TestReadMatching_RefusesNonRegularFiles(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "dir"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, ok := readMatching(root, File{Path: "dir", Hash: hashOf("x")}); ok {
		t.Fatal("a directory is not a source file")
	}
}

// flakyModel fails matching calls while fails reports true.
type flakyModel struct {
	*fakeModel
	match func(prompt string) bool // prompt is the system and user prompt joined
	mu    sync.Mutex
	n     int
	fails func(n int) bool // n counts matching calls from 1
}

func keyMatch(key string) func(string) bool {
	return func(p string) bool { return strings.Contains(p, key) }
}

func (m *flakyModel) Chat(ctx context.Context, msgs []model.Message, opts ...model.Option) (*model.Generation, error) {
	var b strings.Builder
	for _, msg := range msgs {
		b.WriteString(msg.Content + "\n")
	}
	if m.match(b.String()) {
		m.mu.Lock()
		m.n++
		fail := m.fails(m.n)
		m.mu.Unlock()
		if fail {
			return nil, errors.New("connection reset")
		}
	}
	return m.fakeModel.Chat(ctx, msgs, opts...)
}

func TestEnrich_SynthesisRetriesTransientErrors(t *testing.T) {
	r := newEnrichRepo(t)
	cache := LLMCache{Dir: t.TempDir()}
	m := &flakyModel{fakeModel: happyModel(), match: keyMatch("仓库总览"), fails: func(n int) bool { return n <= 2 }}
	res, err := Enrich(context.Background(), r.input(m, cache, EnrichOptions{}))
	if err != nil || res.State != LLMStateComplete {
		t.Fatalf("%#v %v", res, err)
	}
	if sk, _ := cache.Skeleton(); sk == nil || sk.Overview != "总览文本" || m.n != 3 {
		t.Fatalf("overview after %d attempts: %#v", m.n, sk)
	}
}

func TestEnrich_SynthesisFailureKeepsStoredSkeleton(t *testing.T) {
	r := newEnrichRepo(t)
	cache := LLMCache{Dir: t.TempDir()}
	if _, err := Enrich(context.Background(), r.input(happyModel(), cache, EnrichOptions{})); err != nil {
		t.Fatal(err)
	}
	for i, f := range r.facts.Files {
		if f.Path == "internal/order/service.go" {
			r.facts.Files[i] = writeRepoFile(t, r.root, f.Path, "package order\nfunc Place() { /* v2 */ }\n")
		}
	}
	m := &flakyModel{fakeModel: happyModel(), match: keyMatch("阶段说明"), fails: func(int) bool { return true }}
	in := r.input(m, cache, EnrichOptions{})
	in.Commit = "c2"
	res, err := Enrich(context.Background(), in)
	if !errors.Is(err, ErrModelUnavailable) || res.State != LLMStateFailed {
		t.Fatalf("want ErrModelUnavailable, got %v %#v", err, res)
	}
	if m.n != 1+synthesisRetries {
		t.Fatalf("stage call attempted %d times", m.n)
	}
	if sk, _ := cache.Skeleton(); sk == nil || sk.Commit != "c1" {
		t.Fatalf("a failed synthesis must not store a partial skeleton: %#v", sk)
	}
}

func TestEnrich_CancelDuringSynthesisWritesNothing(t *testing.T) {
	r := newEnrichRepo(t)
	cache := LLMCache{Dir: t.TempDir()}
	ctx, cancel := context.WithCancel(context.Background())
	m := happyModel()
	m.rules[2] = fakeRule{"仓库总览", func(string) string { cancel(); return `{"overview":"总览文本"}` }}
	res, err := Enrich(ctx, r.input(m, cache, EnrichOptions{}))
	if err != nil || res.State != LLMStatePartial || res.CardsDone != 4 || res.SkeletonRebuilt {
		t.Fatalf("%#v %v", res, err)
	}
	if sk, _ := cache.Skeleton(); sk != nil {
		t.Fatalf("a cancelled synthesis stores nothing: %#v", sk)
	}
}

func TestEnrich_DocsOnlyRepoSkipsOverview(t *testing.T) {
	root := t.TempDir()
	f := &Facts{Files: []File{writeRepoFile(t, root, "README.md", "# docs\n")}}
	cache := LLMCache{Dir: t.TempDir()}
	m := happyModel()
	in := EnrichInput{RelPath: "docs", Root: root, Commit: "c1", Facts: f, Cache: cache, Model: m, Now: time.Unix(5000, 0).UTC()}
	res, err := Enrich(context.Background(), in)
	if err != nil || res.State != LLMStateComplete || res.Stages != 0 || res.CardsTotal != 0 || !res.Changed {
		t.Fatalf("%#v %v", res, err)
	}
	if n := m.callCount(); n != 0 {
		t.Fatalf("a repository without source files needs no model call, got %d", n)
	}
	if sk, _ := cache.Skeleton(); sk == nil || len(sk.Stages) != 0 || sk.Overview != "" {
		t.Fatalf("skeleton %#v", sk)
	}
	res, err = Enrich(context.Background(), in)
	if err != nil || res.State != LLMStateComplete || res.Changed || m.callCount() != 0 {
		t.Fatalf("rerun %#v %v", res, err)
	}
}
