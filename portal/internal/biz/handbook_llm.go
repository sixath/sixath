package biz

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"backend/internal/handbook"

	"github.com/sixath/framework/model"
)

// handbookLLMLeaseSlack is how long the LLM lease outlives the run timeout.
const handbookLLMLeaseSlack = 10 * time.Minute

var (
	ErrHandbookLLMDisabled = errors.New("handbook: LLM layer is disabled for this repository")
	ErrHandbookNotReady    = errors.New("handbook: deterministic handbook is not current")
)

// HandbookLLMConfig is the handbook: section of the portal config.
type HandbookLLMConfig struct {
	Model               string `json:"model"`                 // "" disables the LLM layer unless a repo overrides it
	Concurrency         int    `json:"concurrency"`           // model calls in flight per run, default 4, max 16
	MaxCardsPerRun      int    `json:"max_cards_per_run"`     // default 600
	MaxFileKB           int    `json:"max_file_kb"`           // file content sent per card, default 24, max 256
	MaxRunMinutes       int    `json:"max_run_minutes"`       // default 15, max 20 (lease = run + 10 min)
	SkeletonRebuildDays int    `json:"skeleton_rebuild_days"` // default 30
}

func (c HandbookLLMConfig) withDefaults() HandbookLLMConfig {
	clamp := func(v, def, max int) int {
		if v <= 0 {
			return def
		}
		if max > 0 && v > max {
			return max
		}
		return v
	}
	c.Concurrency = clamp(c.Concurrency, 4, 16)
	c.MaxCardsPerRun = clamp(c.MaxCardsPerRun, 600, 0)
	c.MaxFileKB = clamp(c.MaxFileKB, 24, 256)
	c.MaxRunMinutes = clamp(c.MaxRunMinutes, 15, 20)
	c.SkeletonRebuildDays = clamp(c.SkeletonRebuildDays, 30, 0)
	return c
}

// HandbookModelResolver turns a model name ("model" or "provider/model") into a model client.
type HandbookModelResolver func(ctx context.Context, name string) (model.Model, error)

type handbookLLMSettings struct {
	cfg     HandbookLLMConfig
	resolve HandbookModelResolver
}

// SetLLM configures the LLM layer; a nil resolver disables it.
func (uc *HandbookUsecase) SetLLM(cfg HandbookLLMConfig, resolve HandbookModelResolver) {
	uc.llm.Store(&handbookLLMSettings{cfg: cfg.withDefaults(), resolve: resolve})
}

// LLMConfig returns the effective LLM configuration.
func (uc *HandbookUsecase) LLMConfig() HandbookLLMConfig { return uc.llm.Load().cfg }

// modelFor is the model name used for r; "" means its LLM layer is disabled.
func (uc *HandbookUsecase) modelFor(r *Repository) string {
	s := uc.llm.Load()
	switch {
	case s.resolve == nil || r.HandbookModel == HandbookModelOff:
		return ""
	case r.HandbookModel != "":
		return r.HandbookModel
	}
	return s.cfg.Model
}

// expectedLLMRev is the LLM revision a current deterministic build of r renders; "" when its
// LLM layer is disabled or has not produced anything yet, matching stats without llm_rev.
func (uc *HandbookUsecase) expectedLLMRev(r *Repository) string {
	if uc.modelFor(r) == "" {
		return ""
	}
	rev, _ := r.HandbookLLM["rev"].(string)
	return rev
}

// needsEnrich reports whether an LLM run is due: the deterministic handbook is current, no
// LLM run holds the lease, and the last run is unfinished, for another commit or prompt
// version, or its skeleton is due for a rebuild. A failed commit waits for HEAD to move.
func (uc *HandbookUsecase) needsEnrich(r *Repository, now time.Time) bool {
	if uc.modelFor(r) == "" || r.Status != RepoStatusActive || r.HandbookVersion == 0 ||
		r.HandbookCommit == "" || r.HandbookCommit != r.HeadCommit || r.HandbookStatus == HandbookStatusBuilding {
		return false
	}
	if r.HandbookLLMLeaseUntil != nil && r.HandbookLLMLeaseUntil.After(now) {
		return false
	}
	state, _ := r.HandbookLLM["state"].(string)
	if state == handbook.LLMStateFailed {
		failed, _ := r.HandbookLLM["failed_commit"].(string)
		return failed != r.HandbookCommit
	}
	commit, _ := r.HandbookLLM["commit"].(string)
	pv, _ := r.HandbookLLM["prompt_version"].(string)
	if state != handbook.LLMStateComplete || commit != r.HandbookCommit || pv != handbook.LLMPromptVersion {
		return true
	}
	if ts, _ := r.HandbookLLM["skeleton_built_at"].(string); ts != "" {
		days := uc.llm.Load().cfg.SkeletonRebuildDays
		if t, err := time.Parse(time.RFC3339, ts); err == nil && now.Sub(t) > time.Duration(days)*24*time.Hour {
			return true
		}
	}
	return false
}

