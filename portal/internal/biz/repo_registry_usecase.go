package biz

import (
	"context"
	"errors"
	"fmt"
	"path"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	pkgErrors "backend/internal/pkg/errors"

	"github.com/go-kratos/kratos/v2/log"
	"github.com/sixath/framework/tool"
	fwws "github.com/sixath/framework/workspace"
)

// RepoRegistryUsecase maintains the repository registry and agent repo bindings.
type RepoRegistryUsecase struct {
	repo       RepoRegistryRepo
	agents     AgentRepo
	codeRoots  []repoCodeRoot
	scanMu     sync.Mutex
	agentLocks sync.Map // agent id -> *sync.Mutex
	log        *log.Helper
}

// repoCodeRoot is a configured code root. path is stored as repositories.code_root;
// resolved has symlinks evaluated (equal to path when evaluation fails).
type repoCodeRoot struct {
	path     string
	resolved string
}

func NewRepoRegistryUsecase(repo RepoRegistryRepo, agents AgentRepo, codeRoots []string, logger log.Logger) *RepoRegistryUsecase {
	return &RepoRegistryUsecase{repo: repo, agents: agents, codeRoots: cleanCodeRoots(codeRoots), log: log.NewHelper(logger)}
}

func cleanCodeRoots(in []string) []repoCodeRoot {
	seen := map[string]struct{}{}
	var out []repoCodeRoot
	for _, r := range in {
		r = strings.TrimSpace(r)
		if r == "" {
			continue
		}
		abs, err := filepath.Abs(r)
		if err != nil {
			abs = filepath.Clean(r)
		}
		if _, ok := seen[pathKey(abs)]; ok {
			continue
		}
		seen[pathKey(abs)] = struct{}{}
		resolved := abs
		if ev, err := filepath.EvalSymlinks(abs); err == nil {
			resolved = filepath.Clean(ev)
		}
		out = append(out, repoCodeRoot{path: abs, resolved: resolved})
	}
	return out
}

// pathKey normalizes p for equality checks; Windows paths compare case-insensitively.
func pathKey(p string) string {
	p = filepath.Clean(p)
	if runtime.GOOS == "windows" {
		return strings.ToLower(p)
	}
	return p
}

func samePath(a, b string) bool {
	return pathKey(a) == pathKey(b)
}

