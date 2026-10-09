package biz

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"time"

	"backend/internal/handbook"

	"github.com/go-kratos/kratos/v2/log"
)

const (
	handbookLease        = 30 * time.Minute
	handbookKeepVersions = 2
	maxHandbookErrorLen  = 500
)

var (
	ErrHandbookBuilding = errors.New("handbook: build already running")
	ErrHandbookNotFound = errors.New("handbook: not found")
)

// HandbookBuilder builds the handbook files of one checkout.
type HandbookBuilder func(ctx context.Context, in handbook.BuildInput) (*handbook.Output, error)

// HandbookView is the API view of a repository handbook.
type HandbookView struct {
	RepoID     string         `json:"repo_id"`
	Status     string         `json:"status"`
	Commit     string         `json:"commit"`
	HeadCommit string         `json:"head_commit"`
	Version    int            `json:"version"`
	Stats      map[string]any `json:"stats,omitempty"`
	Pages      []string       `json:"pages"`
}

// HandbookUsecase builds repository handbooks and assembles per-agent handbook skill dirs.
type HandbookUsecase struct {
	repo      RepoRegistryRepo
	registry  *RepoRegistryUsecase
	store     handbook.Store
	build     HandbookBuilder
	staleMu   sync.Mutex
	codeMapMu sync.Mutex
	wg        sync.WaitGroup
	now       func() time.Time
	log       *log.Helper
}

func NewHandbookUsecase(repo RepoRegistryRepo, registry *RepoRegistryUsecase, dataRoot string, logger log.Logger) *HandbookUsecase {
	return &HandbookUsecase{
		repo: repo, registry: registry, store: handbook.Store{Root: filepath.Join(dataRoot, "handbooks")},
		build: handbook.Build, now: time.Now, log: log.NewHelper(logger),
	}
}

// SetBuilder replaces the handbook generator.
func (uc *HandbookUsecase) SetBuilder(b HandbookBuilder) { uc.build = b }

// Wait blocks until rebuilds started by RequestRebuild have finished.
func (uc *HandbookUsecase) Wait() { uc.wg.Wait() }

func handbookNeedsRebuild(r *Repository) bool {
	if r.Status != RepoStatusActive || r.HeadCommit == "" {
		return false
	}
	if r.HandbookStatus == HandbookStatusFailed {
		failed, _ := r.HandbookStats["failed_commit"].(string)
		return failed != r.HeadCommit
	}
	gen, _ := r.HandbookStats["generator_version"].(string)
	return r.HandbookCommit != r.HeadCommit || gen != handbook.GeneratorVersion
}

// RebuildStale rebuilds, one at a time, every active repo whose handbook lags its HEAD or
// the generator version. A commit that failed is not retried until HEAD moves. Concurrent
// calls return immediately. It returns the number of builds attempted.
func (uc *HandbookUsecase) RebuildStale(ctx context.Context) (int, error) {
	if !uc.staleMu.TryLock() {
		return 0, nil
	}
	defer uc.staleMu.Unlock()
	repos, err := uc.repo.ListRepositories(ctx, RepoFilter{Status: RepoStatusActive})
	if err != nil {
		return 0, err
	}
	n := 0
	for _, r := range repos {
		if err := ctx.Err(); err != nil {
			return n, err
		}
		if !handbookNeedsRebuild(r) {
			continue
		}
		ok, err := uc.claim(ctx, r.ID)
		if err != nil {
			return n, err
		}
		if !ok {
			continue
		}
		n++
		if err := uc.runClaimed(ctx, r.ID); err != nil {
			uc.log.Warnf("handbook rebuild %s: %v", r.RelPath, err)
		}
	}
	return n, nil
}

// RequestRebuild starts an asynchronous rebuild of one active repo regardless of staleness.
func (uc *HandbookUsecase) RequestRebuild(ctx context.Context, id string) error {
	r, err := uc.getRepo(ctx, id)
	if err != nil {
		return err
	}
	if r.Status != RepoStatusActive {
		return fmt.Errorf("%w: repository is %s", ErrInvalidRepo, r.Status)
	}
	ok, err := uc.claim(ctx, id)
	if err != nil {
		return err
	}
	if !ok {
		return ErrHandbookBuilding
	}
	uc.wg.Add(1)
	go func() {
		defer uc.wg.Done()
		bctx, cancel := context.WithTimeout(context.Background(), handbookLease)
		defer cancel()
		if err := uc.runClaimed(bctx, id); err != nil {
			uc.log.Warnf("handbook rebuild %s: %v", id, err)
		}
	}()
	return nil
}

func (uc *HandbookUsecase) claim(ctx context.Context, id string) (bool, error) {
	now := uc.now()
	return uc.repo.ClaimHandbookBuild(ctx, id, now, now.Add(handbookLease))
}

