package data

import (
	"context"
	"errors"
	"fmt"
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
