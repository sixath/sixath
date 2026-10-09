package data

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"backend/internal/biz"
	"backend/internal/data/model"

	"github.com/go-kratos/kratos/v2/log"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func openRepoRegistryTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&model.Repository{}, &model.RepoGroup{}, &model.RepoGroupMember{},
		&model.AgentRepoBinding{}, &model.AgentEffectiveRepo{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

func newRepoRegistryRepoForTest(t *testing.T) biz.RepoRegistryRepo {
	return NewRepoRegistryRepo(&Data{db: openRepoRegistryTestDB(t)}, log.DefaultLogger)
}

func TestRepoRegistryRepo_UpsertKeepsUserFieldsAndArchived(t *testing.T) {
	ctx := context.Background()
	r := newRepoRegistryRepoForTest(t)
	now := time.Now()
	a, err := r.UpsertScannedRepository(ctx, &biz.Repository{CodeRoot: "/c", RelPath: "cg/a", Name: "a", HeadCommit: "111", LastScannedAt: &now})
	if err != nil {
		t.Fatal(err)
	}
	desc := "gateway"
	if _, err := r.UpdateRepositoryMeta(ctx, a.ID, biz.RepoMetaPatch{Description: &desc, Tags: &[]string{"go"}}); err != nil {
		t.Fatal(err)
	}
	b, err := r.UpsertScannedRepository(ctx, &biz.Repository{CodeRoot: "/c", RelPath: "cg/a", Name: "ignored", HeadCommit: "222", LastScannedAt: &now})
	if err != nil {
		t.Fatal(err)
	}
	if b.ID != a.ID || b.HeadCommit != "222" || b.Description != "gateway" || b.Name != "a" || !reflect.DeepEqual(b.Tags, []string{"go"}) {
		t.Fatalf("after rescan: %#v", b)
	}
	if err := r.SetRepositoryStatus(ctx, a.ID, biz.RepoStatusArchived); err != nil {
		t.Fatal(err)
	}
	c, _ := r.UpsertScannedRepository(ctx, &biz.Repository{CodeRoot: "/c", RelPath: "cg/a", LastScannedAt: &now})
	if c.Status != biz.RepoStatusArchived {
		t.Fatalf("archived must stay archived, got %s", c.Status)
	}
}

func TestRepoRegistryRepo_ListFilters(t *testing.T) {
	ctx := context.Background()
	r := newRepoRegistryRepoForTest(t)
	a, _ := r.UpsertScannedRepository(ctx, &biz.Repository{CodeRoot: "/c", RelPath: "cg/a", Name: "a"})
	_, _ = r.UpsertScannedRepository(ctx, &biz.Repository{CodeRoot: "/c", RelPath: "solo", Name: "solo"})
	g, _ := r.UpsertDirGroup(ctx, "/c", "cg")
	if err := r.ReplaceGroupMembers(ctx, g.ID, biz.RepoMemberSourceRule, []string{a.ID}); err != nil {
		t.Fatal(err)
	}
	got, _ := r.ListRepositories(ctx, biz.RepoFilter{GroupID: g.ID})
	if len(got) != 1 || got[0].ID != a.ID {
		t.Fatalf("group filter: %#v", got)
	}
	got, _ = r.ListRepositories(ctx, biz.RepoFilter{Query: "sol"})
	if len(got) != 1 || got[0].RelPath != "solo" {
		t.Fatalf("query filter: %#v", got)
	}
}

func TestRepoRegistryRepo_DirGroupIDStable(t *testing.T) {
	ctx := context.Background()
	r := newRepoRegistryRepoForTest(t)
	g1, err := r.UpsertDirGroup(ctx, "/c", "cg")
	if err != nil {
		t.Fatal(err)
	}
	g2, _ := r.UpsertDirGroup(ctx, "/c", "cg")
	if g1.ID != g2.ID || g1.Kind != biz.RepoGroupDir || g1.Rule.RelPrefix != "cg" || g1.Name != "cg" {
		t.Fatalf("g1 = %#v, g2 = %#v", g1, g2)
	}
}

func TestRepoRegistryRepo_BindingsAndEffective(t *testing.T) {
	ctx := context.Background()
	r := newRepoRegistryRepoForTest(t)
	bs := []*biz.AgentRepoBinding{
		{AgentID: "ag", TargetKind: biz.RepoTargetGroup, TargetID: "g1", Mode: biz.RepoBindingInclude},
		{AgentID: "ag", TargetKind: biz.RepoTargetRepo, TargetID: "r1", Mode: biz.RepoBindingInclude, SubPaths: []string{"x"}},
	}
	if err := r.ReplaceAgentBindings(ctx, "ag", bs); err != nil {
		t.Fatal(err)
	}
	got, _ := r.ListAgentBindings(ctx, "ag")
	if len(got) != 2 || got[0].TargetKind != biz.RepoTargetRepo || !reflect.DeepEqual(got[0].SubPaths, []string{"x"}) {
		t.Fatalf("bindings = %#v", got)
	}
	ids, _ := r.ListAgentIDsBoundToGroup(ctx, "g1")
	if !reflect.DeepEqual(ids, []string{"ag"}) {
		t.Fatalf("bound to group = %v", ids)
	}
	if err := r.ReplaceAgentBindings(ctx, "ag", nil); err != nil {
		t.Fatal(err)
	}
	if got, _ := r.ListAgentBindings(ctx, "ag"); len(got) != 0 {
		t.Fatalf("replace with nil must clear, got %#v", got)
	}

	now := time.Now()
	rows := []*biz.AgentEffectiveRepo{{AgentID: "ag", RepoID: "r1", Via: []biz.BindingRef{{Kind: "repo", ID: "r1"}}, ComputedAt: now}}
	if err := r.ReplaceEffectiveRepos(ctx, "ag", rows); err != nil {
		t.Fatal(err)
	}
	eff, _ := r.ListEffectiveRepos(ctx, "ag")
	if len(eff) != 1 || eff[0].Via[0].ID != "r1" {
		t.Fatalf("effective = %#v", eff)
	}
	agents, _ := r.ListAgentIDsByEffectiveRepo(ctx, "r1")
	if !reflect.DeepEqual(agents, []string{"ag"}) {
		t.Fatalf("agents by repo = %v", agents)
	}
}

func TestRepoRegistryRepo_DeleteGroupCascades(t *testing.T) {
	ctx := context.Background()
	r := newRepoRegistryRepoForTest(t)
	g, _ := r.CreateGroup(ctx, &biz.RepoGroup{Name: "m", Kind: biz.RepoGroupManual})
	_ = r.ReplaceGroupMembers(ctx, g.ID, biz.RepoMemberSourceManual, []string{"r1"})
	_ = r.ReplaceAgentBindings(ctx, "ag", []*biz.AgentRepoBinding{{AgentID: "ag", TargetKind: biz.RepoTargetGroup, TargetID: g.ID, Mode: biz.RepoBindingInclude}})
	if err := r.DeleteGroup(ctx, g.ID); err != nil {
		t.Fatal(err)
	}
	if bs, _ := r.ListAgentBindings(ctx, "ag"); len(bs) != 0 {
		t.Fatalf("bindings to deleted group must be removed: %#v", bs)
	}
	if m, _ := r.ListActiveGroupMembers(ctx, []string{g.ID}); len(m[g.ID]) != 0 {
		t.Fatalf("members must be removed: %#v", m)
	}
}

func TestRepoRegistryRepo_UpsertRestoresMissing(t *testing.T) {
	ctx := context.Background()
	r := newRepoRegistryRepoForTest(t)
	a, err := r.UpsertScannedRepository(ctx, &biz.Repository{CodeRoot: "/c", RelPath: "a", Name: "a"})
	if err != nil {
		t.Fatal(err)
	}
	if err := r.SetRepositoryStatus(ctx, a.ID, biz.RepoStatusMissing); err != nil {
		t.Fatal(err)
	}
	b, err := r.UpsertScannedRepository(ctx, &biz.Repository{CodeRoot: "/c", RelPath: "a"})
	if err != nil {
		t.Fatal(err)
	}
	if b.ID != a.ID || b.Status != biz.RepoStatusActive {
		t.Fatalf("missing must be restored to active: %#v", b)
	}
}

func TestRepoRegistryRepo_UpsertDirGroupKeepsExisting(t *testing.T) {
	ctx := context.Background()
	db := openRepoRegistryTestDB(t)
	r := NewRepoRegistryRepo(&Data{db: db}, log.DefaultLogger)
	g, err := r.UpsertDirGroup(ctx, "/c", "cg")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.RepoGroup{}).Where("id = ?", g.ID).Update("name", "renamed").Error; err != nil {
		t.Fatal(err)
	}
	g2, err := r.UpsertDirGroup(ctx, "/c", "cg")
	if err != nil {
		t.Fatal(err)
	}
	if g2.ID != g.ID || g2.Name != "renamed" {
		t.Fatalf("existing dir group must not be overwritten: %#v", g2)
	}
}

