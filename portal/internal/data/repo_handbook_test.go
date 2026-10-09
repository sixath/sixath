package data

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"backend/internal/biz"
	"backend/internal/handbook"

	"github.com/go-kratos/kratos/v2/log"
	"github.com/sixath/framework/skills"
)

func TestRepoRegistryRepo_HandbookClaimAndFinish(t *testing.T) {
	ctx := context.Background()
	r := newRepoRegistryRepoForTest(t)
	now := time.Now()
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

	if ok, err := r.ClaimHandbookBuild(ctx, a.ID, now, now.Add(time.Minute)); err != nil || !ok {
		t.Fatalf("first claim: ok=%v err=%v", ok, err)
	}
	if got := reload(); got.HandbookStatus != biz.HandbookStatusBuilding {
		t.Fatalf("status = %s", got.HandbookStatus)
	}
	if ok, _ := r.ClaimHandbookBuild(ctx, a.ID, now, now.Add(time.Minute)); ok {
		t.Fatal("second claim must fail while the lease is live")
	}
	if ok, _ := r.ClaimHandbookBuild(ctx, a.ID, now.Add(2*time.Minute), now.Add(3*time.Minute)); !ok {
		t.Fatal("an expired lease must be claimable")
	}

	if err := r.FinishHandbookBuild(ctx, a.ID, biz.HandbookBuildResult{
		Status: biz.HandbookStatusReady, Commit: "111", Version: 1, Stats: map[string]any{"files": 3},
	}); err != nil {
		t.Fatal(err)
	}
	g := reload()
	if g.HandbookStatus != biz.HandbookStatusReady || g.HandbookCommit != "111" || g.HandbookVersion != 1 || g.HandbookStats["files"] != float64(3) {
		t.Fatalf("after ready: %#v", g)
	}

	if ok, _ := r.ClaimHandbookBuild(ctx, a.ID, now.Add(5*time.Minute), now.Add(6*time.Minute)); !ok {
		t.Fatal("a finished build must release the lease")
	}
	if err := r.FinishHandbookBuild(ctx, a.ID, biz.HandbookBuildResult{
		Status: biz.HandbookStatusFailed, Stats: map[string]any{"last_error": "boom"},
	}); err != nil {
		t.Fatal(err)
	}
	g = reload()
	if g.HandbookStatus != biz.HandbookStatusFailed || g.HandbookCommit != "111" || g.HandbookVersion != 1 || g.HandbookStats["last_error"] != "boom" {
		t.Fatalf("a failed build must keep commit and version: %#v", g)
	}
}

func TestRepoRegistryRepo_ClaimUnknownRepo(t *testing.T) {
	r := newRepoRegistryRepoForTest(t)
	now := time.Now()
	if ok, err := r.ClaimHandbookBuild(context.Background(), "nope", now, now.Add(time.Minute)); err != nil || ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
}