// pathWithin reports whether p equals root or lies under it. filepath.Rel already folds
// case on Windows.
func pathWithin(root, p string) bool {
	rel, err := filepath.Rel(root, p)
	if err != nil || filepath.IsAbs(rel) {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// mapTargetToConfiguredRoot rewrites a symlink-resolved path under a code root's resolved
// form back to the configured root, so it compares equal to stored repository paths.
func mapTargetToConfiguredRoot(target string, roots []repoCodeRoot) string {
	target = filepath.Clean(target)
	for _, r := range roots {
		if pathWithin(r.path, target) {
			return target
		}
	}
	best := -1
	for i, r := range roots {
		if pathWithin(r.resolved, target) && (best < 0 || len(r.resolved) > len(roots[best].resolved)) {
			best = i
		}
	}
	if best < 0 {
		return target
	}
	rel, _ := filepath.Rel(roots[best].resolved, target)
	return filepath.Join(roots[best].path, rel)
}

// lockAgent serializes binding writes and effective-set recomputation per agent.
func (uc *RepoRegistryUsecase) lockAgent(agentID string) func() {
	v, _ := uc.agentLocks.LoadOrStore(agentID, &sync.Mutex{})
	mu := v.(*sync.Mutex)
	mu.Lock()
	return mu.Unlock
}

func (uc *RepoRegistryUsecase) configuredRoot(codeRoot string) (repoCodeRoot, bool) {
	for _, r := range uc.codeRoots {
		if samePath(r.path, codeRoot) {
			return r, true
		}
	}
	return repoCodeRoot{}, false
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
// effective repo sets. An unreadable code root is reported and its repos are left untouched;
// active repos under code roots that are no longer configured are marked missing.
func (uc *RepoRegistryUsecase) Scan(ctx context.Context) (*RepoScanReport, error) {
	if !uc.scanMu.TryLock() {
		return nil, ErrRepoScanRunning
	}
	defer uc.scanMu.Unlock()
	rep := &RepoScanReport{}
	for _, root := range uc.codeRoots {
		if err := ctx.Err(); err != nil {
			return rep, err
		}
		if err := uc.scanRoot(ctx, root.path, rep); err != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return rep, ctxErr
			}
			rep.Errors = append(rep.Errors, fmt.Sprintf("%s: %v", root.path, err))
		}
	}
	if err := uc.markRemovedRoots(ctx, rep); err != nil {
		return rep, err
	}
	if err := uc.RecomputeAll(ctx); err != nil {
		return rep, err
	}
	return rep, nil
}

// markRemovedRoots is a no-op when no code root is configured, so an empty config does not
// mark the whole registry missing.
func (uc *RepoRegistryUsecase) markRemovedRoots(ctx context.Context, rep *RepoScanReport) error {
	if len(uc.codeRoots) == 0 {
		return nil
	}
	repos, err := uc.repo.ListRepositories(ctx, RepoFilter{Status: RepoStatusActive})
	if err != nil {
		return err
	}
	for _, r := range repos {
		if _, ok := uc.configuredRoot(r.CodeRoot); ok {
			continue
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		changed, err := uc.repo.MarkRepositoryMissingIfActive(ctx, r.ID)
		if err != nil {
			return err
		}
		if changed {
			rep.Missing++
		}
	}
	return nil
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
		if err := ctx.Err(); err != nil {
			return err
		}
		seen[rel] = true
		prev := byRel[rel]
		info, gerr := ReadGitInfo(filepath.Join(root, filepath.FromSlash(rel)))
		if gerr != nil {
			uc.log.Warnf("repo scan: read git info %s/%s: %v", root, rel, gerr)
			if prev != nil {
				info = GitInfo{Branch: prev.GitBranch, Commit: prev.HeadCommit, Remote: prev.GitRemote}
			}
		}
		saved, err := uc.repo.UpsertScannedRepository(ctx, &Repository{
			CodeRoot: root, RelPath: rel, Name: path.Base(rel),
			GitRemote: info.Remote, GitBranch: info.Branch, HeadCommit: info.Commit, LastScannedAt: &now,
		})
		if err != nil {
			return err
		}
		rep.Found++
		switch {
		case prev == nil:
			rep.Added++
		case prev.Status == RepoStatusMissing && saved.Status == RepoStatusActive:
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
		changed, err := uc.repo.MarkRepositoryMissingIfActive(ctx, r.ID)
		if err != nil {
			return err
		}
		if changed {
			rep.Missing++
		}
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
	defer uc.lockAgent(agentID)()
	return uc.recomputeAgentLocked(ctx, agentID)
}

// recomputeAgentLocked requires the caller to hold lockAgent(agentID). The stored rows are
// only rewritten when the set (repo, sub_paths, via) changed.
func (uc *RepoRegistryUsecase) recomputeAgentLocked(ctx context.Context, agentID string) ([]*AgentEffectiveRepo, error) {
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
	cur, err := uc.repo.ListEffectiveRepos(ctx, agentID)
	if err != nil {
		return nil, err
	}
	if sameEffectiveSet(cur, eff) {
		computedAt := make(map[string]time.Time, len(cur))
		for _, c := range cur {
			computedAt[c.RepoID] = c.ComputedAt
		}
		for _, e := range eff {
			e.ComputedAt = computedAt[e.RepoID]
		}
		return eff, nil
	}
	if err := uc.repo.ReplaceEffectiveRepos(ctx, agentID, eff); err != nil {
		return nil, err
	}
	return eff, nil
}

func sameEffectiveSet(a, b []*AgentEffectiveRepo) bool {
	if len(a) != len(b) {
		return false
	}
	key := func(e *AgentEffectiveRepo) string {
		var sb strings.Builder
		sb.WriteString(strings.Join(e.SubPaths, "\x00"))
		for _, v := range e.Via {
			sb.WriteString("\x01" + v.Kind + "\x00" + v.ID)
		}
		return sb.String()
	}
	byRepo := make(map[string]string, len(a))
	for _, e := range a {
		byRepo[e.RepoID] = key(e)
	}
	for _, e := range b {
		k, ok := byRepo[e.RepoID]
		if !ok || k != key(e) {
			return false
		}
	}
	return true
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
		if err := ctx.Err(); err != nil {
			errs = append(errs, err)
			break
		}
		if _, err := uc.RecomputeAgent(ctx, id); err != nil {
			errs = append(errs, fmt.Errorf("agent %s: %w", id, err))
		}
	}
	return errors.Join(errs...)
}

// RCARootsForAgent returns RCA roots for the agent's effective repos. A nil slice means the
// agent has no repo bindings and callers should fall back to the legacy workspace/code link;
// a non-nil (possibly empty) slice is authoritative. Repos under code roots that are no longer
// configured, and paths that vanished or resolve outside their code root, are dropped.
func (uc *RepoRegistryUsecase) RCARootsForAgent(ctx context.Context, agentID string) ([]tool.RCARoot, error) {
	bound, eff, err := uc.readAgentEffective(ctx, agentID)
	if err != nil || !bound {
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
	kept := make([]*AgentEffectiveRepo, 0, len(eff))
	for _, e := range eff {
		r := repos[e.RepoID]
		if r == nil {
			continue
		}
		cr, ok := uc.configuredRoot(r.CodeRoot)
		if !ok {
			continue
		}
		base := r.AbsPath()
		if len(e.SubPaths) == 0 {
			if err := checkRootPath(cr, base); err != nil {
				uc.log.Warnf("rca roots for agent %s: drop %s: %v", agentID, base, err)
				continue
			}
			kept = append(kept, e)
			continue
		}
		var subs []string
		for _, sp := range e.SubPaths {
			p := filepath.Join(base, filepath.FromSlash(sp))
			if err := checkRootPath(cr, p); err != nil {
				uc.log.Warnf("rca roots for agent %s: drop %s: %v", agentID, p, err)
				continue
			}
			subs = append(subs, sp)
		}
		if len(subs) == 0 {
			continue
		}
		c := *e
		c.SubPaths = subs
		kept = append(kept, &c)
	}
	sort.Slice(kept, func(i, j int) bool {
		ri, rj := repos[kept[i].RepoID], repos[kept[j].RepoID]
		if ri.RelPath != rj.RelPath {
			return ri.RelPath < rj.RelPath
		}
		return ri.CodeRoot < rj.CodeRoot
	})
	out := buildRCARoots(kept, repos)
	if out == nil {
		out = []tool.RCARoot{}
	}
	return out, nil
}

// readAgentEffective reads bindings and effective rows under the agent lock so a concurrent
// first bind is observed either before or after its recompute, never in between.
func (uc *RepoRegistryUsecase) readAgentEffective(ctx context.Context, agentID string) (bool, []*AgentEffectiveRepo, error) {
	defer uc.lockAgent(agentID)()
	bs, err := uc.repo.ListAgentBindings(ctx, agentID)
	if err != nil || len(bs) == 0 {
		return false, nil, err
	}
	eff, err := uc.repo.ListEffectiveRepos(ctx, agentID)
	if err != nil {
		return false, nil, err
	}
	return true, eff, nil
}

// checkRootPath requires p to exist and, with symlinks resolved, stay inside the repo's code
// root. The root is re-resolved on every call so a root mounted after startup still works.
func checkRootPath(cr repoCodeRoot, p string) error {
	rootResolved := cr.resolved
	if ev, err := filepath.EvalSymlinks(cr.path); err == nil {
		rootResolved = ev
	}
	resolved, err := filepath.EvalSymlinks(p)
	if err != nil {
		return err
	}
	if !pathWithin(rootResolved, resolved) {
		return fmt.Errorf("resolves to %s outside code root %s", resolved, rootResolved)
	}
	return nil
}

// ---- repositories ----

// nonNilStrings keeps API slices encoding as [] rather than null.
func nonNilStrings(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

// repoView normalizes r in place for API responses; nil-safe.
func repoView(r *Repository) *Repository {
	if r != nil {
		r.Tags = nonNilStrings(r.Tags)
	}
	return r
}

func (uc *RepoRegistryUsecase) ListRepos(ctx context.Context, f RepoFilter) ([]*Repository, error) {
	rs, err := uc.repo.ListRepositories(ctx, f)
	if err != nil {
		return nil, err
	}
	for _, r := range rs {
		repoView(r)
	}
	return rs, nil
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
	return &RepoDetail{Repository: repoView(r), AgentIDs: nonNilStrings(agents)}, nil
}

// PatchRepo updates user-editable fields; status may only be set to active or archived.
func (uc *RepoRegistryUsecase) PatchRepo(ctx context.Context, id string, p RepoMetaPatch) (*Repository, error) {
	if p.Status != nil && *p.Status != RepoStatusActive && *p.Status != RepoStatusArchived {
		return nil, fmt.Errorf("%w: status must be active or archived", ErrInvalidRepo)
	}
	var prevStatus string
	if p.Status != nil {
		m, err := uc.repo.GetRepositoriesByIDs(ctx, []string{id})
		if err != nil {
			return nil, err
		}
		if m[id] == nil {
			return nil, ErrRepoNotFound
		}
		prevStatus = m[id].Status
	}
	r, err := uc.repo.UpdateRepositoryMeta(ctx, id, p)
	if err != nil {
		return nil, err
	}
	if p.Status != nil && *p.Status != prevStatus {
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
	return repoView(r), nil
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
		out = append(out, &RepoGroupView{RepoGroup: g, RepoIDs: nonNilStrings(members[g.ID])})
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
	return &RepoGroupView{RepoGroup: g, RepoIDs: nonNilStrings(repoIDs)}, nil
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
// An unknown agent yields ErrAgentNotFound.
func (uc *RepoRegistryUsecase) ReplaceBindings(ctx context.Context, agentID string, in []*AgentRepoBinding, actor string) (*AgentRepoBindingsView, error) {
	defer uc.lockAgent(agentID)()
	return uc.replaceBindingsLocked(ctx, agentID, in, actor)
}

func (uc *RepoRegistryUsecase) requireAgent(ctx context.Context, agentID string) error {
	if _, err := uc.agents.GetByID(ctx, agentID); err != nil {
		if errors.Is(err, pkgErrors.ErrNotFound) || errors.Is(err, ErrAgentNotFound) {
			return ErrAgentNotFound
		}
		return err
	}
	return nil
}

// replaceBindingsLocked requires the caller to hold lockAgent(agentID).
func (uc *RepoRegistryUsecase) replaceBindingsLocked(ctx context.Context, agentID string, in []*AgentRepoBinding, actor string) (*AgentRepoBindingsView, error) {
	bs, err := normalizeRepoBindings(agentID, actor, in)
	if err != nil {
		return nil, err
	}
	if err := uc.requireAgent(ctx, agentID); err != nil {
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
	eff, err := uc.recomputeAgentLocked(ctx, agentID)
	if err != nil {
		return nil, err
	}
	return uc.bindingsView(ctx, bs, eff)
}

func (uc *RepoRegistryUsecase) CopyBindings(ctx context.Context, fromAgentID, toAgentID, actor string) (*AgentRepoBindingsView, error) {
	if err := uc.requireAgent(ctx, fromAgentID); err != nil {
		return nil, err
	}
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
	if bs == nil {
		bs = []*AgentRepoBinding{}
	}
	view := &AgentRepoBindingsView{Bindings: bs, Effective: make([]*EffectiveRepoView, 0, len(eff))}
	for _, e := range eff {
		if e.Via == nil {
			e.Via = []BindingRef{}
		}
		view.Effective = append(view.Effective, &EffectiveRepoView{AgentEffectiveRepo: e, Repository: repoView(repos[e.RepoID])})
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
// When allow is non-nil, agents it rejects are left out of both the report and apply.
func (uc *RepoRegistryUsecase) MigrateLegacyLinks(ctx context.Context, apply bool, allow func(context.Context, string) bool) ([]*LegacyLinkMigrationItem, error) {
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
	// agentRepo.List clamps page sizes above 100 to 10.
	const pageSize = 100
	for page := int32(1); ; page++ {
		if err := ctx.Err(); err != nil {
			return items, err
		}
		agents, total, err := uc.agents.List(ctx, page, pageSize)
		if err != nil {
			return items, err
		}
		for _, a := range agents {
			target := fwws.ResolveCodeMount(a.Workspace)
			if target == "" {
				continue
			}
			if allow != nil && !allow(ctx, a.ID) {
				continue
			}
			target = mapTargetToConfiguredRoot(target, uc.codeRoots)
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
			applied, err := uc.applyLegacyPlan(ctx, a.ID, plan, "legacy-migration")
			switch {
			case err != nil:
				item.Error = err.Error()
			case applied:
				item.Applied = true
			default:
				item.Action, item.Bindings = LegacyActionSkipHasBindings, nil
			}
		}
		if len(agents) == 0 || int(page)*pageSize >= total {
			return items, nil
		}
	}
}

// applyLegacyPlan writes plan's bindings unless the agent gained bindings meanwhile;
// applied=false with a nil error means the agent was already bound.
func (uc *RepoRegistryUsecase) applyLegacyPlan(ctx context.Context, agentID string, plan legacyPlan, actor string) (bool, error) {
	defer uc.lockAgent(agentID)()
	existing, err := uc.repo.ListAgentBindings(ctx, agentID)
	if err != nil || len(existing) > 0 {
		return false, err
	}
	if _, err := uc.replaceBindingsLocked(ctx, agentID, plan.Bindings, actor); err != nil {
		return false, err
	}
	return true, nil
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
	plan := planLegacyBinding(mapTargetToConfiguredRoot(target, uc.codeRoots), repos, groups)
	if !plan.AutoApply() {
		return false, nil
	}
	return uc.applyLegacyPlan(ctx, agentID, plan, actor)
}