func TestRepoRegistryRepo_NotFound(t *testing.T) {
	ctx := context.Background()
	r := newRepoRegistryRepoForTest(t)
	if err := r.SetRepositoryStatus(ctx, "nope", biz.RepoStatusArchived); !errors.Is(err, biz.ErrRepoNotFound) {
		t.Fatalf("SetRepositoryStatus err = %v", err)
	}
	if err := r.DeleteGroup(ctx, "nope"); !errors.Is(err, biz.ErrRepoNotFound) {
		t.Fatalf("DeleteGroup err = %v", err)
	}
	desc := "x"
	if _, err := r.UpdateRepositoryMeta(ctx, "nope", biz.RepoMetaPatch{Description: &desc}); !errors.Is(err, biz.ErrRepoNotFound) {
		t.Fatalf("UpdateRepositoryMeta err = %v", err)
	}
}

func TestRepoRegistryRepo_ReplaceGroupMembersDedupes(t *testing.T) {
	ctx := context.Background()
	r := newRepoRegistryRepoForTest(t)
	if err := r.ReplaceGroupMembers(ctx, "g", biz.RepoMemberSourceManual, []string{"r2", "", "r1", "r2"}); err != nil {
		t.Fatal(err)
	}
	m, err := r.ListActiveGroupMembers(ctx, []string{"g"})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(m["g"], []string{"r1", "r2"}) {
		t.Fatalf("members = %v", m["g"])
	}
}