// runClaimed builds and publishes under a held lease. The row is re-read after the claim so
// the next version number cannot collide with a build that finished in between.
func (uc *HandbookUsecase) runClaimed(ctx context.Context, id string) error {
	start := uc.now()
	r, err := uc.getRepo(ctx, id)
	if err != nil {
		return uc.finishFailed(ctx, &Repository{ID: id}, err)
	}
	root, err := uc.registry.ResolveRepoPath(r)
	if err != nil {
		return uc.finishFailed(ctx, r, err)
	}
	out, err := uc.build(ctx, handbook.BuildInput{RepoID: r.ID, RelPath: r.RelPath, Root: root, Commit: r.HeadCommit, Now: start})
	if err != nil {
		return uc.finishFailed(ctx, r, err)
	}
	version := r.HandbookVersion + 1
	if err := uc.store.Publish(r.ID, version, out.Files, handbookKeepVersions); err != nil {
		return uc.finishFailed(ctx, r, err)
	}
	stats := out.Stats.Map()
	stats["duration_ms"] = uc.now().Sub(start).Milliseconds()
	return uc.repo.FinishHandbookBuild(context.WithoutCancel(ctx), r.ID, HandbookBuildResult{
		Status: HandbookStatusReady, Commit: r.HeadCommit, Version: version, Stats: stats,
	})
}

func (uc *HandbookUsecase) finishFailed(ctx context.Context, r *Repository, cause error) error {
	stats := make(map[string]any, len(r.HandbookStats)+2)
	for k, v := range r.HandbookStats {
		stats[k] = v
	}
	msg := cause.Error()
	if len(msg) > maxHandbookErrorLen {
		msg = msg[:maxHandbookErrorLen]
	}
	stats["last_error"] = msg
	stats["failed_commit"] = r.HeadCommit
	if err := uc.repo.FinishHandbookBuild(context.WithoutCancel(ctx), r.ID, HandbookBuildResult{Status: HandbookStatusFailed, Stats: stats}); err != nil {
		return errors.Join(cause, err)
	}
	return cause
}

func (uc *HandbookUsecase) getRepo(ctx context.Context, id string) (*Repository, error) {
	m, err := uc.repo.GetRepositoriesByIDs(ctx, []string{id})
	if err != nil {
		return nil, err
	}
	r := m[id]
	if r == nil {
		return nil, ErrRepoNotFound
	}
	return r, nil
}

// GetHandbook returns the handbook state and page list of the published version.
func (uc *HandbookUsecase) GetHandbook(ctx context.Context, id string) (*HandbookView, error) {
	r, err := uc.getRepo(ctx, id)
	if err != nil {
		return nil, err
	}
	v := &HandbookView{
		RepoID: r.ID, Status: r.HandbookStatus, Commit: r.HandbookCommit, HeadCommit: r.HeadCommit,
		Version: r.HandbookVersion, Stats: r.HandbookStats, Pages: []string{},
	}
	if r.HandbookVersion > 0 {
		pages, err := uc.store.ListSkillFiles(r.ID, r.HandbookVersion)
		if err != nil {
			uc.log.Warnf("list handbook pages %s v%d: %v", r.ID, r.HandbookVersion, err)
		} else {
			v.Pages = pages
		}
	}
	return v, nil
}

// ReadPage returns one page (path relative to the skill dir) of the published version.
func (uc *HandbookUsecase) ReadPage(ctx context.Context, id, rel string) (string, error) {
	r, err := uc.getRepo(ctx, id)
	if err != nil {
		return "", err
	}
	if r.HandbookVersion == 0 {
		return "", ErrHandbookNotFound
	}
	b, err := uc.store.ReadSkillFile(r.ID, r.HandbookVersion, rel)
	switch {
	case errors.Is(err, handbook.ErrBadPath):
		return "", fmt.Errorf("%w: %v", ErrInvalidRepo, err)
	case errors.Is(err, fs.ErrNotExist):
		return "", ErrHandbookNotFound
	case err != nil:
		return "", err
	}
	return string(b), nil
}

// SkillDirsForAgent returns the agent's code-map skill dir followed by the handbook skill
// dirs of its effective repos. nil means the agent has no repo bindings.
func (uc *HandbookUsecase) SkillDirsForAgent(ctx context.Context, agentID string) ([]string, error) {
	bound, repos, err := uc.registry.EffectiveRepositories(ctx, agentID)
	if err != nil || !bound {
		return nil, err
	}
	var dirs []string
	entries := make([]handbook.CodeMapEntry, 0, len(repos))
	for _, e := range repos {
		entry := handbook.CodeMapEntry{RelPath: e.Repo.RelPath, Description: e.Repo.Description, SubPaths: e.SubPaths}
		if e.Repo.HandbookVersion > 0 {
			d := uc.store.SkillDir(e.Repo.ID, e.Repo.HandbookVersion)
			if _, err := os.Stat(filepath.Join(d, "SKILL.md")); err == nil {
				dirs = append(dirs, d)
				entry.SkillName = handbook.SkillName(e.Repo.RelPath)
			}
		}
		entries = append(entries, entry)
	}
	cm, err := uc.writeCodeMap(agentID, handbook.RenderCodeMap(entries))
	if err != nil {
		return dirs, err
	}
	return append([]string{cm}, dirs...), nil
}

func (uc *HandbookUsecase) writeCodeMap(agentID, content string) (string, error) {
	if agentID == "" || filepath.Base(agentID) != agentID || agentID == "." || agentID == ".." {
		return "", fmt.Errorf("handbook: invalid agent id %q", agentID)
	}
	dir := filepath.Join(uc.store.Root, "agents", agentID, handbook.CodeMapSkillName)
	p := filepath.Join(dir, "SKILL.md")
	uc.codeMapMu.Lock()
	defer uc.codeMapMu.Unlock()
	if old, err := os.ReadFile(p); err == nil && string(old) == content {
		return dir, nil
	}
	if err := handbook.WriteFileAtomic(p, []byte(content)); err != nil {
		return "", err
	}
	return dir, nil
}
