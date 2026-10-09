package biz

import (
	"context"
	"errors"
	"fmt"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/go-kratos/kratos/v2/log"
	"github.com/sixath/framework/tool"
	fwws "github.com/sixath/framework/workspace"
)

// RepoRegistryUsecase maintains the repository registry and agent repo bindings.
type RepoRegistryUsecase struct {
	repo      RepoRegistryRepo
	agents    AgentRepo
	codeRoots []string
	scanMu    sync.Mutex
	log       *log.Helper
}

func NewRepoRegistryUsecase(repo RepoRegistryRepo, agents AgentRepo, codeRoots []string, logger log.Logger) *RepoRegistryUsecase {
	return &RepoRegistryUsecase{repo: repo, agents: agents, codeRoots: cleanCodeRoots(codeRoots), log: log.NewHelper(logger)}
}

func cleanCodeRoots(in []string) []string {
	seen := map[string]struct{}{}
	var out []string
	for _, r := range in {
		r = strings.TrimSpace(r)
		if r == "" {
			continue
		}
		r = filepath.Clean(r)
		if _, ok := seen[r]; ok {
			continue
		}
		seen[r] = struct{}{}
		out = append(out, r)
	}
	return out
}

type RepoScanReport struct {
	Roots    int      `json:"roots"`
	Found    int      `json:"found"`
	Added    int      `json:"added"`
	Restored int      `json:"restored"`
	Missing  int      `json:"missing"`
	Errors   []string `json:"errors,omitempty"`
}

// Scan discovers repositories under every code root, refreshes dir groups and recomputes
// effective repo sets. An unreadable code root is reported and its repos are left untouched.
func (uc *RepoRegistryUsecase) Scan(ctx context.Context) (*RepoScanReport, error) {
	if !uc.scanMu.TryLock() {
		return nil, ErrRepoScanRunning
	}
	defer uc.scanMu.Unlock()
	rep := &RepoScanReport{}
	for _, root := range uc.codeRoots {
		if err := uc.scanRoot(ctx, root, rep); err != nil {
			rep.Errors = append(rep.Errors, fmt.Sprintf("%s: %v", root, err))
		}
	}
	if err := uc.RecomputeAll(ctx); err != nil {
		return rep, err
	}
	return rep, nil
}

func (uc *RepoRegistryUsecase) scanRoot(ctx context.Context, root string, rep *RepoScanReport) error {
	rels, unreadable, err := DiscoverGitRepos(root, RepoScanMaxDepth)
	if err != nil {
		return err
	}
	rep.Roots++
	for _, u := range unreadable {
		rep.Errors = append(rep.Errors, fmt.Sprintf("%s: unreadable subtree %q", root, u))
	}
	existing, err := uc.repo.ListRepositories(ctx, RepoFilter{CodeRoot: root})
	if err != nil {
		return err
	}
	byRel := make(map[string]*Repository, len(existing))
	for _, r := range existing {
		byRel[r.RelPath] = r
	}
	now := time.Now()
	seen := make(map[string]bool, len(rels))
	dirMembers := map[string][]string{}
	for _, rel := range rels {
		seen[rel] = true
		info, gerr := ReadGitInfo(filepath.Join(root, filepath.FromSlash(rel)))
		if gerr != nil {
			uc.log.Warnf("repo scan: read git info %s/%s: %v", root, rel, gerr)
		}
		saved, err := uc.repo.UpsertScannedRepository(ctx, &Repository{
			CodeRoot: root, RelPath: rel, Name: path.Base(rel),
			GitRemote: info.Remote, GitBranch: info.Branch, HeadCommit: info.Commit, LastScannedAt: &now,
		})
		if err != nil {
			return err
		}
		rep.Found++
		switch prev := byRel[rel]; {
		case prev == nil:
			rep.Added++
		case prev.Status == RepoStatusMissing:
			rep.Restored++
		}
		if parent := path.Dir(rel); parent != "." {
			dirMembers[parent] = append(dirMembers[parent], saved.ID)
		}
	}
	for rel, r := range byRel {
		if seen[rel] || r.Status != RepoStatusActive {
			continue
		}
		if underAnyPrefix(rel, unreadable) {
			// 子树读不到时保持原状，并保留其目录组成员身份，避免绑定该组的 agent 丢仓库。
			if parent := path.Dir(rel); parent != "." {
				dirMembers[parent] = append(dirMembers[parent], r.ID)
			}
			continue
		}
		if err := uc.repo.SetRepositoryStatus(ctx, r.ID, RepoStatusMissing); err != nil {
			return err
		}
		rep.Missing++
	}
	return uc.syncDirGroups(ctx, root, dirMembers)
}