func TestRepoRegistryRepo_ReplaceEffectiveReposBatches(t *testing.T) {
	ctx := context.Background()
	r := newRepoRegistryRepoForTest(t)
	now := time.Now()
	rows := make([]*biz.AgentEffectiveRepo, 0, 1200)
	for i := 0; i < 1200; i++ {
		id := fmt.Sprintf("r%04d", i)
		rows = append(rows, &biz.AgentEffectiveRepo{AgentID: "ag", RepoID: id, Via: []biz.BindingRef{{Kind: "repo", ID: id}}, ComputedAt: now})
	}
	if err := r.ReplaceEffectiveRepos(ctx, "ag", rows); err != nil {
		t.Fatal(err)
	}
	eff, err := r.ListEffectiveRepos(ctx, "ag")
	if err != nil {
		t.Fatal(err)
	}
	if len(eff) != 1200 {
		t.Fatalf("effective rows = %d, want 1200", len(eff))
	}
}

type stubAgentRepo struct {
	biz.AgentRepo
	agents []*biz.AgentMeta
}

// List mirrors agentRepo.List paging, including clamping out-of-range page sizes to 10.
func (s *stubAgentRepo) List(_ context.Context, page, pageSize int32) ([]*biz.AgentMeta, int, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 10
	}
	start := int((page - 1) * pageSize)
	if start >= len(s.agents) {
		return nil, len(s.agents), nil
	}
	end := start + int(pageSize)
	if end > len(s.agents) {
		end = len(s.agents)
	}
	return s.agents[start:end], len(s.agents), nil
}

func (s *stubAgentRepo) GetByID(_ context.Context, id string) (*biz.AgentMeta, error) {
	for _, a := range s.agents {
		if a.ID == id {
			return a, nil
		}
	}
	return nil, ErrNotFound
}

func mkRepoDir(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".git", "HEAD"), []byte("ref: refs/heads/main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func newUsecaseForTest(t *testing.T, codeRoot string, agents ...*biz.AgentMeta) (*biz.RepoRegistryUsecase, biz.RepoRegistryRepo) {
	repo := newRepoRegistryRepoForTest(t)
	uc := biz.NewRepoRegistryUsecase(repo, &stubAgentRepo{agents: agents}, []string{codeRoot}, log.DefaultLogger)
	return uc, repo
}

