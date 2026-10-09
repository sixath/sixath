package data

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"backend/internal/biz"
	"backend/internal/handbook"

	"github.com/go-kratos/kratos/v2/log"
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
	if got, err := set(" OFF "); err != nil || got.HandbookModel != biz.HandbookModelOff {
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
	mu       sync.Mutex
	calls    int
	cardErr  error
	synthErr error         // returned by every prompt but cards
	block    bool          // calls wait for their context to end
	gate     chan struct{} // calls wait for gate to close
	onCall   func()
}

func (m *llmFake) Chat(ctx context.Context, msgs []model.Message, _ ...model.Option) (*model.Generation, error) {
	m.mu.Lock()
	m.calls++
	onCall, cardErr, synthErr, block, gate := m.onCall, m.cardErr, m.synthErr, m.block, m.gate
	m.mu.Unlock()
	if onCall != nil {
		onCall()
	}
	if block {
		<-ctx.Done()
	}
	if gate != nil {
		<-gate
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
	case synthErr != nil:
		return nil, synthErr
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
		r.HandbookLLM["failed_commit"] != nil || llmString(r, "skeleton_built_at") == "" || r.HandbookLLMLeaseUntil != nil ||
		r.HandbookLLM["fallback"] != true || llmString(r, "fallback_reason") != handbook.FallbackBadReply {
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
	if err := f.repo.FinishHandbookEnrich(f.ctx, f.repoID, token, nil); err != nil {
		t.Fatal(err)
	}
	patchStats(t, f, func(m map[string]any) { m["generator_version"] = "old" })
	if err := f.hb.RequestEnrich(f.ctx, f.repoID, false); !errors.Is(err, biz.ErrHandbookNotReady) {
		t.Fatalf("an outdated generator: err = %v, want ErrHandbookNotReady", err)
	}
	n, err := f.hb.EnrichPending(f.ctx)
	mustCount(t, "enrich before the deterministic rebuild", n, err, 0)
	n, err = f.hb.RebuildStale(f.ctx)
	mustCount(t, "rebuild for the generator", n, err, 1)
	token, ok, err = f.repo.ClaimHandbookEnrich(f.ctx, f.repoID, now, now.Add(time.Hour))
	if err != nil || !ok {
		t.Fatalf("claim: ok=%v err=%v", ok, err)
	}
	if v, _ := f.hb.GetHandbook(f.ctx, f.repoID); !v.LLMRunning {
		t.Fatal("a held LLM lease must show as running")
	}
	n, err = f.hb.EnrichPending(f.ctx)
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

// patchLLM rewrites the stored handbook_llm of id through the LLM lease.
func patchLLM(t *testing.T, f *handbookFixture, id string, mutate func(map[string]any)) {
	t.Helper()
	m, err := f.repo.GetRepositoriesByIDs(f.ctx, []string{id})
	if err != nil {
		t.Fatal(err)
	}
	llm := map[string]any{}
	for k, v := range m[id].HandbookLLM {
		llm[k] = v
	}
	mutate(llm)
	now := time.Now().UTC()
	token, ok, err := f.repo.ClaimHandbookEnrich(f.ctx, id, now, now.Add(time.Minute))
	if err != nil || !ok {
		t.Fatalf("claim: ok=%v err=%v", ok, err)
	}
	if err := f.repo.FinishHandbookEnrich(f.ctx, id, token, llm); err != nil {
		t.Fatal(err)
	}
}

// patchStats rewrites the stored handbook_stats through the build lease.
func patchStats(t *testing.T, f *handbookFixture, mutate func(map[string]any)) {
	t.Helper()
	r := f.get(t)
	stats := map[string]any{}
	for k, v := range r.HandbookStats {
		stats[k] = v
	}
	mutate(stats)
	now := time.Now().UTC()
	token, ok, err := f.repo.ClaimHandbookBuild(f.ctx, f.repoID, now, now.Add(time.Minute))
	if err != nil || !ok {
		t.Fatalf("claim: ok=%v err=%v", ok, err)
	}
	if err := f.repo.FinishHandbookBuild(f.ctx, f.repoID, token, biz.HandbookBuildResult{
		Status: biz.HandbookStatusReady, Commit: r.HandbookCommit, Version: r.HandbookVersion, Stats: stats,
	}); err != nil {
		t.Fatal(err)
	}
}

// addSecondRepo gives svc-b of the fixture a commit and a source file and returns its id.
func addSecondRepo(t *testing.T, f *handbookFixture) string {
	t.Helper()
	dir := filepath.Join(f.codeRoot, "cloudgame", "svc-b")
	setRepoHead(t, dir, shaOf('c'))
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n\nfunc main() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := f.reg.Scan(f.ctx); err != nil {
		t.Fatal(err)
	}
	repos, err := f.reg.ListRepos(f.ctx, biz.RepoFilter{Query: "svc-b"})
	if err != nil || len(repos) != 1 {
		t.Fatalf("repos = %v err=%v", repos, err)
	}
	return repos[0].ID
}

func (f *handbookFixture) getID(t *testing.T, id string) *biz.Repository {
	t.Helper()
	m, err := f.repo.GetRepositoriesByIDs(f.ctx, []string{id})
	if err != nil {
		t.Fatal(err)
	}
	return m[id]
}

func TestHandbookLLM_FailedRetriesAfterModelChange(t *testing.T) {
	for _, tc := range []struct {
		name string
		fix  func(f *handbookFixture, resolve biz.HandbookModelResolver)
	}{
		{"global", func(f *handbookFixture, resolve biz.HandbookModelResolver) {
			f.hb.SetLLM(biz.HandbookLLMConfig{Model: "good"}, resolve)
		}},
		{"override", func(f *handbookFixture, _ biz.HandbookModelResolver) {
			good := "good"
			if _, err := f.reg.PatchRepo(f.ctx, f.repoID, biz.RepoMetaPatch{HandbookModel: &good}); err != nil {
				t.Fatal(err)
			}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newHandbookFixture(t)
			fake := &llmFake{}
			resolve := func(_ context.Context, name string) (model.Model, error) {
				if name == "bad" {
					return nil, errors.New("unknown model")
				}
				return fake, nil
			}
			f.hb.SetLLM(biz.HandbookLLMConfig{Model: "bad"}, resolve)
			if _, err := f.hb.RebuildStale(f.ctx); err != nil {
				t.Fatal(err)
			}
			n, err := f.hb.EnrichPending(f.ctx)
			mustCount(t, "enrich", n, err, 1)
			if r := f.get(t); llmString(r, "state") != handbook.LLMStateFailed || llmString(r, "model") != "bad" {
				t.Fatalf("handbook_llm = %#v", r.HandbookLLM)
			}
			n, err = f.hb.EnrichPending(f.ctx)
			mustCount(t, "same model", n, err, 0)
			tc.fix(f, resolve)
			n, err = f.hb.EnrichPending(f.ctx)
			mustCount(t, "a changed model must retry the failed commit", n, err, 1)
			f.hb.Wait()
			if r := f.get(t); llmString(r, "state") != handbook.LLMStateComplete || llmString(r, "model") != "good" ||
				r.HandbookLLM["failed_commit"] != nil || r.HandbookLLM["last_error"] != nil {
				t.Fatalf("after the fix: %#v", r.HandbookLLM)
			}
		})
	}
}

func TestHandbookLLM_RerunTriggers(t *testing.T) {
	f := newHandbookFixture(t)
	fake := &llmFake{}
	var names []string
	resolve := fakeResolver(fake, &names)
	f.hb.SetLLM(biz.HandbookLLMConfig{Model: "fake"}, resolve)
	if _, err := f.hb.RebuildStale(f.ctx); err != nil {
		t.Fatal(err)
	}
	n, err := f.hb.EnrichPending(f.ctx)
	mustCount(t, "enrich", n, err, 1)
	f.hb.Wait()
	ago := func(d time.Duration) string { return time.Now().UTC().Add(-d).Format(time.RFC3339) }
	for _, tc := range []struct {
		name   string
		days   int
		mutate func(map[string]any)
		want   int
	}{
		{"current", 0, func(map[string]any) {}, 0},
		{"prompt version", 0, func(m map[string]any) { m["prompt_version"] = "old" }, 1},
		{"other commit", 0, func(m map[string]any) { m["commit"] = shaOf('z') }, 1},
		{"fallback retry", 0, func(m map[string]any) { m["skeleton_built_at"] = ago(25 * time.Hour) }, 1},
		{"fallback retry too early", 0, func(m map[string]any) { m["skeleton_built_at"] = ago(23 * time.Hour) }, 0},
		{"fallback without a bad reply", 0, func(m map[string]any) {
			m["fallback_reason"] = handbook.FallbackFewStages
			m["skeleton_built_at"] = ago(48 * time.Hour)
		}, 0},
		{"skeleton age", 1, func(m map[string]any) {
			m["fallback_reason"] = handbook.FallbackFewStages
			m["skeleton_built_at"] = ago(48 * time.Hour)
		}, 1},
	} {
		f.hb.SetLLM(biz.HandbookLLMConfig{Model: "fake", SkeletonRebuildDays: tc.days}, resolve)
		patchLLM(t, f, f.repoID, tc.mutate)
		n, err := f.hb.EnrichPending(f.ctx)
		mustCount(t, tc.name, n, err, tc.want)
		f.hb.Wait()
		r := f.get(t)
		if tc.want == 1 && (llmString(r, "state") != handbook.LLMStateComplete || llmString(r, "prompt_version") != handbook.LLMPromptVersion ||
			llmString(r, "commit") != shaOf('a') || llmString(r, "fallback_reason") != handbook.FallbackBadReply) {
			t.Fatalf("%s: after the rerun %#v", tc.name, r.HandbookLLM)
		}
		if tc.want == 0 {
			// Restore a current state for the next case.
			patchLLM(t, f, f.repoID, func(m map[string]any) {
				m["fallback_reason"] = handbook.FallbackBadReply
				m["skeleton_built_at"] = ago(time.Minute)
			})
		}
	}
}

func TestHandbookLLM_LostLeaseDropsResult(t *testing.T) {
	f := newHandbookFixture(t)
	var once sync.Once
	var thief string
	fake := &llmFake{}
	fake.onCall = func() {
		once.Do(func() {
			later := time.Now().UTC().Add(2 * time.Hour)
			token, ok, err := f.repo.ClaimHandbookEnrich(f.ctx, f.repoID, later, later.Add(time.Hour))
			if err != nil || !ok {
				t.Errorf("steal: ok=%v err=%v", ok, err)
			}
			thief = token
		})
	}
	var names []string
	f.hb.SetLLM(biz.HandbookLLMConfig{Model: "fake"}, fakeResolver(fake, &names))
	if _, err := f.hb.RebuildStale(f.ctx); err != nil {
		t.Fatal(err)
	}
	n, err := f.hb.EnrichPending(f.ctx)
	mustCount(t, "enrich", n, err, 1)
	f.hb.Wait()
	r := f.get(t)
	if len(r.HandbookLLM) != 0 || r.HandbookLLMLeaseUntil == nil || r.HandbookVersion != 1 {
		t.Fatalf("a run that lost its lease must not write its result: %#v", r)
	}
	if err := f.repo.FinishHandbookEnrich(f.ctx, f.repoID, thief, map[string]any{"state": "partial"}); err != nil {
		t.Fatalf("the new lease holder must still own the row: %v", err)
	}
}

func TestHandbookLLM_RunTimeoutRecordsPartial(t *testing.T) {
	f := newHandbookFixture(t)
	fake := &llmFake{block: true}
	var names []string
	f.hb.SetLLM(biz.HandbookLLMConfig{Model: "fake"}, fakeResolver(fake, &names))
	f.hb.SetEnrichRunTimeout(50 * time.Millisecond)
	if _, err := f.hb.RebuildStale(f.ctx); err != nil {
		t.Fatal(err)
	}
	n, err := f.hb.EnrichPending(f.ctx)
	mustCount(t, "enrich", n, err, 1)
	f.hb.Wait()
	r := f.get(t)
	if llmString(r, "state") != handbook.LLMStatePartial || r.HandbookLLM["failed_commit"] != nil ||
		r.HandbookLLM["cards_new"] != float64(0) || r.HandbookLLMLeaseUntil != nil {
		t.Fatalf("a run cut by its timeout must record partial: %#v", r)
	}
}

func TestHandbookLLM_PendingOrderAndBudget(t *testing.T) {
	f := newHandbookFixture(t)
	second := addSecondRepo(t, f)
	fake := &llmFake{block: true}
	var names []string
	f.hb.SetLLM(biz.HandbookLLMConfig{Model: "fake"}, fakeResolver(fake, &names))
	f.hb.SetEnrichRunTimeout(50 * time.Millisecond)
	n, err := f.hb.RebuildStale(f.ctx)
	mustCount(t, "builds", n, err, 2)
	runAt := func(id string) string { return llmString(f.getID(t, id), "run_at") }

	n, err = f.hb.EnrichPending(f.ctx)
	mustCount(t, "the budget allows one run per call", n, err, 1)
	f.hb.Wait()
	first, other := f.repoID, second
	if runAt(first) == "" {
		first, other = second, f.repoID
	}
	if runAt(other) != "" {
		t.Fatal("only one repo may run")
	}
	n, err = f.hb.EnrichPending(f.ctx)
	mustCount(t, "second call", n, err, 1)
	f.hb.Wait()
	if runAt(other) == "" {
		t.Fatal("a repo that never ran must go before one that ran")
	}

	patchLLM(t, f, other, func(m map[string]any) { m["run_at"] = "2000-01-01T00:00:00Z" })
	before := runAt(first)
	n, err = f.hb.EnrichPending(f.ctx)
	mustCount(t, "third call", n, err, 1)
	f.hb.Wait()
	if runAt(other) == "2000-01-01T00:00:00Z" || runAt(first) != before {
		t.Fatalf("the oldest run must go first: first=%s other=%s", runAt(first), runAt(other))
	}
}

func TestHandbookLLM_BusyWithAnotherRepo(t *testing.T) {
	f := newHandbookFixture(t)
	second := addSecondRepo(t, f)
	gate := make(chan struct{})
	fake := &llmFake{gate: gate}
	var names []string
	f.hb.SetLLM(biz.HandbookLLMConfig{Model: "fake"}, fakeResolver(fake, &names))
	n, err := f.hb.RebuildStale(f.ctx)
	mustCount(t, "builds", n, err, 2)
	if err := f.hb.RequestEnrich(f.ctx, f.repoID, false); err != nil {
		t.Fatal(err)
	}
	if err := f.hb.RequestEnrich(f.ctx, second, false); !errors.Is(err, biz.ErrHandbookLLMBusy) {
		t.Fatalf("another repo running: err = %v, want ErrHandbookLLMBusy", err)
	}
	if err := f.hb.RequestEnrich(f.ctx, f.repoID, false); !errors.Is(err, biz.ErrHandbookBuilding) {
		t.Fatalf("this repo running: err = %v, want ErrHandbookBuilding", err)
	}
	ctx, cancel := context.WithTimeout(f.ctx, 50*time.Millisecond)
	defer cancel()
	if n, err := f.hb.EnrichPending(ctx); n != 0 || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("EnrichPending must wait for the slot: n=%d err=%v", n, err)
	}
	close(gate)
	f.hb.Wait()
	if r := f.get(t); llmString(r, "state") != handbook.LLMStateComplete {
		t.Fatalf("after the gate: %#v", r.HandbookLLM)
	}
}

func TestHandbookLLM_FailureKeepsGeneratedCards(t *testing.T) {
	f := newHandbookFixture(t)
	fake := &llmFake{synthErr: errors.New("synth down")}
	var names []string
	f.hb.SetLLM(biz.HandbookLLMConfig{Model: "fake"}, fakeResolver(fake, &names))
	if _, err := f.hb.RebuildStale(f.ctx); err != nil {
		t.Fatal(err)
	}
	n, err := f.hb.EnrichPending(f.ctx)
	mustCount(t, "enrich", n, err, 1)
	f.hb.Wait()
	r := f.get(t)
	rev := llmString(r, "rev")
	if llmString(r, "state") != handbook.LLMStateFailed || !strings.Contains(llmString(r, "last_error"), "synth down") || rev == "" {
		t.Fatalf("handbook_llm = %#v", r.HandbookLLM)
	}
	if r.HandbookStats["llm_rev"] != rev || r.HandbookStats["cards"] != float64(1) || r.HandbookVersion != 2 {
		t.Fatalf("cards generated before the failure must render: version=%d stats=%#v", r.HandbookVersion, r.HandbookStats)
	}
}

func TestHandbookLLM_LocalErrorRetriesNextPass(t *testing.T) {
	f := newHandbookFixture(t)
	fake := &llmFake{}
	var names []string
	f.hb.SetLLM(biz.HandbookLLMConfig{Model: "fake"}, fakeResolver(fake, &names))
	if _, err := f.hb.RebuildStale(f.ctx); err != nil {
		t.Fatal(err)
	}
	moved := f.repoDir + ".moved"
	if err := os.Rename(f.repoDir, moved); err != nil {
		t.Fatal(err)
	}
	n, err := f.hb.EnrichPending(f.ctx)
	mustCount(t, "enrich", n, err, 1)
	r := f.get(t)
	if llmString(r, "state") != "" || llmString(r, "last_error") == "" || llmString(r, "run_at") == "" ||
		r.HandbookLLM["failed_commit"] != nil || r.HandbookLLMLeaseUntil != nil {
		t.Fatalf("a local error must record last_error only: %#v", r)
	}
	if err := os.Rename(moved, f.repoDir); err != nil {
		t.Fatal(err)
	}
	n, err = f.hb.EnrichPending(f.ctx)
	mustCount(t, "the next pass retries", n, err, 1)
	f.hb.Wait()
	if r := f.get(t); llmString(r, "state") != handbook.LLMStateComplete || r.HandbookLLM["last_error"] != nil {
		t.Fatalf("after the retry: %#v", r.HandbookLLM)
	}
}

// enrichRaceRepo runs beforeClaim once before the next LLM claim.
type enrichRaceRepo struct {
	biz.RepoRegistryRepo
	beforeClaim func()
}

func (h *enrichRaceRepo) ClaimHandbookEnrich(ctx context.Context, id string, now, leaseUntil time.Time) (string, bool, error) {
	if fn := h.beforeClaim; fn != nil {
		h.beforeClaim = nil
		fn()
	}
	return h.RepoRegistryRepo.ClaimHandbookEnrich(ctx, id, now, leaseUntil)
}

func TestHandbookLLM_PendingSkipsRunFinishedBeforeClaim(t *testing.T) {
	f := newHandbookFixture(t)
	if _, err := f.hb.RebuildStale(f.ctx); err != nil {
		t.Fatal(err)
	}
	race := &enrichRaceRepo{RepoRegistryRepo: f.repo, beforeClaim: func() {
		// a manual run completes between the listing and the claim
		patchLLM(t, f, f.repoID, func(m map[string]any) {
			m["state"] = handbook.LLMStateComplete
			m["commit"] = shaOf('a')
			m["prompt_version"] = handbook.LLMPromptVersion
		})
	}}
	hb := biz.NewHandbookUsecase(race, f.reg, f.dataRoot, log.DefaultLogger)
	var names []string
	hb.SetLLM(biz.HandbookLLMConfig{Model: "fake"}, fakeResolver(&llmFake{}, &names))
	n, err := hb.EnrichPending(f.ctx)
	mustCount(t, "enrich", n, err, 0)
	hb.Wait()
	if r := f.get(t); len(names) != 0 || r.HandbookLLMLeaseUntil != nil || r.HandbookLLM["run_at"] != nil {
		t.Fatalf("a repo no longer due must be released without a run: resolved=%v repo=%#v", names, r)
	}
}

func TestHandbookLLM_ShutdownCancelsManualRun(t *testing.T) {
	f := newHandbookFixture(t)
	called := make(chan struct{})
	var once sync.Once
	fake := &llmFake{block: true, onCall: func() { once.Do(func() { close(called) }) }}
	var names []string
	f.hb.SetLLM(biz.HandbookLLMConfig{Model: "fake"}, fakeResolver(fake, &names))
	if _, err := f.hb.RebuildStale(f.ctx); err != nil {
		t.Fatal(err)
	}
	reqCtx, cancelReq := context.WithCancel(f.ctx)
	if err := f.hb.RequestEnrich(reqCtx, f.repoID, false); err != nil {
		t.Fatal(err)
	}
	cancelReq() // the HTTP request ending must not stop the run
	<-called
	if r := f.get(t); r.HandbookLLMLeaseUntil == nil {
		t.Fatal("the run must hold its lease")
	}
	ctx, cancel := context.WithTimeout(f.ctx, 10*time.Second)
	defer cancel()
	if err := f.hb.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}
	r := f.get(t)
	if r.HandbookLLMLeaseUntil != nil || len(r.HandbookLLM) != 0 || r.HandbookVersion != 1 {
		t.Fatalf("a run cancelled by shutdown must release without recording: %#v", r)
	}
	if err := f.hb.RequestEnrich(f.ctx, f.repoID, false); !errors.Is(err, biz.ErrHandbookShutdown) {
		t.Fatalf("after shutdown: err = %v, want ErrHandbookShutdown", err)
	}
	if err := f.hb.RequestRebuild(f.ctx, f.repoID); !errors.Is(err, biz.ErrHandbookShutdown) {
		t.Fatalf("rebuild after shutdown: err = %v, want ErrHandbookShutdown", err)
	}
	if n, err := f.hb.EnrichPending(f.ctx); n != 0 || err == nil {
		t.Fatalf("EnrichPending after shutdown: n=%d err=%v", n, err)
	}
}

func TestHandbookLLM_CacheWriteErrorRetriesNextPass(t *testing.T) {
	f := newHandbookFixture(t)
	fake := &llmFake{}
	var names []string
	f.hb.SetLLM(biz.HandbookLLMConfig{Model: "fake"}, fakeResolver(fake, &names))
	if _, err := f.hb.RebuildStale(f.ctx); err != nil {
		t.Fatal(err)
	}
	cache, err := handbook.Store{Root: filepath.Join(f.dataRoot, "handbooks")}.LLMCache(f.repoID)
	if err != nil {
		t.Fatal(err)
	}
	// a file where the cards directory belongs makes every card write fail
	blocker := filepath.Join(cache.Dir, "cards")
	if err := os.MkdirAll(cache.Dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(blocker, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	n, err := f.hb.EnrichPending(f.ctx)
	mustCount(t, "enrich", n, err, 1)
	f.hb.Wait()
	r := f.get(t)
	if llmString(r, "state") != "" || !strings.Contains(llmString(r, "last_error"), "cache") || llmString(r, "run_at") == "" ||
		r.HandbookLLM["failed_commit"] != nil || r.HandbookLLMLeaseUntil != nil {
		t.Fatalf("a cache write error must record last_error only: %#v", r)
	}
	if err := os.Remove(blocker); err != nil {
		t.Fatal(err)
	}
	n, err = f.hb.EnrichPending(f.ctx)
	mustCount(t, "the next pass retries", n, err, 1)
	f.hb.Wait()
	if r := f.get(t); llmString(r, "state") != handbook.LLMStateComplete || r.HandbookLLM["last_error"] != nil {
		t.Fatalf("after the retry: %#v", r.HandbookLLM)
	}
}