func (uc *RepoRegistryUsecase) syncDirGroups(ctx context.Context, root string, members map[string][]string) error {
	touched := map[string]bool{}
	for prefix, ids := range members {
		g, err := uc.repo.UpsertDirGroup(ctx, root, prefix)
		if err != nil {
			return err
		}
		touched[g.ID] = true
		if err := uc.repo.ReplaceGroupMembers(ctx, g.ID, RepoMemberSourceRule, ids); err != nil {
			return err
		}
	}
	groups, err := uc.repo.ListGroups(ctx, RepoGroupDir)
	if err != nil {
		return err
	}
	for _, g := range groups {
		if g.Rule != nil && g.Rule.CodeRoot == root && !touched[g.ID] {
			if err := uc.repo.ReplaceGroupMembers(ctx, g.ID, RepoMemberSourceRule, nil); err != nil {
				return err
			}
		}
	}
	return nil
}

// RecomputeAgent rebuilds agent_effective_repos for one agent.
func (uc *RepoRegistryUsecase) RecomputeAgent(ctx context.Context, agentID string) ([]*AgentEffectiveRepo, error) {
	bs, err := uc.repo.ListAgentBindings(ctx, agentID)
	if err != nil {
		return nil, err
	}
	var groupIDs, repoIDs []string
	for _, b := range bs {
		switch b.TargetKind {
		case RepoTargetGroup:
			groupIDs = append(groupIDs, b.TargetID)
		case RepoTargetRepo:
			repoIDs = append(repoIDs, b.TargetID)
		}
	}
	members, err := uc.repo.ListActiveGroupMembers(ctx, groupIDs)
	if err != nil {
		return nil, err
	}
	for _, ids := range members {
		repoIDs = append(repoIDs, ids...)
	}
	repos, err := uc.repo.GetRepositoriesByIDs(ctx, repoIDs)
	if err != nil {
		return nil, err
	}
	eff := ExpandRepoBindings(agentID, bs, members, repos, time.Now())
	if err := uc.repo.ReplaceEffectiveRepos(ctx, agentID, eff); err != nil {
		return nil, err
	}
	return eff, nil
}

// RecomputeAll recomputes every agent that has bindings; errors are joined, not fatal.
func (uc *RepoRegistryUsecase) RecomputeAll(ctx context.Context) error {
	ids, err := uc.repo.ListAgentIDsWithBindings(ctx)
	if err != nil {
		return err
	}
	return uc.recomputeAgents(ctx, ids)
}

func (uc *RepoRegistryUsecase) recomputeAgents(ctx context.Context, ids []string) error {
	var errs []error
	for _, id := range ids {
		if _, err := uc.RecomputeAgent(ctx, id); err != nil {
			errs = append(errs, fmt.Errorf("agent %s: %w", id, err))
		}
	}
	return errors.Join(errs...)
}

// RCARootsForAgent returns RCA roots for the agent's effective repos; nil means the agent
// has no repo bindings and callers should fall back to the legacy workspace/code link.
func (uc *RepoRegistryUsecase) RCARootsForAgent(ctx context.Context, agentID string) ([]tool.RCARoot, error) {
	eff, err := uc.repo.ListEffectiveRepos(ctx, agentID)
	if err != nil || len(eff) == 0 {
		return nil, err
	}
	ids := make([]string, 0, len(eff))
	for _, e := range eff {
		ids = append(ids, e.RepoID)
	}
	repos, err := uc.repo.GetRepositoriesByIDs(ctx, ids)
	if err != nil {
		return nil, err
	}
	sort.Slice(eff, func(i, j int) bool {
		ri, rj := repos[eff[i].RepoID], repos[eff[j].RepoID]
		if ri == nil || rj == nil {
			return ri != nil
		}
		return ri.RelPath < rj.RelPath
	})
	return buildRCARoots(eff, repos), nil
}