func setRepoHead(t *testing.T, dir, sha string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".git", "HEAD"), []byte(sha+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func shaOf(c byte) string { return strings.Repeat(string(c), 40) }

type handbookFixture struct {
	ctx      context.Context
	codeRoot string
	repoDir  string
	reg      *biz.RepoRegistryUsecase
	repo     biz.RepoRegistryRepo
	hb       *biz.HandbookUsecase
	repoID   string
}

func newHandbookFixture(t *testing.T) *handbookFixture {
	t.Helper()
	ctx := context.Background()
	codeRoot := t.TempDir()
	repoDir := filepath.Join(codeRoot, "cloudgame", "svc-a")
	setRepoHead(t, repoDir, shaOf('a'))
	if err := os.MkdirAll(filepath.Join(repoDir, "internal", "order"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repoDir, "internal", "order", "store.go"),
		[]byte("// Package order stores orders.\npackage order\n\nfunc Get() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mkRepoDir(t, filepath.Join(codeRoot, "cloudgame", "svc-b"))
	reg, repo := newUsecaseForTest(t, codeRoot, &biz.AgentMeta{ID: "ag"}, &biz.AgentMeta{ID: "other"})
	if _, err := reg.Scan(ctx); err != nil {
		t.Fatal(err)
	}
	repos, err := reg.ListRepos(ctx, biz.RepoFilter{Query: "svc-a"})
	if err != nil || len(repos) != 1 {
		t.Fatalf("repos = %v err=%v", repos, err)
	}
	hb := biz.NewHandbookUsecase(repo, reg, t.TempDir(), log.DefaultLogger)
	return &handbookFixture{ctx: ctx, codeRoot: codeRoot, repoDir: repoDir, reg: reg, repo: repo, hb: hb, repoID: repos[0].ID}
}

func (f *handbookFixture) get(t *testing.T) *biz.Repository {
	t.Helper()
	m, err := f.repo.GetRepositoriesByIDs(f.ctx, []string{f.repoID})
	if err != nil {
		t.Fatal(err)
	}
	return m[f.repoID]
}

func TestHandbookUsecase_RebuildStaleAndSkillDirs(t *testing.T) {
	f := newHandbookFixture(t)
	n, err := f.hb.RebuildStale(f.ctx)
	if err != nil || n != 1 {
		t.Fatalf("first pass n=%d err=%v (svc-b has no commit and must be skipped)", n, err)
	}
	r := f.get(t)
	if r.HandbookStatus != biz.HandbookStatusReady || r.HandbookCommit != shaOf('a') || r.HandbookVersion != 1 ||
		r.HandbookStats["generator_version"] != handbook.GeneratorVersion {
		t.Fatalf("after build: %#v", r)
	}
	if n, _ := f.hb.RebuildStale(f.ctx); n != 0 {
		t.Fatalf("second pass rebuilt %d repos", n)
	}

	if dirs, err := f.hb.SkillDirsForAgent(f.ctx, "ag"); err != nil || dirs != nil {
		t.Fatalf("unbound agent: dirs=%v err=%v", dirs, err)
	}
	if _, err := f.reg.ReplaceBindings(f.ctx, "ag", []*biz.AgentRepoBinding{{TargetKind: biz.RepoTargetRepo, TargetID: f.repoID}}, "alice"); err != nil {
		t.Fatal(err)
	}
	dirs, err := f.hb.SkillDirsForAgent(f.ctx, "ag")
	if err != nil || len(dirs) != 2 {
		t.Fatalf("dirs = %v err=%v", dirs, err)
	}
	idx, err := skills.NewIndex(dirs, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	cm, ok := idx.GetByName("code-map")
	if !ok || !cm.SummaryPinned {
		t.Fatalf("code-map = %#v ok=%v", cm, ok)
	}
	body, _ := idx.LoadSkillBody("code-map")
	if !strings.Contains(body, `skill_view("handbook-cloudgame-svc-a")`) {
		t.Fatalf("code-map body:\n%s", body)
	}
	if hbMeta, ok := idx.GetByName("handbook-cloudgame-svc-a"); !ok || !hbMeta.HiddenFromSummary {
		t.Fatalf("handbook skill = %#v ok=%v", hbMeta, ok)
	}

	setRepoHead(t, f.repoDir, shaOf('b'))
	if _, err := f.reg.Scan(f.ctx); err != nil {
		t.Fatal(err)
	}
	if n, _ := f.hb.RebuildStale(f.ctx); n != 1 {
		t.Fatalf("HEAD change must trigger a rebuild, n=%d", n)
	}
	if r := f.get(t); r.HandbookVersion != 2 || r.HandbookCommit != shaOf('b') {
		t.Fatalf("after head change: %#v", r)
	}
}

func TestHandbookUsecase_FailureIsNotRetriedUntilHeadMoves(t *testing.T) {
	f := newHandbookFixture(t)
	f.hb.SetBuilder(func(context.Context, handbook.BuildInput) (*handbook.Output, error) {
		return nil, errors.New("boom")
	})
	if _, err := f.hb.RebuildStale(f.ctx); err != nil {
		t.Fatal(err)
	}
	r := f.get(t)
	if r.HandbookStatus != biz.HandbookStatusFailed || r.HandbookStats["failed_commit"] != shaOf('a') ||
		!strings.Contains(r.HandbookStats["last_error"].(string), "boom") || r.HandbookVersion != 0 {
		t.Fatalf("after failure: %#v", r)
	}
	if n, _ := f.hb.RebuildStale(f.ctx); n != 0 {
		t.Fatal("a failed commit must not be retried automatically")
	}

	f.hb.SetBuilder(handbook.Build)
	if err := f.hb.RequestRebuild(f.ctx, f.repoID); err != nil {
		t.Fatal(err)
	}
	f.hb.Wait()
	if r := f.get(t); r.HandbookStatus != biz.HandbookStatusReady || r.HandbookVersion != 1 {
		t.Fatalf("manual rebuild: %#v", r)
	}
}

func TestHandbookUsecase_RequestRebuildConflictsAndValidates(t *testing.T) {
	f := newHandbookFixture(t)
	now := time.Now()
	if ok, _ := f.repo.ClaimHandbookBuild(f.ctx, f.repoID, now, now.Add(time.Hour)); !ok {
		t.Fatal("claim")
	}
	if err := f.hb.RequestRebuild(f.ctx, f.repoID); !errors.Is(err, biz.ErrHandbookBuilding) {
		t.Fatalf("err = %v, want ErrHandbookBuilding", err)
	}
	if err := f.hb.RequestRebuild(f.ctx, "nope"); !errors.Is(err, biz.ErrRepoNotFound) {
		t.Fatalf("err = %v, want ErrRepoNotFound", err)
	}
	archived := biz.RepoStatusArchived
	if _, err := f.reg.PatchRepo(f.ctx, f.repoID, biz.RepoMetaPatch{Status: &archived}); err != nil {
		t.Fatal(err)
	}
	if err := f.hb.RequestRebuild(f.ctx, f.repoID); !errors.Is(err, biz.ErrInvalidRepo) {
		t.Fatalf("err = %v, want ErrInvalidRepo", err)
	}
}

func TestHandbookUsecase_ViewAndPages(t *testing.T) {
	f := newHandbookFixture(t)
	if _, err := f.hb.ReadPage(f.ctx, f.repoID, "SKILL.md"); !errors.Is(err, biz.ErrHandbookNotFound) {
		t.Fatalf("before build err = %v", err)
	}
	if _, err := f.hb.RebuildStale(f.ctx); err != nil {
		t.Fatal(err)
	}
	v, err := f.hb.GetHandbook(f.ctx, f.repoID)
	if err != nil || v.Version != 1 || v.HeadCommit != shaOf('a') {
		t.Fatalf("view = %#v err=%v", v, err)
	}
	joined := strings.Join(v.Pages, ",")
	if !strings.Contains(joined, "SKILL.md") || !strings.Contains(joined, "references/areas/internal-order.md") {
		t.Fatalf("pages = %v", v.Pages)
	}
	page, err := f.hb.ReadPage(f.ctx, f.repoID, "references/index.md")
	if err != nil || !strings.Contains(page, "Package order stores orders.") {
		t.Fatalf("page = %q err=%v", page, err)
	}
	if _, err := f.hb.ReadPage(f.ctx, f.repoID, "../manifest.json"); !errors.Is(err, biz.ErrInvalidRepo) {
		t.Fatalf("traversal err = %v", err)
	}
	if _, err := f.hb.ReadPage(f.ctx, f.repoID, "nope.md"); !errors.Is(err, biz.ErrHandbookNotFound) {
		t.Fatalf("missing page err = %v", err)
	}
}

func TestHandbookUsecase_CodeMapWithoutHandbook(t *testing.T) {
	f := newHandbookFixture(t)
	if _, err := f.reg.ReplaceBindings(f.ctx, "other", []*biz.AgentRepoBinding{{TargetKind: biz.RepoTargetRepo, TargetID: f.repoID}}, "alice"); err != nil {
		t.Fatal(err)
	}
	dirs, err := f.hb.SkillDirsForAgent(f.ctx, "other")
	if err != nil || len(dirs) != 1 {
		t.Fatalf("dirs = %v err=%v", dirs, err)
	}
	b, err := os.ReadFile(filepath.Join(dirs[0], "SKILL.md"))
	if err != nil || !strings.Contains(string(b), "暂无 handbook") {
		t.Fatalf("code-map = %s err=%v", b, err)
	}
}