func TestRepoRegistryUsecase_ScanBindAndRoots(t *testing.T) {
	ctx := context.Background()
	codeRoot := t.TempDir()
	mkRepoDir(t, filepath.Join(codeRoot, "cloudgame", "gateway"))
	mkRepoDir(t, filepath.Join(codeRoot, "cloudgame", "svc-a"))
	mkRepoDir(t, filepath.Join(codeRoot, "migu", "gateway"))
	uc, _ := newUsecaseForTest(t, codeRoot, &biz.AgentMeta{ID: "ag"})

	rep, err := uc.Scan(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Found != 3 || rep.Added != 3 {
		t.Fatalf("report = %#v", rep)
	}
	groups, err := uc.ListGroups(ctx, biz.RepoGroupDir)
	if err != nil {
		t.Fatal(err)
	}
	var cg *biz.RepoGroupView
	for _, g := range groups {
		if g.Name == "cloudgame" {
			cg = g
		}
	}
	if cg == nil || len(cg.RepoIDs) != 2 {
		t.Fatalf("cloudgame dir group = %#v", cg)
	}
	repos, _ := uc.ListRepos(ctx, biz.RepoFilter{Query: "migu/gateway"})
	if len(repos) != 1 {
		t.Fatalf("migu repos = %#v", repos)
	}

	_, err = uc.ReplaceBindings(ctx, "ag", []*biz.AgentRepoBinding{
		{TargetKind: biz.RepoTargetGroup, TargetID: cg.ID},
		{TargetKind: biz.RepoTargetRepo, TargetID: repos[0].ID},
	}, "alice")
	if err != nil {
		t.Fatal(err)
	}
	roots, err := uc.RCARootsForAgent(ctx, "ag")
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]string{}
	for _, r := range roots {
		names[r.Name] = r.Path
	}
	if len(roots) != 3 || names["migu/gateway"] != filepath.Join(codeRoot, "migu", "gateway") || names["cloudgame/gateway"] == "" {
		t.Fatalf("roots = %#v", roots)
	}

	// a repo disappearing drops out of the effective set after rescan
	if err := os.RemoveAll(filepath.Join(codeRoot, "cloudgame", "svc-a")); err != nil {
		t.Fatal(err)
	}
	rep, _ = uc.Scan(ctx)
	if rep.Missing != 1 {
		t.Fatalf("report = %#v", rep)
	}
	roots, _ = uc.RCARootsForAgent(ctx, "ag")
	if len(roots) != 2 {
		t.Fatalf("after missing: %#v", roots)
	}
}

func TestRepoRegistryUsecase_ReplaceBindingsRejectsUnknownTarget(t *testing.T) {
	uc, _ := newUsecaseForTest(t, t.TempDir(), &biz.AgentMeta{ID: "ag"})
	_, err := uc.ReplaceBindings(context.Background(), "ag", []*biz.AgentRepoBinding{{TargetKind: biz.RepoTargetRepo, TargetID: "nope"}}, "")
	if !errors.Is(err, biz.ErrInvalidRepoBinding) {
		t.Fatalf("err = %v", err)
	}
}

func TestRepoRegistryUsecase_NoBindingsMeansNoRoots(t *testing.T) {
	uc, _ := newUsecaseForTest(t, t.TempDir())
	roots, err := uc.RCARootsForAgent(context.Background(), "ag")
	if err != nil || roots != nil {
		t.Fatalf("roots = %#v, err = %v", roots, err)
	}
}