// ---- repositories ----

func (uc *RepoRegistryUsecase) ListRepos(ctx context.Context, f RepoFilter) ([]*Repository, error) {
	return uc.repo.ListRepositories(ctx, f)
}

type RepoDetail struct {
	Repository *Repository `json:"repository"`
	AgentIDs   []string    `json:"agent_ids"`
}

func (uc *RepoRegistryUsecase) GetRepo(ctx context.Context, id string) (*RepoDetail, error) {
	m, err := uc.repo.GetRepositoriesByIDs(ctx, []string{id})
	if err != nil {
		return nil, err
	}
	r := m[id]
	if r == nil {
		return nil, ErrRepoNotFound
	}
	agents, err := uc.repo.ListAgentIDsByEffectiveRepo(ctx, id)
	if err != nil {
		return nil, err
	}
	return &RepoDetail{Repository: r, AgentIDs: agents}, nil
}

// PatchRepo updates user-editable fields; status may only be set to active or archived.
func (uc *RepoRegistryUsecase) PatchRepo(ctx context.Context, id string, p RepoMetaPatch) (*Repository, error) {
	if p.Status != nil && *p.Status != RepoStatusActive && *p.Status != RepoStatusArchived {
		return nil, fmt.Errorf("%w: status must be active or archived", ErrInvalidRepoBinding)
	}
	r, err := uc.repo.UpdateRepositoryMeta(ctx, id, p)
	if err != nil {
		return nil, err
	}
	if p.Status != nil {
		agents, err := uc.repo.ListAgentIDsByEffectiveRepo(ctx, id)
		if err != nil {
			return nil, err
		}
		if *p.Status == RepoStatusActive {
			if agents, err = uc.repo.ListAgentIDsWithBindings(ctx); err != nil {
				return nil, err
			}
		}
		if err := uc.recomputeAgents(ctx, agents); err != nil {
			uc.log.Warnf("recompute after repo status change: %v", err)
		}
	}
	return r, nil
}

// ---- groups ----

type RepoGroupView struct {
	*RepoGroup
	RepoIDs []string `json:"repo_ids"`
}

func (uc *RepoRegistryUsecase) ListGroups(ctx context.Context, kind string) ([]*RepoGroupView, error) {
	groups, err := uc.repo.ListGroups(ctx, kind)
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(groups))
	for _, g := range groups {
		ids = append(ids, g.ID)
	}
	members, err := uc.repo.ListActiveGroupMembers(ctx, ids)
	if err != nil {
		return nil, err
	}
	out := make([]*RepoGroupView, 0, len(groups))
	for _, g := range groups {
		out = append(out, &RepoGroupView{RepoGroup: g, RepoIDs: members[g.ID]})
	}
	return out, nil
}

func (uc *RepoRegistryUsecase) CreateManualGroup(ctx context.Context, name string, repoIDs []string, owner string) (*RepoGroupView, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, fmt.Errorf("%w: name required", ErrInvalidRepoGroup)
	}
	if err := uc.requireRepos(ctx, repoIDs, ErrInvalidRepoGroup); err != nil {
		return nil, err
	}
	g, err := uc.repo.CreateGroup(ctx, &RepoGroup{Name: name, Kind: RepoGroupManual, OwnerID: owner})
	if err != nil {
		return nil, err
	}
	if err := uc.repo.ReplaceGroupMembers(ctx, g.ID, RepoMemberSourceManual, repoIDs); err != nil {
		return nil, err
	}
	return &RepoGroupView{RepoGroup: g, RepoIDs: repoIDs}, nil
}

func (uc *RepoRegistryUsecase) SetManualGroupMembers(ctx context.Context, groupID string, repoIDs []string) error {
	if _, err := uc.requireGroup(ctx, groupID, RepoGroupManual); err != nil {
		return err
	}
	if err := uc.requireRepos(ctx, repoIDs, ErrInvalidRepoGroup); err != nil {
		return err
	}
	if err := uc.repo.ReplaceGroupMembers(ctx, groupID, RepoMemberSourceManual, repoIDs); err != nil {
		return err
	}
	agents, err := uc.repo.ListAgentIDsBoundToGroup(ctx, groupID)
	if err != nil {
		return err
	}
	return uc.recomputeAgents(ctx, agents)
}

