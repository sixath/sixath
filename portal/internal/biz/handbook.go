package biz

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"backend/internal/handbook"

	"github.com/go-kratos/kratos/v2/log"
)

const (
	handbookLease        = 30 * time.Minute
	handbookBuildTimeout = handbookLease - 5*time.Minute
	handbookKeepVersions = 2
	handbookMaxBuilds    = 2
	// handbookPublishAttempts bounds version bumps past directories left by builds that lost their lease.
	handbookPublishAttempts = 3
	maxHandbookErrorLen     = 500
)

var (
	ErrHandbookBuilding  = errors.New("handbook: build already running")
	ErrHandbookNotFound  = errors.New("handbook: not found")
	ErrHandbookLeaseLost = errors.New("handbook: build lease lost")
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
	// LLM is the handbook_llm state; LLMModel the model the repo uses ("" = LLM layer off).
	LLM        map[string]any `json:"llm,omitempty"`
	LLMModel   string         `json:"llm_model"`
	LLMRunning bool           `json:"llm_running"`
}

// HandbookUsecase builds repository handbooks and assembles per-agent handbook skill dirs.
type HandbookUsecase struct {
	repo      RepoRegistryRepo
	registry  *RepoRegistryUsecase
	store     handbook.Store
	build     HandbookBuilder
	staleMu   sync.Mutex
	codeMapMu sync.Mutex
	// slots caps concurrent builds across RebuildStale and RequestRebuild.
	slots        chan struct{}
	buildTimeout time.Duration
	llm          atomic.Pointer[handbookLLMSettings]
	enrichMu     sync.Mutex
	// enrichSlots caps concurrent LLM runs across EnrichPending and RequestEnrich.
	enrichSlots chan struct{}
	// wg covers the goroutines of RequestRebuild and RequestEnrich.
	wg  sync.WaitGroup
	now func() time.Time
	log *log.Helper
}

func NewHandbookUsecase(repo RepoRegistryRepo, registry *RepoRegistryUsecase, dataRoot string, logger log.Logger) *HandbookUsecase {
	uc := &HandbookUsecase{
		repo: repo, registry: registry, store: handbook.Store{Root: filepath.Join(dataRoot, "handbooks")},
		build: handbook.Build, slots: make(chan struct{}, handbookMaxBuilds), buildTimeout: handbookBuildTimeout,
		enrichSlots: make(chan struct{}, 1), now: time.Now, log: log.NewHelper(logger),
	}
	uc.SetLLM(HandbookLLMConfig{}, nil)
	return uc
}

// SetBuilder replaces the handbook generator.
func (uc *HandbookUsecase) SetBuilder(b HandbookBuilder) { uc.build = b }

// SetBuildTimeout overrides how long one build may run; it must stay below the lease.
func (uc *HandbookUsecase) SetBuildTimeout(d time.Duration) { uc.buildTimeout = d }

// Wait blocks until runs started by RequestRebuild and RequestEnrich, and the re-renders
// they trigger, have finished.
func (uc *HandbookUsecase) Wait() { uc.wg.Wait() }

// needsRebuild also picks up builds stuck in building after their lease expired, and
// re-renders when the LLM revision the handbook shows is not the expected one.
func (uc *HandbookUsecase) needsRebuild(r *Repository, now time.Time) bool {
	if r.Status != RepoStatusActive || r.HeadCommit == "" {
		return false
	}
	switch r.HandbookStatus {
	case HandbookStatusBuilding:
		return r.HandbookLeaseUntil == nil || r.HandbookLeaseUntil.Before(now)
	case HandbookStatusFailed:
		failed, _ := r.HandbookStats["failed_commit"].(string)
		return failed != r.HeadCommit
	}
	gen, _ := r.HandbookStats["generator_version"].(string)
	rev, _ := r.HandbookStats["llm_rev"].(string)
	return r.HandbookCommit != r.HeadCommit || gen != handbook.GeneratorVersion || rev != uc.expectedLLMRev(r)
}

// statusBeforeClaim is the status restored when a claimed build is released without an
// outcome. A stuck building status is not restored since it no longer has a lease.
func statusBeforeClaim(r *Repository) string {
	if r.HandbookStatus != HandbookStatusBuilding {
		return r.HandbookStatus
	}
	if r.HandbookVersion > 0 {
		return HandbookStatusReady
	}
	return HandbookStatusNone
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
		if !uc.needsRebuild(r, uc.now().UTC()) {
			continue
		}
		select {
		case uc.slots <- struct{}{}:
		case <-ctx.Done():
			return n, ctx.Err()
		}
		token, ok, err := uc.claim(ctx, r.ID)
		if err != nil || !ok {
			<-uc.slots
			if err != nil {
				return n, err
			}
			continue
		}
		n++
		uc.buildInSlot(ctx, r.ID, token, statusBeforeClaim(r))
	}
	return n, nil
}

// RequestRebuild starts an asynchronous rebuild of one active repo regardless of staleness.
// It returns ErrHandbookBuilding when the repo is already building or no build slot is free.
func (uc *HandbookUsecase) RequestRebuild(ctx context.Context, id string) error {
	r, err := uc.getRepo(ctx, id)
	if err != nil {
		return err
	}
	if r.Status != RepoStatusActive {
		return fmt.Errorf("%w: repository is %s", ErrInvalidRepo, r.Status)
	}
	select {
	case uc.slots <- struct{}{}:
	default:
		return ErrHandbookBuilding
	}
	token, ok, err := uc.claim(ctx, id)
	if err != nil || !ok {
		<-uc.slots
		if err != nil {
			return err
		}
		return ErrHandbookBuilding
	}
	uc.wg.Add(1)
	go func() {
		defer uc.wg.Done()
		uc.buildInSlot(context.WithoutCancel(ctx), id, token, statusBeforeClaim(r))
	}()
	return nil
}

