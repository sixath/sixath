package data

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

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

	first, ok, err := r.ClaimHandbookBuild(ctx, a.ID, now, now.Add(time.Minute))
	if err != nil || !ok || first == "" {
		t.Fatalf("first claim: token=%q ok=%v err=%v", first, ok, err)
	}
	got := reload()
	if got.HandbookStatus != biz.HandbookStatusBuilding || got.HandbookLeaseUntil == nil ||
		got.HandbookLeaseUntil.Sub(now.Add(time.Minute)).Abs() > time.Millisecond {
		t.Fatalf("after claim: %#v", got)
	}
	if _, ok, _ := r.ClaimHandbookBuild(ctx, a.ID, now, now.Add(time.Minute)); ok {
		t.Fatal("second claim must fail while the lease is live")
	}
	second, ok, _ := r.ClaimHandbookBuild(ctx, a.ID, now.Add(2*time.Minute), now.Add(3*time.Minute))
	if !ok || second == "" || second == first {
		t.Fatalf("an expired lease must be claimable with a new token: %q ok=%v", second, ok)
	}

	if err := r.FinishHandbookBuild(ctx, a.ID, first, biz.HandbookBuildResult{
		Status: biz.HandbookStatusReady, Commit: "stale", Version: 9,
	}); !errors.Is(err, biz.ErrHandbookLeaseLost) {
		t.Fatalf("finish with a superseded token: err = %v, want ErrHandbookLeaseLost", err)
	}
	if err := r.FinishHandbookBuild(ctx, a.ID, second, biz.HandbookBuildResult{
		Status: biz.HandbookStatusReady, Commit: "111", Version: 1, Stats: map[string]any{"files": 3},
	}); err != nil {
		t.Fatal(err)
	}
	g := reload()
	if g.HandbookStatus != biz.HandbookStatusReady || g.HandbookCommit != "111" || g.HandbookVersion != 1 ||
		g.HandbookStats["files"] != float64(3) || g.HandbookLeaseUntil != nil {
		t.Fatalf("after ready: %#v", g)
	}
	if err := r.FinishHandbookBuild(ctx, a.ID, second, biz.HandbookBuildResult{Status: biz.HandbookStatusFailed}); !errors.Is(err, biz.ErrHandbookLeaseLost) {
		t.Fatalf("finishing twice: err = %v, want ErrHandbookLeaseLost", err)
	}

	third, ok, _ := r.ClaimHandbookBuild(ctx, a.ID, now.Add(5*time.Minute), now.Add(6*time.Minute))
	if !ok {
		t.Fatal("a finished build must release the lease")
	}
	if err := r.FinishHandbookBuild(ctx, a.ID, third, biz.HandbookBuildResult{
		Status: biz.HandbookStatusFailed, Stats: map[string]any{"last_error": "boom"},
	}); err != nil {
		t.Fatal(err)
	}
	g = reload()
	if g.HandbookStatus != biz.HandbookStatusFailed || g.HandbookCommit != "111" || g.HandbookVersion != 1 || g.HandbookStats["last_error"] != "boom" {
		t.Fatalf("a failed build must keep commit and version: %#v", g)
	}
}

func TestRepoRegistryRepo_ReleaseHandbookBuild(t *testing.T) {
	ctx := context.Background()
	r := newRepoRegistryRepoForTest(t)
	now := time.Now()
	a, err := r.UpsertScannedRepository(ctx, &biz.Repository{CodeRoot: "/c", RelPath: "cg/a", Name: "a", HeadCommit: "111", LastScannedAt: &now})
	if err != nil {
		t.Fatal(err)
	}
	token, ok, err := r.ClaimHandbookBuild(ctx, a.ID, now, now.Add(time.Hour))
	if err != nil || !ok {
		t.Fatalf("claim: ok=%v err=%v", ok, err)
	}
	if err := r.ReleaseHandbookBuild(ctx, a.ID, "other-token", biz.HandbookStatusReady); !errors.Is(err, biz.ErrHandbookLeaseLost) {
		t.Fatalf("release with a foreign token: err = %v", err)
	}
	if err := r.ReleaseHandbookBuild(ctx, a.ID, token, biz.HandbookStatusFailed); err != nil {
		t.Fatal(err)
	}
	m, err := r.GetRepositoriesByIDs(ctx, []string{a.ID})
	if err != nil {
		t.Fatal(err)
	}
	if g := m[a.ID]; g.HandbookStatus != biz.HandbookStatusFailed || g.HandbookLeaseUntil != nil {
		t.Fatalf("after release: %#v", g)
	}
	if _, ok, _ := r.ClaimHandbookBuild(ctx, a.ID, now, now.Add(time.Hour)); !ok {
		t.Fatal("a released lease must be claimable")
	}
}