// DeleteGroup deletes a manual group and the bindings that reference it.
func (uc *RepoRegistryUsecase) DeleteGroup(ctx context.Context, groupID string) error {
	if _, err := uc.requireGroup(ctx, groupID, RepoGroupManual); err != nil {
		return err
	}
	agents, err := uc.repo.ListAgentIDsBoundToGroup(ctx, groupID)
	if err != nil {
		return err
	}
	if err := uc.repo.DeleteGroup(ctx, groupID); err != nil {
		return err
	}
	return uc.recomputeAgents(ctx, agents)
}

func (uc *RepoRegistryUsecase) requireGroup(ctx context.Context, id, kind string) (*RepoGroup, error) {
	m, err := uc.repo.GetGroupsByIDs(ctx, []string{id})
	if err != nil {
		return nil, err
	}
	g := m[id]
	if g == nil {
		return nil, ErrRepoNotFound
	}
	if kind != "" && g.Kind != kind {
		return nil, fmt.Errorf("%w: group %s is %s, only %s groups can be edited", ErrInvalidRepoGroup, id, g.Kind, kind)
	}
	return g, nil
}

func (uc *RepoRegistryUsecase) requireRepos(ctx context.Context, ids []string, kindErr error) error {
	m, err := uc.repo.GetRepositoriesByIDs(ctx, ids)
	if err != nil {
		return err
	}
	for _, id := range ids {
		if m[id] == nil {
			return fmt.Errorf("%w: unknown repo %s", kindErr, id)
		}
	}
	return nil
}

// ---- agent bindings ----

type EffectiveRepoView struct {
	*AgentEffectiveRepo
	Repository *Repository `json:"repository,omitempty"`
}

type AgentRepoBindingsView struct {
	Bindings  []*AgentRepoBinding  `json:"bindings"`
	Effective []*EffectiveRepoView `json:"effective"`
}

func (uc *RepoRegistryUsecase) GetBindings(ctx context.Context, agentID string) (*AgentRepoBindingsView, error) {
	bs, err := uc.repo.ListAgentBindings(ctx, agentID)
	if err != nil {
		return nil, err
	}
	eff, err := uc.repo.ListEffectiveRepos(ctx, agentID)
	if err != nil {
		return nil, err
	}
	return uc.bindingsView(ctx, bs, eff)
}

// ReplaceBindings validates and replaces all bindings of an agent, then recomputes its effective set.
func (uc *RepoRegistryUsecase) ReplaceBindings(ctx context.Context, agentID string, in []*AgentRepoBinding, actor string) (*AgentRepoBindingsView, error) {
	bs, err := normalizeRepoBindings(agentID, actor, in)
	if err != nil {
		return nil, err
	}
	var repoIDs, groupIDs []string
	for _, b := range bs {
		if b.TargetKind == RepoTargetRepo {
			repoIDs = append(repoIDs, b.TargetID)
		} else {
			groupIDs = append(groupIDs, b.TargetID)
		}
	}
	if err := uc.requireRepos(ctx, repoIDs, ErrInvalidRepoBinding); err != nil {
		return nil, err
	}
	groups, err := uc.repo.GetGroupsByIDs(ctx, groupIDs)
	if err != nil {
		return nil, err
	}
	for _, id := range groupIDs {
		if groups[id] == nil {
			return nil, fmt.Errorf("%w: unknown repo group %s", ErrInvalidRepoBinding, id)
		}
	}
	if err := uc.repo.ReplaceAgentBindings(ctx, agentID, bs); err != nil {
		return nil, err
	}
	eff, err := uc.RecomputeAgent(ctx, agentID)
	if err != nil {
		return nil, err
	}
	return uc.bindingsView(ctx, bs, eff)
}

func (uc *RepoRegistryUsecase) CopyBindings(ctx context.Context, fromAgentID, toAgentID, actor string) (*AgentRepoBindingsView, error) {
	src, err := uc.repo.ListAgentBindings(ctx, fromAgentID)
	if err != nil {
		return nil, err
	}
	return uc.ReplaceBindings(ctx, toAgentID, src, actor)
}