// EnrichPending runs, one at a time, the LLM layer of every active repo that needs it.
// Concurrent calls return immediately. It returns the number of runs attempted.
func (uc *HandbookUsecase) EnrichPending(ctx context.Context) (int, error) {
	if !uc.enrichMu.TryLock() {
		return 0, nil
	}
	defer uc.enrichMu.Unlock()
	repos, err := uc.repo.ListRepositories(ctx, RepoFilter{Status: RepoStatusActive})
	if err != nil {
		return 0, err
	}
	n := 0
	for _, r := range repos {
		if err := ctx.Err(); err != nil {
			return n, err
		}
		if !uc.needsEnrich(r, uc.now().UTC()) {
			continue
		}
		select {
		case uc.enrichSlots <- struct{}{}:
		case <-ctx.Done():
			return n, ctx.Err()
		}
		token, ok, err := uc.claimEnrich(ctx, r.ID)
		if err != nil || !ok {
			<-uc.enrichSlots
			if err != nil {
				return n, err
			}
			continue
		}
		n++
		uc.enrichInSlot(ctx, r.ID, token, false)
	}
	return n, nil
}

// RequestEnrich starts an asynchronous LLM run of one repo regardless of its LLM state; full
// rebuilds the skeleton and retries failed cards. It returns ErrHandbookBuilding when an LLM
// run holds the repo's lease or another LLM run is in progress.
func (uc *HandbookUsecase) RequestEnrich(ctx context.Context, id string, full bool) error {
	r, err := uc.getRepo(ctx, id)
	if err != nil {
		return err
	}
	if r.Status != RepoStatusActive {
		return fmt.Errorf("%w: repository is %s", ErrInvalidRepo, r.Status)
	}
	if uc.modelFor(r) == "" {
		return ErrHandbookLLMDisabled
	}
	if r.HandbookVersion == 0 || r.HandbookCommit != r.HeadCommit {
		return ErrHandbookNotReady
	}
	select {
	case uc.enrichSlots <- struct{}{}:
	default:
		return ErrHandbookBuilding
	}
	token, ok, err := uc.claimEnrich(ctx, id)
	if err != nil || !ok {
		<-uc.enrichSlots
		if err != nil {
			return err
		}
		return ErrHandbookBuilding
	}
	uc.wg.Add(1)
	go func() {
		defer uc.wg.Done()
		uc.enrichInSlot(context.WithoutCancel(ctx), id, token, full)
	}()
	return nil
}

func (uc *HandbookUsecase) claimEnrich(ctx context.Context, id string) (string, bool, error) {
	now := uc.now().UTC()
	lease := time.Duration(uc.llm.Load().cfg.MaxRunMinutes)*time.Minute + handbookLLMLeaseSlack
	return uc.repo.ClaimHandbookEnrich(ctx, id, now, now.Add(lease))
}

// enrichInSlot runs one claimed LLM run and frees its slot.
func (uc *HandbookUsecase) enrichInSlot(ctx context.Context, id, token string, full bool) {
	defer func() { <-uc.enrichSlots }()
	if err := uc.runEnrichClaimed(ctx, id, token, full); err != nil {
		uc.log.Warnf("handbook enrich %s: %v", id, err)
	}
}