// buildInSlot runs one claimed build and frees its slot.
func (uc *HandbookUsecase) buildInSlot(ctx context.Context, id, token, prevStatus string) {
	defer func() { <-uc.slots }()
	if err := uc.runClaimed(ctx, id, token, prevStatus); err != nil {
		uc.log.Warnf("handbook rebuild %s: %v", id, err)
	}
}

func (uc *HandbookUsecase) claim(ctx context.Context, id string) (string, bool, error) {
	now := uc.now().UTC()
	return uc.repo.ClaimHandbookBuild(ctx, id, now, now.Add(handbookLease))
}

// runClaimed builds and publishes under a held lease, bounded by the build timeout. The row
// is re-read after the claim so the next version number cannot collide with a build that
// finished in between. A build cancelled through ctx, or whose repo cannot be read or is no
// longer active, releases the lease without recording a failure; hitting the build timeout
// is recorded as a failure so the commit is not retried on every scan.
func (uc *HandbookUsecase) runClaimed(ctx context.Context, id, token, prevStatus string) error {
	bctx, cancel := context.WithTimeout(ctx, uc.buildTimeout)
	defer cancel()
	start := uc.now()
	r, err := uc.getRepo(bctx, id)
	if err != nil {
		return errors.Join(err, uc.release(ctx, id, token, prevStatus))
	}
	if r.Status != RepoStatusActive {
		return uc.release(ctx, id, token, prevStatus)
	}
	root, err := uc.registry.ResolveRepoPath(r)
	if err != nil {
		return uc.finishFailed(ctx, r, token, err)
	}
	in := handbook.BuildInput{RepoID: r.ID, RelPath: r.RelPath, Root: root, Commit: r.HeadCommit, Now: start, LLMRev: uc.expectedLLMRev(r)}
	if uc.modelFor(r) != "" {
		if c, err := uc.store.LLMCache(r.ID); err == nil {
			in.LLMDir = c.Dir
		}
	}
	out, err := uc.build(bctx, in)
	if err != nil {
		switch {
		case ctx.Err() != nil:
			return errors.Join(err, uc.release(ctx, id, token, prevStatus))
		case errors.Is(bctx.Err(), context.DeadlineExceeded):
			err = fmt.Errorf("构建超时（%s）: %w", formatBuildTimeout(uc.buildTimeout), err)
		}
		return uc.finishFailed(ctx, r, token, err)
	}
	version := r.HandbookVersion + 1
	for attempt := 1; ; attempt++ {
		err = uc.store.Publish(r.ID, version, out.Files, handbookKeepVersions)
		if !errors.Is(err, handbook.ErrVersionExists) || attempt == handbookPublishAttempts {
			break
		}
		version++
	}
	if err != nil {
		return uc.finishFailed(ctx, r, token, err)
	}
	stats := out.Stats.Map()
	stats["duration_ms"] = uc.now().Sub(start).Milliseconds()
	return uc.finish(ctx, r.ID, token, HandbookBuildResult{
		Status: HandbookStatusReady, Commit: r.HeadCommit, Version: version, Stats: stats,
	})
}

func (uc *HandbookUsecase) finishFailed(ctx context.Context, r *Repository, token string, cause error) error {
	stats := make(map[string]any, len(r.HandbookStats)+2)
	for k, v := range r.HandbookStats {
		stats[k] = v
	}
	stats["last_error"] = truncateRunes(cause.Error(), maxHandbookErrorLen)
	stats["failed_commit"] = r.HeadCommit
	if err := uc.finish(ctx, r.ID, token, HandbookBuildResult{Status: HandbookStatusFailed, Stats: stats}); err != nil {
		return errors.Join(cause, err)
	}
	return cause
}

// finish drops the result when another build has taken over the lease.
func (uc *HandbookUsecase) finish(ctx context.Context, id, token string, res HandbookBuildResult) error {
	err := uc.repo.FinishHandbookBuild(context.WithoutCancel(ctx), id, token, res)
	if errors.Is(err, ErrHandbookLeaseLost) {
		uc.log.Warnf("handbook rebuild %s: lease lost, dropping %s result", id, res.Status)
		return nil
	}
	return err
}

func (uc *HandbookUsecase) release(ctx context.Context, id, token, status string) error {
	err := uc.repo.ReleaseHandbookBuild(context.WithoutCancel(ctx), id, token, status)
	if errors.Is(err, ErrHandbookLeaseLost) {
		return nil
	}
	return err
}

func formatBuildTimeout(d time.Duration) string {
	if d >= time.Minute && d%time.Minute == 0 {
		return fmt.Sprintf("%d 分钟", int(d/time.Minute))
	}
	return d.String()
}

func truncateRunes(s string, n int) string {
	i := 0
	for j := range s {
		if i == n {
			return s[:j]
		}
		i++
	}
	return s
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
		LLM: r.HandbookLLM, LLMModel: uc.modelFor(r),
		LLMRunning: r.HandbookLLMLeaseUntil != nil && r.HandbookLLMLeaseUntil.After(uc.now().UTC()),
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
	if uc == nil {
		return nil, nil
	}
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
