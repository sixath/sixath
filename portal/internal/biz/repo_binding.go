package biz

import (
	"fmt"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/sixath/framework/tool"
)

// normalizeRepoBindings validates and canonicalizes bindings for one agent.
func normalizeRepoBindings(agentID, actor string, in []*AgentRepoBinding) ([]*AgentRepoBinding, error) {
	out := make([]*AgentRepoBinding, 0, len(in))
	seen := map[string]struct{}{}
	for i, b := range in {
		if b == nil {
			continue
		}
		kind := strings.TrimSpace(b.TargetKind)
		id := strings.TrimSpace(b.TargetID)
		mode := strings.TrimSpace(b.Mode)
		if mode == "" {
			mode = RepoBindingInclude
		}
		if kind != RepoTargetRepo && kind != RepoTargetGroup {
			return nil, fmt.Errorf("%w: [%d] target_kind %q not supported", ErrInvalidRepoBinding, i, kind)
		}
		if id == "" {
			return nil, fmt.Errorf("%w: [%d] target_id required", ErrInvalidRepoBinding, i)
		}
		if mode != RepoBindingInclude && mode != RepoBindingExclude {
			return nil, fmt.Errorf("%w: [%d] mode %q invalid", ErrInvalidRepoBinding, i, mode)
		}
		if mode == RepoBindingExclude && kind != RepoTargetRepo {
			return nil, fmt.Errorf("%w: [%d] exclude is only allowed for repo", ErrInvalidRepoBinding, i)
		}
		subs, err := normalizeSubPaths(b.SubPaths)
		if err != nil {
			return nil, fmt.Errorf("%w: [%d] %v", ErrInvalidRepoBinding, i, err)
		}
		if len(subs) > 0 && (kind != RepoTargetRepo || mode != RepoBindingInclude) {
			return nil, fmt.Errorf("%w: [%d] sub_paths only allowed for repo include", ErrInvalidRepoBinding, i)
		}
		key := kind + "\x00" + id
		if _, dup := seen[key]; dup {
			return nil, fmt.Errorf("%w: [%d] duplicate target %s/%s", ErrInvalidRepoBinding, i, kind, id)
		}
		seen[key] = struct{}{}
		out = append(out, &AgentRepoBinding{
			AgentID: agentID, TargetKind: kind, TargetID: id, Mode: mode,
			SubPaths: subs, Priority: b.Priority, CreatedBy: actor,
		})
	}
	return out, nil
}

func normalizeSubPaths(in []string) ([]string, error) {
	set := map[string]struct{}{}
	for _, p := range in {
		s := strings.Trim(filepath.ToSlash(strings.TrimSpace(p)), "/")
		if s == "" {
			continue
		}
		c := path.Clean(s)
		if c == "." || c == ".." || strings.HasPrefix(c, "../") || strings.Contains(c, ":") {
			return nil, fmt.Errorf("sub_path %q must be a relative path inside the repo", p)
		}
		for _, seg := range strings.Split(c, "/") {
			if strings.EqualFold(seg, ".git") {
				return nil, fmt.Errorf("sub_path %q must not reach into .git", p)
			}
		}
		set[c] = struct{}{}
	}
	if len(set) == 0 {
		return nil, nil
	}
	out := make([]string, 0, len(set))
	for s := range set {
		out = append(out, s)
	}
	sort.Strings(out)
	return out, nil
}

// ExpandRepoBindings computes the effective repo set (design §4.3). groupMembers holds
// active member repo ids per group; repos must contain every referenced repo id.
func ExpandRepoBindings(agentID string, bindings []*AgentRepoBinding, groupMembers map[string][]string, repos map[string]*Repository, now time.Time) []*AgentEffectiveRepo {
	type acc struct {
		via   []BindingRef
		subs  []string
		whole bool
	}
	accs := map[string]*acc{}
	add := func(repoID string, ref BindingRef, subs []string) {
		a := accs[repoID]
		if a == nil {
			a = &acc{}
			accs[repoID] = a
		}
		a.via = append(a.via, ref)
		if len(subs) == 0 {
			a.whole = true
		} else {
			a.subs = append(a.subs, subs...)
		}
	}
	excluded := map[string]bool{}
	for _, b := range bindings {
		ref := BindingRef{Kind: b.TargetKind, ID: b.TargetID}
		switch {
		case b.TargetKind == RepoTargetRepo && b.Mode == RepoBindingExclude:
			excluded[b.TargetID] = true
		case b.TargetKind == RepoTargetRepo:
			add(b.TargetID, ref, b.SubPaths)
		case b.TargetKind == RepoTargetGroup:
			for _, id := range groupMembers[b.TargetID] {
				add(id, ref, nil)
			}
		}
	}
	out := make([]*AgentEffectiveRepo, 0, len(accs))
	for id, a := range accs {
		r := repos[id]
		if excluded[id] || r == nil || r.Status != RepoStatusActive {
			continue
		}
		var subs []string
		if !a.whole {
			subs, _ = normalizeSubPaths(a.subs)
		}
		out = append(out, &AgentEffectiveRepo{AgentID: agentID, RepoID: id, Via: a.via, SubPaths: subs, ComputedAt: now})
	}
	sort.Slice(out, func(i, j int) bool {
		ri, rj := repos[out[i].RepoID], repos[out[j].RepoID]
		if ri.RelPath != rj.RelPath {
			return ri.RelPath < rj.RelPath
		}
		return ri.CodeRoot < rj.CodeRoot
	})
	return out
}