// runEnrichClaimed runs the LLM layer under a held lease, bounded by MaxRunMinutes, and
// stores its state. A run cancelled through ctx, or whose repo cannot be read or no longer
// has a current handbook, releases the lease without recording anything. A run that changed
// the cache gets a new rev and a deterministic re-render.
func (uc *HandbookUsecase) runEnrichClaimed(ctx context.Context, id, token string, full bool) error {
	s := uc.llm.Load()
	start := uc.now()
	r, err := uc.getRepo(ctx, id)
	if err != nil {
		return errors.Join(err, uc.releaseEnrich(ctx, id, token))
	}
	name := uc.modelFor(r)
	if name == "" || r.Status != RepoStatusActive || r.HandbookVersion == 0 || r.HandbookCommit != r.HeadCommit {
		return uc.releaseEnrich(ctx, id, token)
	}
	fail := func(cause error) error {
		llm := make(map[string]any, len(r.HandbookLLM)+4)
		for k, v := range r.HandbookLLM {
			llm[k] = v
		}
		llm["state"] = handbook.LLMStateFailed
		llm["last_error"] = truncateRunes(cause.Error(), maxHandbookErrorLen)
		llm["failed_commit"] = r.HandbookCommit
		llm["model"] = name
		llm["run_at"] = start.UTC().Format(time.RFC3339)
		llm["duration_ms"] = uc.now().Sub(start).Milliseconds()
		if err := uc.finishEnrich(ctx, id, token, llm); err != nil {
			return errors.Join(cause, err)
		}
		return cause
	}
	m, err := s.resolve(ctx, name)
	if err != nil {
		return fail(fmt.Errorf("解析模型 %s: %w", name, err))
	}
	facts, err := uc.store.ReadFacts(r.ID, r.HandbookVersion)
	if err != nil {
		return fail(err)
	}
	root, err := uc.registry.ResolveRepoPath(r)
	if err != nil {
		return fail(err)
	}
	cache, err := uc.store.LLMCache(r.ID)
	if err != nil {
		return fail(err)
	}
	rctx, cancel := context.WithTimeout(ctx, time.Duration(s.cfg.MaxRunMinutes)*time.Minute)
	defer cancel()
	res, err := handbook.Enrich(rctx, handbook.EnrichInput{
		RelPath: r.RelPath, Root: root, Commit: r.HandbookCommit, ModelName: name, Facts: facts, Cache: cache, Model: m, Now: start.UTC(),
		Opts: handbook.EnrichOptions{Concurrency: s.cfg.Concurrency, MaxCardsPerRun: s.cfg.MaxCardsPerRun,
			MaxFileBytes: s.cfg.MaxFileKB << 10, SkeletonRebuildDays: s.cfg.SkeletonRebuildDays, Full: full},
	})
	if ctx.Err() != nil {
		return errors.Join(err, uc.releaseEnrich(ctx, id, token))
	}
	if err != nil {
		return fail(err)
	}
	if res.PruneError != nil {
		uc.log.Warnf("handbook enrich %s: prune cards: %v", id, res.PruneError)
	}
	llm := map[string]any{
		"state": res.State, "model": name, "commit": r.HandbookCommit, "prompt_version": handbook.LLMPromptVersion,
		"cards_total": res.CardsTotal, "cards_done": res.CardsDone, "cards_new": res.CardsNew,
		"card_errors": res.CardErrors, "card_transport_errors": res.CardTransportErrors,
		"stages": res.Stages, "fallback": res.Fallback, "skeleton_rebuilt": res.SkeletonRebuilt, "rebuild_reason": res.RebuildReason,
		"tokens_in": res.TokensIn, "tokens_out": res.TokensOut,
		"run_at": start.UTC().Format(time.RFC3339), "duration_ms": uc.now().Sub(start).Milliseconds(),
	}
	if res.State != handbook.LLMStateComplete {
		// Synthesis did not run: the cached skeleton, and what describes it, is unchanged.
		for _, k := range []string{"stages", "fallback"} {
			if v, ok := r.HandbookLLM[k]; ok {
				llm[k] = v
			}
		}
	}
	if !res.SkeletonBuiltAt.IsZero() {
		llm["skeleton_built_at"] = res.SkeletonBuiltAt.UTC().Format(time.RFC3339)
	} else if v, ok := r.HandbookLLM["skeleton_built_at"]; ok {
		llm["skeleton_built_at"] = v
	}
	if res.LastTransportError != "" {
		llm["last_error"] = res.LastTransportError
	}
	prevRev, _ := r.HandbookLLM["rev"].(string)
	rev := prevRev
	if res.Changed || rev == "" {
		rev = strconv.FormatInt(uc.now().UnixNano(), 36)
	}
	llm["rev"] = rev
	if err := uc.finishEnrich(ctx, id, token, llm); err != nil {
		return err
	}
	if rev != prevRev {
		if err := uc.RequestRebuild(context.WithoutCancel(ctx), id); err != nil && !errors.Is(err, ErrHandbookBuilding) {
			uc.log.Warnf("handbook re-render %s: %v", id, err)
		}
	}
	return nil
}

// finishEnrich drops the result when another run has taken over the lease.
func (uc *HandbookUsecase) finishEnrich(ctx context.Context, id, token string, llm map[string]any) error {
	err := uc.repo.FinishHandbookEnrich(context.WithoutCancel(ctx), id, token, llm)
	if errors.Is(err, ErrHandbookLeaseLost) {
		uc.log.Warnf("handbook enrich %s: lease lost, dropping %v result", id, llm["state"])
		return nil
	}
	return err
}

func (uc *HandbookUsecase) releaseEnrich(ctx context.Context, id, token string) error {
	err := uc.repo.ReleaseHandbookEnrich(context.WithoutCancel(ctx), id, token)
	if errors.Is(err, ErrHandbookLeaseLost) {
		return nil
	}
	return err
}
