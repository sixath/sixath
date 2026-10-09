package biz

import (
	"errors"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/sixath/framework/tool"
)

func repoFixture(id, root, rel, status string) *Repository {
	return &Repository{ID: id, CodeRoot: root, RelPath: rel, Status: status}
}

func TestNormalizeRepoBindings(t *testing.T) {
	in := []*AgentRepoBinding{
		{TargetKind: RepoTargetGroup, TargetID: "g1"},
		{TargetKind: RepoTargetRepo, TargetID: "r1", SubPaths: []string{"/svc/x/", "svc/x", "a"}},
		{TargetKind: RepoTargetRepo, TargetID: "r2", Mode: RepoBindingExclude},
	}
	got, err := normalizeRepoBindings("agent-1", "alice", in)
	if err != nil {
		t.Fatal(err)
	}
	if got[0].Mode != RepoBindingInclude || got[0].AgentID != "agent-1" || got[0].CreatedBy != "alice" {
		t.Fatalf("got[0] = %#v", got[0])
	}
	if !reflect.DeepEqual(got[1].SubPaths, []string{"a", "svc/x"}) {
		t.Fatalf("sub_paths = %v", got[1].SubPaths)
	}

	bad := [][]*AgentRepoBinding{
		{{TargetKind: "repo_selector", TargetID: "x"}},
		{{TargetKind: RepoTargetRepo, TargetID: ""}},
		{{TargetKind: RepoTargetGroup, TargetID: "g", Mode: RepoBindingExclude}},
		{{TargetKind: RepoTargetGroup, TargetID: "g", SubPaths: []string{"a"}}},
		{{TargetKind: RepoTargetRepo, TargetID: "r", SubPaths: []string{"../etc"}}},
		{{TargetKind: RepoTargetRepo, TargetID: "r", SubPaths: []string{"svc/.GIT/config"}}},
		{{TargetKind: RepoTargetRepo, TargetID: "r", Mode: "weird"}},
		{{TargetKind: RepoTargetRepo, TargetID: "r"}, {TargetKind: RepoTargetRepo, TargetID: "r", Mode: RepoBindingExclude}},
	}
	for i, b := range bad {
		if _, err := normalizeRepoBindings("a", "", b); !errors.Is(err, ErrInvalidRepoBinding) {
			t.Fatalf("case %d: err = %v, want ErrInvalidRepoBinding", i, err)
		}
	}
}

func TestExpandRepoBindings(t *testing.T) {
	repos := map[string]*Repository{
		"r1": repoFixture("r1", "/c", "cg/a", RepoStatusActive),
		"r2": repoFixture("r2", "/c", "cg/b", RepoStatusActive),
		"r3": repoFixture("r3", "/c", "cg/c", RepoStatusMissing),
		"r4": repoFixture("r4", "/c", "solo", RepoStatusActive),
	}
	members := map[string][]string{"g1": {"r1", "r2", "r3"}}
	bs := []*AgentRepoBinding{
		{TargetKind: RepoTargetGroup, TargetID: "g1", Mode: RepoBindingInclude},
		{TargetKind: RepoTargetRepo, TargetID: "r2", Mode: RepoBindingExclude},
		{TargetKind: RepoTargetRepo, TargetID: "r4", Mode: RepoBindingInclude, SubPaths: []string{"x"}},
		{TargetKind: RepoTargetRepo, TargetID: "r1", Mode: RepoBindingInclude, SubPaths: []string{"only"}},
	}
	now := time.Unix(100, 0)
	got := ExpandRepoBindings("ag", bs, members, repos, now)

	if len(got) != 2 {
		t.Fatalf("want r1 + r4, got %#v", got)
	}
	if got[0].RepoID != "r1" || got[1].RepoID != "r4" {
		t.Fatalf("order by rel_path: %s, %s", got[0].RepoID, got[1].RepoID)
	}
	if got[0].SubPaths != nil {
		t.Fatalf("whole-repo via group must win over sub_paths, got %v", got[0].SubPaths)
	}
	if len(got[0].Via) != 2 {
		t.Fatalf("r1 via = %#v", got[0].Via)
	}
	if !reflect.DeepEqual(got[1].SubPaths, []string{"x"}) {
		t.Fatalf("r4 sub_paths = %v", got[1].SubPaths)
	}
	if got[0].AgentID != "ag" || !got[0].ComputedAt.Equal(now) {
		t.Fatalf("meta = %#v", got[0])
	}
}