func TestRepoRegistryRepo_ClaimUnknownRepo(t *testing.T) {
	r := newRepoRegistryRepoForTest(t)
	now := time.Now()
	if _, ok, err := r.ClaimHandbookBuild(context.Background(), "nope", now, now.Add(time.Minute)); err != nil || ok {
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
	if !strings.Contains(body, `skill_view("`+handbook.SkillName("cloudgame/svc-a")+`")`) {
		t.Fatalf("code-map body:\n%s", body)
	}
	if hbMeta, ok := idx.GetByName(handbook.SkillName("cloudgame/svc-a")); !ok || !hbMeta.HiddenFromSummary {
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
	if _, ok, _ := f.repo.ClaimHandbookBuild(f.ctx, f.repoID, now, now.Add(time.Hour)); !ok {
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

func TestHandbookUsecase_NilUsecaseHasNoSkillDirs(t *testing.T) {
	var hb *biz.HandbookUsecase
	if dirs, err := hb.SkillDirsForAgent(context.Background(), "ag"); dirs != nil || err != nil {
		t.Fatalf("dirs=%v err=%v", dirs, err)
	}
}

func TestHandbookUsecase_BuildsRunUnderTimeout(t *testing.T) {
	f := newHandbookFixture(t)
	var deadlines []time.Duration
	f.hb.SetBuilder(func(ctx context.Context, in handbook.BuildInput) (*handbook.Output, error) {
		dl, ok := ctx.Deadline()
		if !ok {
			deadlines = append(deadlines, -1)
		} else {
			deadlines = append(deadlines, time.Until(dl))
		}
		return handbook.Build(ctx, in)
	})
	if _, err := f.hb.RebuildStale(f.ctx); err != nil {
		t.Fatal(err)
	}
	if err := f.hb.RequestRebuild(f.ctx, f.repoID); err != nil {
		t.Fatal(err)
	}
	f.hb.Wait()
	if len(deadlines) != 2 {
		t.Fatalf("builds = %d", len(deadlines))
	}
	for _, d := range deadlines {
		if d < 24*time.Minute || d > 25*time.Minute {
			t.Fatalf("build deadline in %v, want about 25m", d)
		}
	}
}

func TestHandbookUsecase_ExpiredBuildingLeaseIsRebuilt(t *testing.T) {
	f := newHandbookFixture(t)
	if _, err := f.hb.RebuildStale(f.ctx); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	if _, ok, _ := f.repo.ClaimHandbookBuild(f.ctx, f.repoID, now, now.Add(time.Hour)); !ok {
		t.Fatal("claim")
	}
	if n, _ := f.hb.RebuildStale(f.ctx); n != 0 {
		t.Fatalf("a live lease must not be rebuilt, n=%d", n)
	}
	past := now.Add(-2 * time.Hour)
	if _, ok, _ := f.repo.ClaimHandbookBuild(f.ctx, f.repoID, now.Add(2*time.Hour), past); !ok {
		t.Fatal("re-claim")
	}
	if r := f.get(t); r.HandbookStatus != biz.HandbookStatusBuilding || r.HandbookLeaseUntil == nil || r.HandbookLeaseUntil.After(now) {
		t.Fatalf("stuck building: %#v", r)
	}
	if n, err := f.hb.RebuildStale(f.ctx); err != nil || n != 1 {
		t.Fatalf("an expired building lease must be rebuilt, n=%d err=%v", n, err)
	}
	if r := f.get(t); r.HandbookStatus != biz.HandbookStatusReady || r.HandbookVersion != 2 || r.HandbookLeaseUntil != nil {
		t.Fatalf("after rebuild: %#v", r)
	}
}

func TestHandbookUsecase_CancelledBuildRestoresStatus(t *testing.T) {
	f := newHandbookFixture(t)
	if _, err := f.hb.RebuildStale(f.ctx); err != nil {
		t.Fatal(err)
	}
	setRepoHead(t, f.repoDir, shaOf('b'))
	if _, err := f.reg.Scan(f.ctx); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(f.ctx)
	defer cancel()
	f.hb.SetBuilder(func(bctx context.Context, _ handbook.BuildInput) (*handbook.Output, error) {
		cancel()
		<-bctx.Done()
		return nil, bctx.Err()
	})
	if _, err := f.hb.RebuildStale(ctx); err != nil && !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	r := f.get(t)
	if r.HandbookStatus != biz.HandbookStatusReady || r.HandbookVersion != 1 || r.HandbookLeaseUntil != nil ||
		r.HandbookStats["failed_commit"] != nil || r.HandbookStats["last_error"] != nil {
		t.Fatalf("a cancelled build must not be recorded as failed: %#v", r)
	}
	f.hb.SetBuilder(handbook.Build)
	if n, _ := f.hb.RebuildStale(f.ctx); n != 1 {
		t.Fatalf("a cancelled build must be retried, n=%d", n)
	}
}

func TestHandbookUsecase_BuildTimeoutIsRecordedAsFailure(t *testing.T) {
	f := newHandbookFixture(t)
	f.hb.SetBuildTimeout(50 * time.Millisecond)
	f.hb.SetBuilder(func(bctx context.Context, _ handbook.BuildInput) (*handbook.Output, error) {
		<-bctx.Done()
		return nil, bctx.Err()
	})
	if n, err := f.hb.RebuildStale(f.ctx); err != nil || n != 1 {
		t.Fatalf("n=%d err=%v", n, err)
	}
	r := f.get(t)
	msg, _ := r.HandbookStats["last_error"].(string)
	if r.HandbookStatus != biz.HandbookStatusFailed || r.HandbookStats["failed_commit"] != shaOf('a') ||
		!strings.Contains(msg, "构建超时") || r.HandbookLeaseUntil != nil {
		t.Fatalf("a timed-out build must be recorded as failed: %#v", r)
	}
	if n, _ := f.hb.RebuildStale(f.ctx); n != 0 {
		t.Fatalf("a timed-out commit must not be retried automatically, n=%d", n)
	}
}

// hookedRepo runs hooks around lease operations to simulate concurrent writers.
type hookedRepo struct {
	biz.RepoRegistryRepo
	afterClaim func()
	getErr     error
	claimed    bool
}

func (h *hookedRepo) ClaimHandbookBuild(ctx context.Context, id string, now, leaseUntil time.Time) (string, bool, error) {
	token, ok, err := h.RepoRegistryRepo.ClaimHandbookBuild(ctx, id, now, leaseUntil)
	if ok {
		h.claimed = true
		if h.afterClaim != nil {
			h.afterClaim()
		}
	}
	return token, ok, err
}

func (h *hookedRepo) GetRepositoriesByIDs(ctx context.Context, ids []string) (map[string]*biz.Repository, error) {
	if h.claimed && h.getErr != nil {
		return nil, h.getErr
	}
	return h.RepoRegistryRepo.GetRepositoriesByIDs(ctx, ids)
}

func TestHandbookUsecase_RepoArchivedAfterClaimIsSkipped(t *testing.T) {
	f := newHandbookFixture(t)
	hooked := &hookedRepo{RepoRegistryRepo: f.repo, afterClaim: func() {
		if err := f.repo.SetRepositoryStatus(f.ctx, f.repoID, biz.RepoStatusArchived); err != nil {
			t.Error(err)
		}
	}}
	hb := biz.NewHandbookUsecase(hooked, f.reg, t.TempDir(), log.DefaultLogger)
	built := false
	hb.SetBuilder(func(ctx context.Context, in handbook.BuildInput) (*handbook.Output, error) {
		built = true
		return handbook.Build(ctx, in)
	})
	if _, err := hb.RebuildStale(f.ctx); err != nil {
		t.Fatal(err)
	}
	r := f.get(t)
	if built || r.HandbookStatus != biz.HandbookStatusNone || r.HandbookLeaseUntil != nil || r.HandbookVersion != 0 {
		t.Fatalf("built=%v repo=%#v", built, r)
	}
}

func TestHandbookUsecase_RereadFailureReleasesLease(t *testing.T) {
	f := newHandbookFixture(t)
	if _, err := f.hb.RebuildStale(f.ctx); err != nil {
		t.Fatal(err)
	}
	hooked := &hookedRepo{RepoRegistryRepo: f.repo, getErr: errors.New("db down")}
	hb := biz.NewHandbookUsecase(hooked, f.reg, t.TempDir(), log.DefaultLogger)
	if err := hb.RequestRebuild(f.ctx, f.repoID); err != nil {
		t.Fatal(err)
	}
	hb.Wait()
	r := f.get(t)
	if r.HandbookStatus != biz.HandbookStatusReady || r.HandbookLeaseUntil != nil || r.HandbookVersion != 1 ||
		r.HandbookStats["generator_version"] != handbook.GeneratorVersion || r.HandbookStats["last_error"] != nil {
		t.Fatalf("a failed re-read must release without touching stats: %#v", r)
	}
}

func TestHandbookUsecase_LostLeaseDropsResult(t *testing.T) {
	f := newHandbookFixture(t)
	var thief string
	f.hb.SetBuilder(func(ctx context.Context, in handbook.BuildInput) (*handbook.Output, error) {
		later := time.Now().Add(2 * time.Hour)
		token, ok, err := f.repo.ClaimHandbookBuild(ctx, f.repoID, later, later.Add(time.Hour))
		if err != nil || !ok {
			t.Errorf("steal: ok=%v err=%v", ok, err)
		}
		thief = token
		return handbook.Build(ctx, in)
	})
	if _, err := f.hb.RebuildStale(f.ctx); err != nil {
		t.Fatal(err)
	}
	r := f.get(t)
	if r.HandbookStatus != biz.HandbookStatusBuilding || r.HandbookVersion != 0 || r.HandbookLeaseUntil == nil {
		t.Fatalf("a build that lost its lease must not write its result: %#v", r)
	}
	if err := f.repo.FinishHandbookBuild(f.ctx, f.repoID, thief, biz.HandbookBuildResult{Status: biz.HandbookStatusFailed}); err != nil {
		t.Fatalf("the new lease holder must still own the row: %v", err)
	}
}

func TestHandbookUsecase_GlobalBuildConcurrency(t *testing.T) {
	f := newHandbookFixture(t)
	mkRepoDir(t, filepath.Join(f.codeRoot, "cloudgame", "svc-c"))
	if _, err := f.reg.Scan(f.ctx); err != nil {
		t.Fatal(err)
	}
	all, err := f.reg.ListRepos(f.ctx, biz.RepoFilter{})
	if err != nil || len(all) != 3 {
		t.Fatalf("repos = %d err=%v", len(all), err)
	}
	var others []string
	for _, r := range all {
		if r.ID != f.repoID {
			others = append(others, r.ID)
		}
	}
	release := make(chan struct{})
	f.hb.SetBuilder(func(ctx context.Context, in handbook.BuildInput) (*handbook.Output, error) {
		<-release
		return handbook.Build(ctx, in)
	})
	for _, id := range others {
		if err := f.hb.RequestRebuild(f.ctx, id); err != nil {
			t.Fatal(err)
		}
	}
	if err := f.hb.RequestRebuild(f.ctx, f.repoID); !errors.Is(err, biz.ErrHandbookBuilding) {
		t.Fatalf("third concurrent build: err = %v, want ErrHandbookBuilding", err)
	}
	if r := f.get(t); r.HandbookStatus == biz.HandbookStatusBuilding {
		t.Fatal("a rejected request must not claim the lease")
	}
	ctx, cancel := context.WithTimeout(f.ctx, 50*time.Millisecond)
	defer cancel()
	if n, err := f.hb.RebuildStale(ctx); n != 0 || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("RebuildStale must wait for a build slot: n=%d err=%v", n, err)
	}
	close(release)
	f.hb.Wait()
	if err := f.hb.RequestRebuild(f.ctx, f.repoID); err != nil {
		t.Fatal(err)
	}
	f.hb.Wait()
	if r := f.get(t); r.HandbookStatus != biz.HandbookStatusReady {
		t.Fatalf("after slots freed: %#v", r)
	}
}

func TestHandbookUsecase_ErrorTruncationIsRuneSafe(t *testing.T) {
	f := newHandbookFixture(t)
	f.hb.SetBuilder(func(context.Context, handbook.BuildInput) (*handbook.Output, error) {
		return nil, errors.New(strings.Repeat("错", 600))
	})
	if _, err := f.hb.RebuildStale(f.ctx); err != nil {
		t.Fatal(err)
	}
	msg, _ := f.get(t).HandbookStats["last_error"].(string)
	if !utf8.ValidString(msg) || strings.ContainsRune(msg, utf8.RuneError) || utf8.RuneCountInString(msg) != 500 {
		t.Fatalf("last_error has %d runes, valid=%v", utf8.RuneCountInString(msg), utf8.ValidString(msg))
	}
}
