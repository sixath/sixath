package data

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"backend/internal/biz"
	"backend/internal/handbook"

	"github.com/sixath/framework/model"
)

func TestHandbookEnrichLease(t *testing.T) {
	ctx := context.Background()
	r := newRepoRegistryRepoForTest(t)
	now := time.Now().UTC()
	a, err := r.UpsertScannedRepository(ctx, &biz.Repository{CodeRoot: "/c", RelPath: "cg/a", Name: "a", HeadCommit: "111", LastScannedAt: &now})
	if err != nil {
		t.Fatal(err)
	}
	reload := func() *biz.Repository {
		m, err := r.GetRepositoriesByIDs(ctx, []string{a.ID})
		if err != nil {
			t.Fatal(err)
		}
		return m[a.ID]
	}

	first, ok, err := r.ClaimHandbookEnrich(ctx, a.ID, now, now.Add(time.Minute))
	if err != nil || !ok || first == "" {
		t.Fatalf("first claim: token=%q ok=%v err=%v", first, ok, err)
	}
	got := reload()
	if got.HandbookLLMLeaseUntil == nil || got.HandbookLLMLeaseUntil.Sub(now.Add(time.Minute)).Abs() > time.Millisecond {
		t.Fatalf("after claim: %#v", got)
	}
	if got.HandbookStatus != biz.HandbookStatusNone || got.HandbookLeaseUntil != nil {
		t.Fatalf("the LLM lease must not touch the build lease: %#v", got)
	}
	build, ok, err := r.ClaimHandbookBuild(ctx, a.ID, now, now.Add(time.Minute))
	if err != nil || !ok {
		t.Fatalf("a build must be claimable while the LLM lease is held: ok=%v err=%v", ok, err)
	}
	if err := r.ReleaseHandbookBuild(ctx, a.ID, build, biz.HandbookStatusNone); err != nil {
		t.Fatal(err)
	}
	if g := reload(); g.HandbookLLMLeaseUntil == nil {
		t.Fatalf("releasing the build lease must keep the LLM lease: %#v", g)
	}

	if _, ok, _ := r.ClaimHandbookEnrich(ctx, a.ID, now, now.Add(time.Minute)); ok {
		t.Fatal("second claim must fail while the lease is live")
	}
	second, ok, _ := r.ClaimHandbookEnrich(ctx, a.ID, now.Add(2*time.Minute), now.Add(3*time.Minute))
	if !ok || second == "" || second == first {
		t.Fatalf("an expired lease must be claimable with a new token: %q ok=%v", second, ok)
	}
	if err := r.FinishHandbookEnrich(ctx, a.ID, first, map[string]any{"state": "complete"}); !errors.Is(err, biz.ErrHandbookLeaseLost) {
		t.Fatalf("finish with a superseded token: err = %v, want ErrHandbookLeaseLost", err)
	}
	if err := r.FinishHandbookEnrich(ctx, a.ID, second, map[string]any{"state": "complete", "rev": "1"}); err != nil {
		t.Fatal(err)
	}
	g := reload()
	if g.HandbookLLM["state"] != "complete" || g.HandbookLLM["rev"] != "1" || g.HandbookLLMLeaseUntil != nil {
		t.Fatalf("after finish: %#v", g)
	}
	if g.HandbookStatus != biz.HandbookStatusNone {
		t.Fatalf("finishing the LLM run must not touch handbook_status: %#v", g)
	}

	third, ok, _ := r.ClaimHandbookEnrich(ctx, a.ID, now.Add(5*time.Minute), now.Add(6*time.Minute))
	if !ok {
		t.Fatal("a finished run must release the lease")
	}
	if err := r.ReleaseHandbookEnrich(ctx, a.ID, "other-token"); !errors.Is(err, biz.ErrHandbookLeaseLost) {
		t.Fatalf("release with a foreign token: err = %v", err)
	}
	if err := r.ReleaseHandbookEnrich(ctx, a.ID, ""); !errors.Is(err, biz.ErrHandbookLeaseLost) {
		t.Fatalf("release with an empty token: err = %v", err)
	}
	if err := r.ReleaseHandbookEnrich(ctx, a.ID, third); err != nil {
		t.Fatal(err)
	}
	g = reload()
	if g.HandbookLLMLeaseUntil != nil || g.HandbookLLM["state"] != "complete" || g.HandbookLLM["rev"] != "1" {
		t.Fatalf("release must clear the lease and keep the state: %#v", g)
	}
	if _, ok, err := r.ClaimHandbookEnrich(ctx, "nope", now, now.Add(time.Minute)); err != nil || ok {
		t.Fatalf("unknown repo: ok=%v err=%v", ok, err)
	}
}