func TestRepoRegistryUsecase_MigrateLegacyLinks(t *testing.T) {
	ctx := context.Background()
	codeRoot := t.TempDir()
	mkRepoDir(t, filepath.Join(codeRoot, "cloudgame", "svc-a"))
	mkRepoDir(t, filepath.Join(codeRoot, "solo"))

	mkLinkedWorkspace := func(target string) string {
		ws := t.TempDir()
		if err := os.Symlink(target, filepath.Join(ws, "code")); err != nil {
			t.Skipf("symlink not permitted: %v", err)
		}
		return ws
	}
	agents := []*biz.AgentMeta{
		{ID: "a-repo", Workspace: mkLinkedWorkspace(filepath.Join(codeRoot, "solo"))},
		{ID: "a-group", Workspace: mkLinkedWorkspace(filepath.Join(codeRoot, "cloudgame"))},
		{ID: "a-multi", Workspace: mkLinkedWorkspace(codeRoot)},
		{ID: "a-none", Workspace: t.TempDir()},
	}
	uc, repo := newUsecaseForTest(t, codeRoot, agents...)
	if _, err := uc.Scan(ctx); err != nil {
		t.Fatal(err)
	}

	items, err := uc.MigrateLegacyLinks(ctx, false)
	if err != nil {
		t.Fatal(err)
	}
	actions := map[string]string{}
	for _, it := range items {
		actions[it.AgentID] = it.Action
		if it.Applied {
			t.Fatalf("dry run must not apply: %#v", it)
		}
	}
	if actions["a-repo"] != biz.LegacyActionBindRepo || actions["a-group"] != biz.LegacyActionBindGroup ||
		actions["a-multi"] != biz.LegacyActionManualMulti {
		t.Fatalf("actions = %v", actions)
	}
	if _, ok := actions["a-none"]; ok {
		t.Fatal("agents without workspace/code are not reported")
	}

	if _, err := uc.MigrateLegacyLinks(ctx, true); err != nil {
		t.Fatal(err)
	}
	if bs, _ := repo.ListAgentBindings(ctx, "a-repo"); len(bs) != 1 {
		t.Fatalf("a-repo bindings = %#v", bs)
	}
	if bs, _ := repo.ListAgentBindings(ctx, "a-multi"); len(bs) != 0 {
		t.Fatalf("manual cases must not be applied: %#v", bs)
	}
	items, _ = uc.MigrateLegacyLinks(ctx, true)
	for _, it := range items {
		if it.AgentID == "a-repo" && it.Action != biz.LegacyActionSkipHasBindings {
			t.Fatalf("second run must skip bound agents: %#v", it)
		}
	}
}

func TestRepoRegistryRepo_MarkMissingIfActive(t *testing.T) {
	ctx := context.Background()
	r := newRepoRegistryRepoForTest(t)
	a, _ := r.UpsertScannedRepository(ctx, &biz.Repository{CodeRoot: "/c", RelPath: "a", Name: "a"})
	b, _ := r.UpsertScannedRepository(ctx, &biz.Repository{CodeRoot: "/c", RelPath: "b", Name: "b"})
	if err := r.SetRepositoryStatus(ctx, b.ID, biz.RepoStatusArchived); err != nil {
		t.Fatal(err)
	}
	if ok, err := r.MarkRepositoryMissingIfActive(ctx, a.ID); err != nil || !ok {
		t.Fatalf("active: ok=%v err=%v", ok, err)
	}
	if ok, err := r.MarkRepositoryMissingIfActive(ctx, a.ID); err != nil || ok {
		t.Fatalf("already missing: ok=%v err=%v", ok, err)
	}
	if ok, err := r.MarkRepositoryMissingIfActive(ctx, b.ID); err != nil || ok {
		t.Fatalf("archived: ok=%v err=%v", ok, err)
	}
	m, _ := r.GetRepositoriesByIDs(ctx, []string{a.ID, b.ID})
	if m[a.ID].Status != biz.RepoStatusMissing || m[b.ID].Status != biz.RepoStatusArchived {
		t.Fatalf("statuses = %s, %s", m[a.ID].Status, m[b.ID].Status)
	}
}

func TestRepoRegistryRepo_RefreshKeepsArchivedOnStaleRead(t *testing.T) {
	ctx := context.Background()
	db := openRepoRegistryTestDB(t)
	r := NewRepoRegistryRepo(&Data{db: db}, log.DefaultLogger)
	a, _ := r.UpsertScannedRepository(ctx, &biz.Repository{CodeRoot: "/c", RelPath: "a", Name: "a"})
	var stale model.Repository
	if err := db.Where("id = ?", a.ID).Take(&stale).Error; err != nil {
		t.Fatal(err)
	}
	if err := r.SetRepositoryStatus(ctx, a.ID, biz.RepoStatusArchived); err != nil {
		t.Fatal(err)
	}
	if err := refreshScannedRepository(db, &stale, &biz.Repository{HeadCommit: "abc"}); err != nil {
		t.Fatal(err)
	}
	if stale.Status != biz.RepoStatusArchived || stale.HeadCommit != "abc" {
		t.Fatalf("after refresh: status=%s head=%s", stale.Status, stale.HeadCommit)
	}
}