func TestBuildRCARoots(t *testing.T) {
	repos := map[string]*Repository{
		"r1": repoFixture("r1", "/codes", "cg/gateway", RepoStatusActive),
		"r2": repoFixture("r2", "/codes", "migu/gateway", RepoStatusActive),
		"r3": repoFixture("r3", "/codes", "solo", RepoStatusActive),
		"r4": repoFixture("r4", "/other", "cg/gateway", RepoStatusActive),
		"r5": repoFixture("r5", "/codes", "gone", RepoStatusMissing),
	}
	eff := []*AgentEffectiveRepo{
		{RepoID: "r1"}, {RepoID: "r2"}, {RepoID: "r3", SubPaths: []string{"svc/x"}}, {RepoID: "r4"}, {RepoID: "r5"},
	}
	got := buildRCARoots(eff, repos)
	want := []tool.RCARoot{
		{Name: "codes/cg/gateway", Path: filepath.Join("/codes", "cg", "gateway")},
		{Name: "migu/gateway", Path: filepath.Join("/codes", "migu", "gateway")},
		{Name: "solo/svc/x", Path: filepath.Join("/codes", "solo", "svc", "x")},
		{Name: "other/cg/gateway", Path: filepath.Join("/other", "cg", "gateway")},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got  %#v\nwant %#v", got, want)
	}
}

func assertUniqueRootNames(t *testing.T, roots []tool.RCARoot) {
	t.Helper()
	seen := map[string]bool{}
	for _, r := range roots {
		if seen[r.Name] {
			t.Fatalf("duplicate root name %q in %#v", r.Name, roots)
		}
		seen[r.Name] = true
	}
}

func TestBuildRCARoots_SameCodeRootBasename(t *testing.T) {
	repos := map[string]*Repository{
		"r1": repoFixture("r1", "/x/codes", "cg/gw", RepoStatusActive),
		"r2": repoFixture("r2", "/y/codes", "cg/gw", RepoStatusActive),
	}
	got := buildRCARoots([]*AgentEffectiveRepo{{RepoID: "r1"}, {RepoID: "r2"}}, repos)
	want := []tool.RCARoot{
		{Name: filepath.ToSlash(filepath.Join("/x/codes", "cg", "gw")), Path: filepath.Join("/x/codes", "cg", "gw")},
		{Name: filepath.ToSlash(filepath.Join("/y/codes", "cg", "gw")), Path: filepath.Join("/y/codes", "cg", "gw")},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got  %#v\nwant %#v", got, want)
	}
}

func TestBuildRCARoots_PrefixedNameCollidesWithRelPath(t *testing.T) {
	repos := map[string]*Repository{
		"r1": repoFixture("r1", "/codes", "cg/gw", RepoStatusActive),
		"r2": repoFixture("r2", "/other", "cg/gw", RepoStatusActive),
		"r3": repoFixture("r3", "/z", "codes/cg/gw", RepoStatusActive),
	}
	got := buildRCARoots([]*AgentEffectiveRepo{{RepoID: "r1"}, {RepoID: "r2"}, {RepoID: "r3"}}, repos)
	if len(got) != 3 {
		t.Fatalf("got %#v", got)
	}
	assertUniqueRootNames(t, got)
	if got[1].Name != "other/cg/gw" {
		t.Fatalf("non-colliding prefixed name must stay, got %q", got[1].Name)
	}
}

func TestPlanLegacyBinding(t *testing.T) {
	root := filepath.FromSlash("/codes")
	repos := []*Repository{
		repoFixture("r1", root, "cg/a", RepoStatusActive),
		repoFixture("r2", root, "cg/b", RepoStatusActive),
		repoFixture("r3", root, "solo", RepoStatusActive),
	}
	groups := []*RepoGroup{{ID: "g1", Kind: RepoGroupDir, Rule: &RepoGroupRule{CodeRoot: root, RelPrefix: "cg"}}}

	cases := []struct {
		target, action string
		wantKinds      []string
		wantSub        []string
	}{
		{filepath.Join(root, "solo"), LegacyActionBindRepo, []string{RepoTargetRepo}, nil},
		{filepath.Join(root, "cg"), LegacyActionBindGroup, []string{RepoTargetGroup}, nil},
		{root, LegacyActionManualMulti, []string{RepoTargetRepo, RepoTargetRepo, RepoTargetRepo}, nil},
		{filepath.Join(root, "solo", "svc", "x"), LegacyActionManualSubdir, []string{RepoTargetRepo}, []string{"svc/x"}},
		{filepath.FromSlash("/elsewhere"), LegacyActionUnresolved, nil, nil},
	}
	for _, c := range cases {
		p := planLegacyBinding(c.target, repos, groups)
		if p.Action != c.action {
			t.Fatalf("%s: action = %s, want %s", c.target, p.Action, c.action)
		}
		var kinds []string
		for _, b := range p.Bindings {
			kinds = append(kinds, b.TargetKind)
		}
		if !reflect.DeepEqual(kinds, c.wantKinds) {
			t.Fatalf("%s: kinds = %v", c.target, kinds)
		}
		if c.wantSub != nil && !reflect.DeepEqual(p.Bindings[0].SubPaths, c.wantSub) {
			t.Fatalf("%s: sub = %v", c.target, p.Bindings[0].SubPaths)
		}
	}
}