func TestPatchRepo_HandbookModel(t *testing.T) {
	ctx := context.Background()
	codeRoot := t.TempDir()
	mkRepoDir(t, codeRoot+"/cloudgame/svc-a")
	uc, _ := newUsecaseForTest(t, codeRoot)
	if _, err := uc.Scan(ctx); err != nil {
		t.Fatal(err)
	}
	repos, err := uc.ListRepos(ctx, biz.RepoFilter{})
	if err != nil || len(repos) != 1 {
		t.Fatalf("repos = %v err=%v", repos, err)
	}
	id := repos[0].ID
	set := func(s string) (*biz.Repository, error) {
		return uc.PatchRepo(ctx, id, biz.RepoMetaPatch{HandbookModel: &s})
	}

	got, err := set("  qwen/qwen-max ")
	if err != nil || got.HandbookModel != "qwen/qwen-max" {
		t.Fatalf("got=%#v err=%v", got, err)
	}
	if got, err := set(biz.HandbookModelOff); err != nil || got.HandbookModel != "off" {
		t.Fatalf("off: got=%#v err=%v", got, err)
	}
	for _, bad := range []string{"a b", "a\tb", "a\x01b", strings.Repeat("m", 256)} {
		if _, err := set(bad); !errors.Is(err, biz.ErrInvalidRepo) {
			t.Fatalf("%q: err = %v, want ErrInvalidRepo", bad, err)
		}
	}
	if got, err := set(strings.Repeat("m", 255)); err != nil || len(got.HandbookModel) != 255 {
		t.Fatalf("255 chars: err=%v", err)
	}
	if got, err := set(""); err != nil || got.HandbookModel != "" {
		t.Fatalf("clear: got=%#v err=%v", got, err)
	}
	desc := "kept"
	if v, err := uc.PatchRepo(ctx, id, biz.RepoMetaPatch{Description: &desc}); err != nil || v.HandbookModel != "" {
		t.Fatalf("a patch without handbook_model must not change it: %#v err=%v", v, err)
	}
}

// llmFake answers handbook prompts by their system prompt. The skeleton reply is unusable,
// so the skeleton falls back to one stage per area.
type llmFake struct {
	mu      sync.Mutex
	calls   int
	cardErr error
	onCall  func()
}