func TestRepoRegistryUsecase_MigratePaginatesWithoutSymlinks(t *testing.T) {
	ctx := context.Background()
	codeRoot := t.TempDir()
	// workspace/code as a plain directory inside the code root resolves without symlinks.
	mkRepoDir(t, filepath.Join(codeRoot, "ws-repo", "code"))
	mkRepoDir(t, filepath.Join(codeRoot, "ws-group", "code", "r1"))
	mkRepoDir(t, filepath.Join(codeRoot, "ws-group", "code", "r2"))
	mkRepoDir(t, filepath.Join(codeRoot, "ws-multi", "code", "x", "r1"))
	mkRepoDir(t, filepath.Join(codeRoot, "ws-multi", "code", "y", "r2"))
	outside := t.TempDir()
	if err := os.MkdirAll(filepath.Join(outside, "code"), 0o755); err != nil {
		t.Fatal(err)
	}

	agents := []*biz.AgentMeta{
		{ID: "a-repo", Workspace: filepath.Join(codeRoot, "ws-repo")},
		{ID: "a-group", Workspace: filepath.Join(codeRoot, "ws-group")},
		{ID: "a-multi", Workspace: filepath.Join(codeRoot, "ws-multi")},
		{ID: "a-none", Workspace: t.TempDir()},
	}
	for i := 0; i < 120; i++ {
		agents = append(agents, &biz.AgentMeta{ID: fmt.Sprintf("a-out-%03d", i), Workspace: outside})
	}
	// the exact-match agents sit on the last page
	agents = append(agents[4:], agents[:4]...)
	uc, repo := newUsecaseForTest(t, codeRoot, agents...)
	if _, err := uc.Scan(ctx); err != nil {
		t.Fatal(err)
	}

	items, err := uc.MigrateLegacyLinks(ctx, false)
	if err != nil {
		t.Fatal(err)
	}
	actions := map[string]string{}
	for _, it := range items {
		actions[it.AgentID] = it.Action
		if it.Applied {
			t.Fatalf("dry run must not apply: %#v", it)
		}
	}
	if len(items) != len(agents)-1 {
		t.Fatalf("reported %d agents, want %d", len(items), len(agents)-1)
	}
	if actions["a-repo"] != biz.LegacyActionBindRepo || actions["a-group"] != biz.LegacyActionBindGroup ||
		actions["a-multi"] != biz.LegacyActionManualMulti || actions["a-out-119"] != biz.LegacyActionUnresolved {
		t.Fatalf("actions = %v", actions)
	}
	if _, ok := actions["a-none"]; ok {
		t.Fatal("agents without workspace/code are not reported")
	}
	for _, a := range agents {
		if bs, _ := repo.ListAgentBindings(ctx, a.ID); len(bs) != 0 {
			t.Fatalf("dry run wrote bindings for %s: %#v", a.ID, bs)
		}
	}

	items, err = uc.MigrateLegacyLinks(ctx, true)
	if err != nil {
		t.Fatal(err)
	}
	applied := 0
	for _, it := range items {
		if it.Applied {
			applied++
		}
	}
	if applied != 2 {
		t.Fatalf("applied = %d, want 2", applied)
	}
	if bs, _ := repo.ListAgentBindings(ctx, "a-group"); len(bs) != 1 || bs[0].TargetKind != biz.RepoTargetGroup {
		t.Fatalf("a-group bindings = %#v", bs)
	}
	if bs, _ := repo.ListAgentBindings(ctx, "a-multi"); len(bs) != 0 {
		t.Fatalf("manual cases must not be applied: %#v", bs)
	}
	roots, err := uc.RCARootsForAgent(ctx, "a-repo")
	if err != nil || len(roots) != 1 || roots[0].Name != "ws-repo/code" {
		t.Fatalf("a-repo roots = %#v, err = %v", roots, err)
	}
}