func (uc *RepoRegistryUsecase) bindingsView(ctx context.Context, bs []*AgentRepoBinding, eff []*AgentEffectiveRepo) (*AgentRepoBindingsView, error) {
	ids := make([]string, 0, len(eff))
	for _, e := range eff {
		ids = append(ids, e.RepoID)
	}
	repos, err := uc.repo.GetRepositoriesByIDs(ctx, ids)
	if err != nil {
		return nil, err
	}
	view := &AgentRepoBindingsView{Bindings: bs, Effective: make([]*EffectiveRepoView, 0, len(eff))}
	for _, e := range eff {
		view.Effective = append(view.Effective, &EffectiveRepoView{AgentEffectiveRepo: e, Repository: repos[e.RepoID]})
	}
	return view, nil
}

// ---- legacy workspace/code migration (design §14) ----

const LegacyActionSkipHasBindings = "skip_has_bindings"

type LegacyLinkMigrationItem struct {
	AgentID  string              `json:"agent_id"`
	Target   string              `json:"target"`
	Action   string              `json:"action"`
	Bindings []*AgentRepoBinding `json:"bindings,omitempty"`
	Applied  bool                `json:"applied"`
	Error    string              `json:"error,omitempty"`
}

// MigrateLegacyLinks maps each agent's workspace/code link to repo bindings. With apply=false
// it only reports; with apply=true it writes bindings only for exact repo / dir-group matches.
func (uc *RepoRegistryUsecase) MigrateLegacyLinks(ctx context.Context, apply bool) ([]*LegacyLinkMigrationItem, error) {
	repos, err := uc.repo.ListRepositories(ctx, RepoFilter{Status: RepoStatusActive})
	if err != nil {
		return nil, err
	}
	groups, err := uc.repo.ListGroups(ctx, RepoGroupDir)
	if err != nil {
		return nil, err
	}
	bound, err := uc.repo.ListAgentIDsWithBindings(ctx)
	if err != nil {
		return nil, err
	}
	hasBindings := make(map[string]bool, len(bound))
	for _, id := range bound {
		hasBindings[id] = true
	}
	var items []*LegacyLinkMigrationItem
	const pageSize = 200
	for page := int32(1); ; page++ {
		agents, _, err := uc.agents.List(ctx, page, pageSize)
		if err != nil {
			return items, err
		}
		for _, a := range agents {
			target := fwws.ResolveCodeMount(a.Workspace)
			if target == "" {
				continue
			}
			item := &LegacyLinkMigrationItem{AgentID: a.ID, Target: target}
			items = append(items, item)
			if hasBindings[a.ID] {
				item.Action = LegacyActionSkipHasBindings
				continue
			}
			plan := planLegacyBinding(target, repos, groups)
			item.Action, item.Bindings = plan.Action, plan.Bindings
			if !apply || !plan.AutoApply() {
				continue
			}
			if _, err := uc.ReplaceBindings(ctx, a.ID, plan.Bindings, "legacy-migration"); err != nil {
				item.Error = err.Error()
				continue
			}
			item.Applied = true
		}
		if len(agents) < pageSize {
			return items, nil
		}
	}
}

// BindFromLegacyLink keeps the old workspace-link API in sync: when the agent has no
// bindings and target maps exactly to a repo or dir group, the binding is written.
func (uc *RepoRegistryUsecase) BindFromLegacyLink(ctx context.Context, agentID, target, actor string) (bool, error) {
	existing, err := uc.repo.ListAgentBindings(ctx, agentID)
	if err != nil || len(existing) > 0 {
		return false, err
	}
	repos, err := uc.repo.ListRepositories(ctx, RepoFilter{Status: RepoStatusActive})
	if err != nil {
		return false, err
	}
	groups, err := uc.repo.ListGroups(ctx, RepoGroupDir)
	if err != nil {
		return false, err
	}
	plan := planLegacyBinding(target, repos, groups)
	if !plan.AutoApply() {
		return false, nil
	}
	if _, err := uc.ReplaceBindings(ctx, agentID, plan.Bindings, actor); err != nil {
		return false, err
	}
	return true, nil
}