func (m *llmFake) Chat(ctx context.Context, msgs []model.Message, _ ...model.Option) (*model.Generation, error) {
	m.mu.Lock()
	m.calls++
	onCall, cardErr := m.onCall, m.cardErr
	m.mu.Unlock()
	if onCall != nil {
		onCall()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	sys := ""
	for _, msg := range msgs {
		if msg.Role == "system" {
			sys = msg.Content
		}
	}
	reply := ""
	switch {
	case strings.Contains(sys, "文件卡片"):
		if cardErr != nil {
			return nil, cardErr
		}
		reply = `{"purpose":"职责"}`
	case strings.Contains(sys, "阶段说明"):
		reply = `{"summary":"说明"}`
	case strings.Contains(sys, "仓库总览"):
		reply = `{"overview":"总览"}`
	case strings.Contains(sys, "执行阶段"):
		reply = "不是 JSON"
	case strings.Contains(sys, "用途"):
		reply = `{"notes":{}}`
	default:
		return nil, errors.New("llm fake: unexpected prompt")
	}
	return &model.Generation{Text: reply, TokenUsage: &model.TokenUsage{InputTokens: 10, OutputTokens: 5}, FinishReason: "stop"}, nil
}

func (m *llmFake) Generate(ctx context.Context, prompt string, opts ...model.Option) (*model.Generation, error) {
	return m.Chat(ctx, []model.Message{{Role: "user", Content: prompt}}, opts...)
}

func (m *llmFake) Embed(context.Context, []string, ...model.Option) ([]model.Embedding, error) {
	return nil, errors.New("llm fake: no embeddings")
}

func (m *llmFake) set(f func(m *llmFake)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	f(m)
}

// fakeResolver resolves every name to m and records the names asked for.
func fakeResolver(m model.Model, names *[]string) biz.HandbookModelResolver {
	var mu sync.Mutex
	return func(_ context.Context, name string) (model.Model, error) {
		mu.Lock()
		defer mu.Unlock()
		*names = append(*names, name)
		return m, nil
	}
}

func mustCount(t *testing.T, what string, n int, err error, want int) {
	t.Helper()
	if err != nil || n != want {
		t.Fatalf("%s: n=%d err=%v, want n=%d", what, n, err, want)
	}
}

func llmString(r *biz.Repository, key string) string {
	s, _ := r.HandbookLLM[key].(string)
	return s
}

func TestHandbookLLM_DisabledWithoutModel(t *testing.T) {
	f := newHandbookFixture(t)
	n, err := f.hb.RebuildStale(f.ctx)
	mustCount(t, "first build", n, err, 1)
	n, err = f.hb.EnrichPending(f.ctx)
	mustCount(t, "enrich without model", n, err, 0)
	if err := f.hb.RequestEnrich(f.ctx, f.repoID, false); !errors.Is(err, biz.ErrHandbookLLMDisabled) {
		t.Fatalf("err = %v, want ErrHandbookLLMDisabled", err)
	}
	v, err := f.hb.GetHandbook(f.ctx, f.repoID)
	if err != nil || v.LLMModel != "" || v.LLM != nil || v.LLMRunning {
		t.Fatalf("view = %#v err=%v", v, err)
	}
	f.hb.SetLLM(biz.HandbookLLMConfig{Model: "fake"}, nil)
	if err := f.hb.RequestEnrich(f.ctx, f.repoID, false); !errors.Is(err, biz.ErrHandbookLLMDisabled) {
		t.Fatalf("without a resolver: err = %v, want ErrHandbookLLMDisabled", err)
	}
	n, err = f.hb.RebuildStale(f.ctx)
	mustCount(t, "rebuild without LLM", n, err, 0)
	if _, ok := f.get(t).HandbookStats["llm_rev"]; ok {
		t.Fatalf("stats without LLM must not carry llm_rev: %#v", f.get(t).HandbookStats)
	}
}

func TestHandbookLLM_EnrichThenRerender(t *testing.T) {
	f := newHandbookFixture(t)
	fake := &llmFake{}
	var names []string
	f.hb.SetLLM(biz.HandbookLLMConfig{Model: "fake"}, fakeResolver(fake, &names))
	n, err := f.hb.RebuildStale(f.ctx)
	mustCount(t, "first build", n, err, 1)
	n, err = f.hb.RebuildStale(f.ctx)
	mustCount(t, "an LLM layer that never ran must not cause rebuilds", n, err, 0)

	n, err = f.hb.EnrichPending(f.ctx)
	mustCount(t, "enrich", n, err, 1)
	f.hb.Wait()
	r := f.get(t)
	total, _ := r.HandbookLLM["cards_total"].(float64)
	rev := llmString(r, "rev")
	if llmString(r, "state") != handbook.LLMStateComplete || total < 1 || r.HandbookLLM["cards_done"] != total ||
		rev == "" || llmString(r, "model") != "fake" || llmString(r, "commit") != shaOf('a') ||
		llmString(r, "prompt_version") != handbook.LLMPromptVersion || r.HandbookLLM["card_errors"] != float64(0) ||
		r.HandbookLLM["card_transport_errors"] != float64(0) || r.HandbookLLM["last_error"] != nil ||
		r.HandbookLLM["failed_commit"] != nil || llmString(r, "skeleton_built_at") == "" || r.HandbookLLMLeaseUntil != nil {
		t.Fatalf("handbook_llm = %#v", r.HandbookLLM)
	}
	if r.HandbookStats["llm_rev"] != rev || r.HandbookVersion != 2 {
		t.Fatalf("an enrich run must re-render: version=%d stats=%#v", r.HandbookVersion, r.HandbookStats)
	}
	page, err := f.hb.ReadPage(f.ctx, f.repoID, "references/index.md")
	if err != nil || !strings.Contains(page, "## 执行阶段") {
		t.Fatalf("index = %q err=%v", page, err)
	}
	v, err := f.hb.GetHandbook(f.ctx, f.repoID)
	if err != nil || v.LLMModel != "fake" || llmString(&biz.Repository{HandbookLLM: v.LLM}, "rev") != rev || v.LLMRunning {
		t.Fatalf("view = %#v err=%v", v, err)
	}

	n, err = f.hb.EnrichPending(f.ctx)
	mustCount(t, "a complete run must not repeat", n, err, 0)
	n, err = f.hb.RebuildStale(f.ctx)
	mustCount(t, "a re-rendered handbook must not rebuild again", n, err, 0)
	if len(names) != 1 || names[0] != "fake" {
		t.Fatalf("resolved %v", names)
	}

	other := "provider/other"
	if _, err := f.reg.PatchRepo(f.ctx, f.repoID, biz.RepoMetaPatch{HandbookModel: &other}); err != nil {
		t.Fatal(err)
	}
	if v, _ := f.hb.GetHandbook(f.ctx, f.repoID); v.LLMModel != other {
		t.Fatalf("a repo override must win: %q", v.LLMModel)
	}
	if err := f.hb.RequestEnrich(f.ctx, f.repoID, true); err != nil {
		t.Fatal(err)
	}
	f.hb.Wait()
	if r := f.get(t); llmString(r, "model") != other || llmString(r, "state") != handbook.LLMStateComplete ||
		r.HandbookLLM["skeleton_rebuilt"] != true || llmString(r, "rebuild_reason") != "manual" {
		t.Fatalf("full run with the override: %#v", r.HandbookLLM)
	}
	if names[len(names)-1] != other {
		t.Fatalf("resolved %v", names)
	}
}

func TestHandbookLLM_RepoOverrideOff(t *testing.T) {
	f := newHandbookFixture(t)
	fake := &llmFake{}
	var names []string
	f.hb.SetLLM(biz.HandbookLLMConfig{Model: "fake"}, fakeResolver(fake, &names))
	if _, err := f.hb.RebuildStale(f.ctx); err != nil {
		t.Fatal(err)
	}
	n, err := f.hb.EnrichPending(f.ctx)
	mustCount(t, "enrich", n, err, 1)
	f.hb.Wait()
	rev := llmString(f.get(t), "rev")

	off := biz.HandbookModelOff
	if _, err := f.reg.PatchRepo(f.ctx, f.repoID, biz.RepoMetaPatch{HandbookModel: &off}); err != nil {
		t.Fatal(err)
	}
	n, err = f.hb.RebuildStale(f.ctx)
	mustCount(t, "turning the LLM layer off must re-render once", n, err, 1)
	r := f.get(t)
	if _, ok := r.HandbookStats["llm_rev"]; ok || r.HandbookVersion != 3 {
		t.Fatalf("after off: version=%d stats=%#v", r.HandbookVersion, r.HandbookStats)
	}
	if page, _ := f.hb.ReadPage(f.ctx, f.repoID, "references/index.md"); strings.Contains(page, "## 执行阶段") {
		t.Fatalf("index still renders stages:\n%s", page)
	}
	n, err = f.hb.RebuildStale(f.ctx)
	mustCount(t, "off must not rebuild again", n, err, 0)
	n, err = f.hb.EnrichPending(f.ctx)
	mustCount(t, "enrich while off", n, err, 0)
	if err := f.hb.RequestEnrich(f.ctx, f.repoID, false); !errors.Is(err, biz.ErrHandbookLLMDisabled) {
		t.Fatalf("err = %v, want ErrHandbookLLMDisabled", err)
	}

	inherit := ""
	if _, err := f.reg.PatchRepo(f.ctx, f.repoID, biz.RepoMetaPatch{HandbookModel: &inherit}); err != nil {
		t.Fatal(err)
	}
	n, err = f.hb.RebuildStale(f.ctx)
	mustCount(t, "turning it back on re-renders from the cache", n, err, 1)
	if r := f.get(t); r.HandbookStats["llm_rev"] != rev {
		t.Fatalf("stats = %#v, want llm_rev %q", r.HandbookStats, rev)
	}
	if page, _ := f.hb.ReadPage(f.ctx, f.repoID, "references/index.md"); !strings.Contains(page, "## 执行阶段") {
		t.Fatalf("index lost its stages:\n%s", page)
	}
	if len(names) != 1 {
		t.Fatalf("re-rendering must not call the model: resolved %v", names)
	}
}

func TestHandbookLLM_ModelFailureRecorded(t *testing.T) {
	f := newHandbookFixture(t)
	resolves := 0
	f.hb.SetLLM(biz.HandbookLLMConfig{Model: "fake"}, func(_ context.Context, name string) (model.Model, error) {
		resolves++
		return nil, fmt.Errorf("unknown model %q", name)
	})
	if _, err := f.hb.RebuildStale(f.ctx); err != nil {
		t.Fatal(err)
	}
	n, err := f.hb.EnrichPending(f.ctx)
	mustCount(t, "enrich", n, err, 1)
	r := f.get(t)
	if llmString(r, "state") != handbook.LLMStateFailed || llmString(r, "failed_commit") != shaOf('a') ||
		!strings.Contains(llmString(r, "last_error"), "fake") || llmString(r, "model") != "fake" ||
		llmString(r, "run_at") == "" || r.HandbookLLMLeaseUntil != nil {
		t.Fatalf("handbook_llm = %#v", r.HandbookLLM)
	}
	n, err = f.hb.EnrichPending(f.ctx)
	mustCount(t, "a failed commit must not be retried automatically", n, err, 0)
	n, err = f.hb.RebuildStale(f.ctx)
	mustCount(t, "a failed LLM run must not rebuild", n, err, 0)
	if err := f.hb.RequestEnrich(f.ctx, f.repoID, false); err != nil {
		t.Fatal(err)
	}
	f.hb.Wait()
	if resolves != 2 || f.get(t).HandbookLLMLeaseUntil != nil {
		t.Fatalf("a manual run must retry: resolves=%d", resolves)
	}
}

func TestHandbookLLM_ParentCancelReleases(t *testing.T) {
	f := newHandbookFixture(t)
	ctx, cancel := context.WithCancel(f.ctx)
	defer cancel()
	fake := &llmFake{onCall: cancel}
	var names []string
	f.hb.SetLLM(biz.HandbookLLMConfig{Model: "fake"}, fakeResolver(fake, &names))
	if _, err := f.hb.RebuildStale(f.ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := f.hb.EnrichPending(ctx); err != nil && !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	r := f.get(t)
	if r.HandbookLLMLeaseUntil != nil || len(r.HandbookLLM) != 0 || r.HandbookVersion != 1 {
		t.Fatalf("a cancelled run must release without recording: %#v", r)
	}
	fake.set(func(m *llmFake) { m.onCall = nil })
	n, err := f.hb.EnrichPending(f.ctx)
	mustCount(t, "a cancelled run must be retried", n, err, 1)
	f.hb.Wait()
	if r := f.get(t); llmString(r, "state") != handbook.LLMStateComplete {
		t.Fatalf("after retry: %#v", r.HandbookLLM)
	}
}

func TestHandbookLLM_TransientCardErrorIsPartial(t *testing.T) {
	f := newHandbookFixture(t)
	fake := &llmFake{cardErr: errors.New("upstream 503")}
	var names []string
	f.hb.SetLLM(biz.HandbookLLMConfig{Model: "fake"}, fakeResolver(fake, &names))
	if _, err := f.hb.RebuildStale(f.ctx); err != nil {
		t.Fatal(err)
	}
	n, err := f.hb.EnrichPending(f.ctx)
	mustCount(t, "enrich", n, err, 1)
	f.hb.Wait()
	r := f.get(t)
	if llmString(r, "state") != handbook.LLMStatePartial || r.HandbookLLM["card_transport_errors"] != float64(1) ||
		!strings.Contains(llmString(r, "last_error"), "upstream 503") || r.HandbookLLM["failed_commit"] != nil ||
		r.HandbookLLM["cards_new"] != float64(0) {
		t.Fatalf("handbook_llm = %#v", r.HandbookLLM)
	}
	fake.set(func(m *llmFake) { m.cardErr = nil })
	n, err = f.hb.EnrichPending(f.ctx)
	mustCount(t, "a partial run continues", n, err, 1)
	f.hb.Wait()
	r = f.get(t)
	if llmString(r, "state") != handbook.LLMStateComplete || r.HandbookLLM["last_error"] != nil ||
		r.HandbookLLM["card_transport_errors"] != float64(0) || r.HandbookStats["llm_rev"] != r.HandbookLLM["rev"] {
		t.Fatalf("after a clean run: llm=%#v stats=%#v", r.HandbookLLM, r.HandbookStats)
	}
}

func TestHandbookLLM_RequestEnrichErrors(t *testing.T) {
	f := newHandbookFixture(t)
	fake := &llmFake{}
	var names []string
	f.hb.SetLLM(biz.HandbookLLMConfig{Model: "fake"}, fakeResolver(fake, &names))
	if err := f.hb.RequestEnrich(f.ctx, f.repoID, false); !errors.Is(err, biz.ErrHandbookNotReady) {
		t.Fatalf("before a build: err = %v, want ErrHandbookNotReady", err)
	}
	if err := f.hb.RequestEnrich(f.ctx, "nope", false); !errors.Is(err, biz.ErrRepoNotFound) {
		t.Fatalf("err = %v, want ErrRepoNotFound", err)
	}
	if _, err := f.hb.RebuildStale(f.ctx); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	token, ok, err := f.repo.ClaimHandbookEnrich(f.ctx, f.repoID, now, now.Add(time.Hour))
	if err != nil || !ok {
		t.Fatalf("claim: ok=%v err=%v", ok, err)
	}
	if err := f.hb.RequestEnrich(f.ctx, f.repoID, false); !errors.Is(err, biz.ErrHandbookBuilding) {
		t.Fatalf("while leased: err = %v, want ErrHandbookBuilding", err)
	}
	if v, _ := f.hb.GetHandbook(f.ctx, f.repoID); !v.LLMRunning {
		t.Fatal("a held LLM lease must show as running")
	}
	n, err := f.hb.EnrichPending(f.ctx)
	mustCount(t, "enrich while leased", n, err, 0)
	if err := f.repo.ReleaseHandbookEnrich(f.ctx, f.repoID, token); err != nil {
		t.Fatal(err)
	}

	setRepoHead(t, f.repoDir, shaOf('b'))
	if _, err := f.reg.Scan(f.ctx); err != nil {
		t.Fatal(err)
	}
	if err := f.hb.RequestEnrich(f.ctx, f.repoID, false); !errors.Is(err, biz.ErrHandbookNotReady) {
		t.Fatalf("HEAD ahead of the handbook: err = %v, want ErrHandbookNotReady", err)
	}
	archived := biz.RepoStatusArchived
	if _, err := f.reg.PatchRepo(f.ctx, f.repoID, biz.RepoMetaPatch{Status: &archived}); err != nil {
		t.Fatal(err)
	}
	if err := f.hb.RequestEnrich(f.ctx, f.repoID, false); !errors.Is(err, biz.ErrInvalidRepo) {
		t.Fatalf("archived: err = %v, want ErrInvalidRepo", err)
	}
	if len(names) != 0 {
		t.Fatalf("no run may start: resolved %v", names)
	}
}