func TestRepoRegistryUsecase_GitInfoFailureKeepsMetadata(t *testing.T) {
	ctx := context.Background()
	codeRoot := t.TempDir()
	dir := filepath.Join(codeRoot, "svc")
	mkRepoDir(t, dir)
	sha := "0123456789abcdef0123456789abcdef01234567"
	if err := os.MkdirAll(filepath.Join(dir, ".git", "refs", "heads"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".git", "refs", "heads", "main"), []byte(sha+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	uc, _ := newUsecaseForTest(t, codeRoot)
	if _, err := uc.Scan(ctx); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".git", "HEAD"), []byte("garbage\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := uc.Scan(ctx); err != nil {
		t.Fatal(err)
	}
	repos, _ := uc.ListRepos(ctx, biz.RepoFilter{})
	if len(repos) != 1 || repos[0].GitBranch != "main" || repos[0].HeadCommit != sha {
		t.Fatalf("repos = %#v", repos)
	}
}

func TestRepoRegistryUsecase_ScanKeepsArchived(t *testing.T) {
	ctx := context.Background()
	codeRoot := t.TempDir()
	mkRepoDir(t, filepath.Join(codeRoot, "keep"))
	mkRepoDir(t, filepath.Join(codeRoot, "gone"))
	uc, _ := newUsecaseForTest(t, codeRoot)
	if _, err := uc.Scan(ctx); err != nil {
		t.Fatal(err)
	}
	archived := biz.RepoStatusArchived
	for _, r := range mustListRepos(t, uc) {
		if _, err := uc.PatchRepo(ctx, r.ID, biz.RepoMetaPatch{Status: &archived}); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.RemoveAll(filepath.Join(codeRoot, "gone")); err != nil {
		t.Fatal(err)
	}
	rep, err := uc.Scan(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Missing != 0 {
		t.Fatalf("report = %#v", rep)
	}
	for _, r := range mustListRepos(t, uc) {
		if r.Status != biz.RepoStatusArchived {
			t.Fatalf("%s status = %s, want archived", r.RelPath, r.Status)
		}
	}
}

func mustListRepos(t *testing.T, uc *biz.RepoRegistryUsecase) []*biz.Repository {
	t.Helper()
	repos, err := uc.ListRepos(context.Background(), biz.RepoFilter{})
	if err != nil {
		t.Fatal(err)
	}
	return repos
}

func TestRepoRegistryUsecase_BoundButEmptyIsNonNil(t *testing.T) {
	ctx := context.Background()
	codeRoot := t.TempDir()
	mkRepoDir(t, filepath.Join(codeRoot, "svc"))
	uc, _ := newUsecaseForTest(t, codeRoot, &biz.AgentMeta{ID: "ag"})
	if _, err := uc.Scan(ctx); err != nil {
		t.Fatal(err)
	}
	repos := mustListRepos(t, uc)
	if _, err := uc.ReplaceBindings(ctx, "ag", []*biz.AgentRepoBinding{{TargetKind: biz.RepoTargetRepo, TargetID: repos[0].ID}}, ""); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(filepath.Join(codeRoot, "svc")); err != nil {
		t.Fatal(err)
	}
	if _, err := uc.Scan(ctx); err != nil {
		t.Fatal(err)
	}
	roots, err := uc.RCARootsForAgent(ctx, "ag")
	if err != nil || roots == nil || len(roots) != 0 {
		t.Fatalf("roots = %#v, err = %v", roots, err)
	}
}

func TestRepoRegistryUsecase_RemovedCodeRoot(t *testing.T) {
	ctx := context.Background()
	rootA, rootB := t.TempDir(), t.TempDir()
	mkRepoDir(t, filepath.Join(rootA, "svc"))
	agents := &stubAgentRepo{agents: []*biz.AgentMeta{{ID: "ag"}}}
	repo := newRepoRegistryRepoForTest(t)
	ucA := biz.NewRepoRegistryUsecase(repo, agents, []string{rootA}, log.DefaultLogger)
	if _, err := ucA.Scan(ctx); err != nil {
		t.Fatal(err)
	}
	repos := mustListRepos(t, ucA)
	if _, err := ucA.ReplaceBindings(ctx, "ag", []*biz.AgentRepoBinding{{TargetKind: biz.RepoTargetRepo, TargetID: repos[0].ID}}, ""); err != nil {
		t.Fatal(err)
	}

	ucB := biz.NewRepoRegistryUsecase(repo, agents, []string{rootB}, log.DefaultLogger)
	roots, err := ucB.RCARootsForAgent(ctx, "ag")
	if err != nil || roots == nil || len(roots) != 0 {
		t.Fatalf("roots under unconfigured code root = %#v, err = %v", roots, err)
	}
	rep, err := ucB.Scan(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Missing != 1 {
		t.Fatalf("report = %#v", rep)
	}
	if r := mustListRepos(t, ucB)[0]; r.Status != biz.RepoStatusMissing {
		t.Fatalf("status = %s", r.Status)
	}
}

func TestRepoRegistryUsecase_RootsDropVanishedAndEscapingPaths(t *testing.T) {
	ctx := context.Background()
	codeRoot := t.TempDir()
	dir := filepath.Join(codeRoot, "svc")
	mkRepoDir(t, dir)
	if err := os.MkdirAll(filepath.Join(dir, "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	subs := []string{"src", "nope"}
	escaped := os.Symlink(t.TempDir(), filepath.Join(dir, "esc")) == nil
	if escaped {
		subs = append(subs, "esc")
	}
	uc, _ := newUsecaseForTest(t, codeRoot, &biz.AgentMeta{ID: "ag"})
	if _, err := uc.Scan(ctx); err != nil {
		t.Fatal(err)
	}
	repos := mustListRepos(t, uc)
	if _, err := uc.ReplaceBindings(ctx, "ag", []*biz.AgentRepoBinding{{TargetKind: biz.RepoTargetRepo, TargetID: repos[0].ID, SubPaths: subs}}, ""); err != nil {
		t.Fatal(err)
	}
	roots, err := uc.RCARootsForAgent(ctx, "ag")
	if err != nil || len(roots) != 1 || roots[0].Name != "svc/src" {
		t.Fatalf("roots = %#v, err = %v (symlink tested: %v)", roots, err, escaped)
	}
}

func TestRepoRegistryUsecase_ReplaceBindingsUnknownAgent(t *testing.T) {
	uc, _ := newUsecaseForTest(t, t.TempDir())
	_, err := uc.ReplaceBindings(context.Background(), "ghost", nil, "")
	if !errors.Is(err, biz.ErrAgentNotFound) {
		t.Fatalf("err = %v", err)
	}
}

func TestRepoRegistryUsecase_CopyBindingsUnknownSourceKeepsTarget(t *testing.T) {
	ctx := context.Background()
	codeRoot := t.TempDir()
	mkRepoDir(t, filepath.Join(codeRoot, "svc"))
	uc, repo := newUsecaseForTest(t, codeRoot, &biz.AgentMeta{ID: "ag"})
	if _, err := uc.Scan(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := uc.ReplaceBindings(ctx, "ag", []*biz.AgentRepoBinding{{TargetKind: biz.RepoTargetRepo, TargetID: mustListRepos(t, uc)[0].ID}}, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := uc.CopyBindings(ctx, "ghost", "ag", ""); !errors.Is(err, biz.ErrAgentNotFound) {
		t.Fatalf("err = %v", err)
	}
	if bs, _ := repo.ListAgentBindings(ctx, "ag"); len(bs) != 1 {
		t.Fatalf("target bindings wiped: %#v", bs)
	}
}

func TestRepoRegistryUsecase_RecomputeSkipsUnchanged(t *testing.T) {
	ctx := context.Background()
	codeRoot := t.TempDir()
	mkRepoDir(t, filepath.Join(codeRoot, "svc"))
	uc, repo := newUsecaseForTest(t, codeRoot, &biz.AgentMeta{ID: "ag"})
	if _, err := uc.Scan(ctx); err != nil {
		t.Fatal(err)
	}
	repos := mustListRepos(t, uc)
	if _, err := uc.ReplaceBindings(ctx, "ag", []*biz.AgentRepoBinding{{TargetKind: biz.RepoTargetRepo, TargetID: repos[0].ID}}, ""); err != nil {
		t.Fatal(err)
	}
	before, _ := repo.ListEffectiveRepos(ctx, "ag")
	time.Sleep(10 * time.Millisecond)
	if _, err := uc.RecomputeAgent(ctx, "ag"); err != nil {
		t.Fatal(err)
	}
	after, _ := repo.ListEffectiveRepos(ctx, "ag")
	if len(before) != 1 || len(after) != 1 || !after[0].ComputedAt.Equal(before[0].ComputedAt) {
		t.Fatalf("unchanged set was rewritten: before=%#v after=%#v", before, after)
	}
}

func TestRepoRegistryUsecase_PatchRepoInvalidStatus(t *testing.T) {
	ctx := context.Background()
	codeRoot := t.TempDir()
	mkRepoDir(t, filepath.Join(codeRoot, "svc"))
	uc, _ := newUsecaseForTest(t, codeRoot)
	if _, err := uc.Scan(ctx); err != nil {
		t.Fatal(err)
	}
	bad := biz.RepoStatusMissing
	_, err := uc.PatchRepo(ctx, mustListRepos(t, uc)[0].ID, biz.RepoMetaPatch{Status: &bad})
	if !errors.Is(err, biz.ErrInvalidRepo) {
		t.Fatalf("err = %v", err)
	}
}
