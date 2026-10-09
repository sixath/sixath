package data

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"backend/internal/biz"
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