// buildRCARoots maps effective repos to RCA roots named by rel_path (rel_path/sub for
// sub_paths). Names repeated across code roots are prefixed with the code root basename;
// names still repeated after that fall back to the slash form of the absolute path.
func buildRCARoots(eff []*AgentEffectiveRepo, repos map[string]*Repository) []tool.RCARoot {
	type item struct {
		root     tool.RCARoot
		codeRoot string
	}
	var items []item
	for _, e := range eff {
		r := repos[e.RepoID]
		if r == nil || r.Status != RepoStatusActive {
			continue
		}
		base := r.AbsPath()
		if len(e.SubPaths) == 0 {
			items = append(items, item{tool.RCARoot{Name: r.RelPath, Path: base}, r.CodeRoot})
			continue
		}
		for _, sp := range e.SubPaths {
			items = append(items, item{tool.RCARoot{
				Name: r.RelPath + "/" + sp,
				Path: filepath.Join(base, filepath.FromSlash(sp)),
			}, r.CodeRoot})
		}
	}
	count := map[string]int{}
	for _, it := range items {
		count[it.root.Name]++
	}
	out := make([]tool.RCARoot, 0, len(items))
	for _, it := range items {
		if count[it.root.Name] > 1 {
			it.root.Name = filepath.Base(it.codeRoot) + "/" + it.root.Name
		}
		out = append(out, it.root)
	}
	clear(count)
	for _, r := range out {
		count[r.Name]++
	}
	for i := range out {
		if count[out[i].Name] > 1 {
			out[i].Name = filepath.ToSlash(out[i].Path)
		}
	}
	return out
}

const (
	LegacyActionBindRepo     = "bind_repo"
	LegacyActionBindGroup    = "bind_group"
	LegacyActionManualMulti  = "manual_multi"
	LegacyActionManualSubdir = "manual_subdir"
	LegacyActionUnresolved   = "unresolved"
)

type legacyPlan struct {
	Action   string
	Bindings []*AgentRepoBinding
}

// AutoApply reports whether the plan reproduces the old link exactly.
func (p legacyPlan) AutoApply() bool {
	return p.Action == LegacyActionBindRepo || p.Action == LegacyActionBindGroup
}

// planLegacyBinding maps an old workspace/code target to bindings (design §14 step 2).
func planLegacyBinding(target string, repos []*Repository, groups []*RepoGroup) legacyPlan {
	target = filepath.Clean(target)
	sep := string(filepath.Separator)
	for _, r := range repos {
		if r.Status == RepoStatusActive && filepath.Clean(r.AbsPath()) == target {
			return legacyPlan{LegacyActionBindRepo, []*AgentRepoBinding{{TargetKind: RepoTargetRepo, TargetID: r.ID, Mode: RepoBindingInclude}}}
		}
	}
	for _, g := range groups {
		if d := g.DirPath(); d != "" && filepath.Clean(d) == target {
			return legacyPlan{LegacyActionBindGroup, []*AgentRepoBinding{{TargetKind: RepoTargetGroup, TargetID: g.ID, Mode: RepoBindingInclude}}}
		}
	}
	var under []*AgentRepoBinding
	for _, r := range repos {
		abs := filepath.Clean(r.AbsPath())
		if r.Status != RepoStatusActive {
			continue
		}
		if strings.HasPrefix(abs, target+sep) {
			under = append(under, &AgentRepoBinding{TargetKind: RepoTargetRepo, TargetID: r.ID, Mode: RepoBindingInclude})
			continue
		}
		if strings.HasPrefix(target, abs+sep) {
			rel, _ := filepath.Rel(abs, target)
			return legacyPlan{LegacyActionManualSubdir, []*AgentRepoBinding{{
				TargetKind: RepoTargetRepo, TargetID: r.ID, Mode: RepoBindingInclude,
				SubPaths: []string{filepath.ToSlash(rel)},
			}}}
		}
	}
	if len(under) > 0 {
		return legacyPlan{LegacyActionManualMulti, under}
	}
	return legacyPlan{Action: LegacyActionUnresolved}
}
