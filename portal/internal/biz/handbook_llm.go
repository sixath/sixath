package biz

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"backend/internal/handbook"

	"github.com/sixath/framework/model"
)

// handbookLLMLeaseSlack is how long the LLM lease outlives the run timeout.
const handbookLLMLeaseSlack = 10 * time.Minute

var (
	ErrHandbookLLMDisabled = errors.New("handbook: LLM layer is disabled for this repository")
	ErrHandbookNotReady    = errors.New("handbook: deterministic handbook is not current")
	ErrHandbookLLMBusy     = errors.New("handbook: another LLM run is in progress")
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

// handbookLLMSettings is one immutable version of the LLM configuration.
type handbookLLMSettings struct {
	cfg     HandbookLLMConfig
	resolve HandbookModelResolver
	// runTimeout bounds one run and the run starts of one EnrichPending call.
	runTimeout time.Duration
}

// SetLLM configures the LLM layer; a nil resolver disables it.
func (uc *HandbookUsecase) SetLLM(cfg HandbookLLMConfig, resolve HandbookModelResolver) {
	cfg = cfg.withDefaults()
	uc.llm.Store(&handbookLLMSettings{cfg: cfg, resolve: resolve, runTimeout: time.Duration(cfg.MaxRunMinutes) * time.Minute})
}

// SetEnrichRunTimeout overrides MaxRunMinutes of the current configuration with a finer duration.
func (uc *HandbookUsecase) SetEnrichRunTimeout(d time.Duration) {
	s := *uc.llm.Load()
	s.runTimeout = d
	uc.llm.Store(&s)
}

// LLMConfig returns the effective LLM configuration.
func (uc *HandbookUsecase) LLMConfig() HandbookLLMConfig { return uc.llm.Load().cfg }

// LLMAvailable reports whether a model resolver is installed; without one no repository,
// override or not, runs the LLM layer.
func (uc *HandbookUsecase) LLMAvailable() bool { return uc.llm.Load().resolve != nil }

// modelFor is the model name s uses for r; "" means its LLM layer is disabled.
func modelFor(s *handbookLLMSettings, r *Repository) string {
	switch {
	case s.resolve == nil || strings.EqualFold(r.HandbookModel, HandbookModelOff):
		return ""
	case r.HandbookModel != "":
		return r.HandbookModel
	}
	return s.cfg.Model
}

// expectedLLMRev is the LLM revision a current deterministic build of r renders; "" when its
// LLM layer is disabled or has not produced anything yet, matching stats without llm_rev.
func expectedLLMRev(s *handbookLLMSettings, r *Repository) string {
	if modelFor(s, r) == "" {
		return ""
	}
	rev, _ := r.HandbookLLM["rev"].(string)
	return rev
}

// handbookCurrent reports whether r's published handbook is built from HEAD by the current
// generator, which an LLM run reads its facts from.
func handbookCurrent(r *Repository) bool {
	gen, _ := r.HandbookStats["generator_version"].(string)
	return r.HandbookVersion > 0 && r.HandbookCommit != "" && r.HandbookCommit == r.HeadCommit && gen == handbook.GeneratorVersion
}

func llmTime(r *Repository, key string) (time.Time, bool) {
	s, _ := r.HandbookLLM[key].(string)
	t, err := time.Parse(time.RFC3339, s)
	return t, err == nil
}

// needsEnrich reports whether an LLM run is due: the deterministic handbook is current, no
// LLM run holds the lease, and the last run is unfinished, for another commit or prompt
// version, or its skeleton is due for a rebuild or a fallback retry. A failed run waits for
// HEAD or the model to change.
func needsEnrich(s *handbookLLMSettings, r *Repository, now time.Time) bool {
	name := modelFor(s, r)
	if name == "" || r.Status != RepoStatusActive || r.HandbookStatus == HandbookStatusBuilding || !handbookCurrent(r) {
		return false
	}
	if r.HandbookLLMLeaseUntil != nil && r.HandbookLLMLeaseUntil.After(now) {
		return false
	}
	state, _ := r.HandbookLLM["state"].(string)
	if state == handbook.LLMStateFailed {
		failed, _ := r.HandbookLLM["failed_commit"].(string)
		used, _ := r.HandbookLLM["model"].(string)
		return failed != r.HandbookCommit || used != name
	}
	commit, _ := r.HandbookLLM["commit"].(string)
	pv, _ := r.HandbookLLM["prompt_version"].(string)
	if state != handbook.LLMStateComplete || commit != r.HandbookCommit || pv != handbook.LLMPromptVersion {
		return true
	}
	built, ok := llmTime(r, "skeleton_built_at")
	if !ok {
		return false
	}
	age := now.Sub(built)
	if age > time.Duration(s.cfg.SkeletonRebuildDays)*24*time.Hour {
		return true
	}
	fallback, _ := r.HandbookLLM["fallback"].(bool)
	reason, _ := r.HandbookLLM["fallback_reason"].(string)
	return fallback && reason == handbook.FallbackBadReply && age > handbook.FallbackRetryInterval
}

// EnrichPending runs, one at a time, the LLM layer of active repos that need it, the least
// recently run first (never run before all others). It starts no further run once the call
// has taken the run timeout, leaving the rest to the next call. Concurrent calls return
// immediately. It returns the number of runs attempted.
func (uc *HandbookUsecase) EnrichPending(ctx context.Context) (int, error) {
	if !uc.enrichMu.TryLock() {
		return 0, nil
	}
	defer uc.enrichMu.Unlock()
	start := uc.now()
	s := uc.llm.Load()
	repos, err := uc.repo.ListRepositories(ctx, RepoFilter{Status: RepoStatusActive})
	if err != nil {
		return 0, err
	}
	var due []*Repository
	for _, r := range repos {
		if needsEnrich(s, r, start.UTC()) {
			due = append(due, r)
		}
	}
	sort.SliceStable(due, func(i, j int) bool {
		a, _ := llmTime(due[i], "run_at")
		b, _ := llmTime(due[j], "run_at")
		return a.Before(b)
	})
	n := 0
	for _, r := range due {
		if err := ctx.Err(); err != nil {
			return n, err
		}
		if uc.now().Sub(start) > s.runTimeout {
			break
		}
		select {
		case uc.enrichSlots <- struct{}{}:
		case <-ctx.Done():
			return n, ctx.Err()
		}
		token, ok, err := uc.claimEnrich(ctx, s, r.ID)
		if err != nil || !ok {
			<-uc.enrichSlots
			if err != nil {
				return n, err
			}
			continue
		}
		n++
		uc.enrichInSlot(ctx, s, r.ID, token, false)
	}
	return n, nil
}

// RequestEnrich starts an asynchronous LLM run of one repo regardless of its LLM state; full
// rebuilds the skeleton and retries failed cards. It returns ErrHandbookBuilding when an LLM
// run holds the repo's lease and ErrHandbookLLMBusy when another repo's run is in progress.
func (uc *HandbookUsecase) RequestEnrich(ctx context.Context, id string, full bool) error {
	s := uc.llm.Load()
	r, err := uc.getRepo(ctx, id)
	if err != nil {
		return err
	}
	if r.Status != RepoStatusActive {
		return fmt.Errorf("%w: repository is %s", ErrInvalidRepo, r.Status)
	}
	if modelFor(s, r) == "" {
		return ErrHandbookLLMDisabled
	}
	if !handbookCurrent(r) {
		return ErrHandbookNotReady
	}
	if r.HandbookLLMLeaseUntil != nil && r.HandbookLLMLeaseUntil.After(uc.now().UTC()) {
		return ErrHandbookBuilding
	}
	select {
	case uc.enrichSlots <- struct{}{}:
	default:
		return ErrHandbookLLMBusy
	}
	token, ok, err := uc.claimEnrich(ctx, s, id)
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
		uc.enrichInSlot(context.WithoutCancel(ctx), s, id, token, full)
	}()
	return nil
}

func (uc *HandbookUsecase) claimEnrich(ctx context.Context, s *handbookLLMSettings, id string) (string, bool, error) {
	now := uc.now().UTC()
	return uc.repo.ClaimHandbookEnrich(ctx, id, now, now.Add(s.runTimeout+handbookLLMLeaseSlack))
}

// enrichInSlot runs one claimed LLM run and frees its slot.
func (uc *HandbookUsecase) enrichInSlot(ctx context.Context, s *handbookLLMSettings, id, token string, full bool) {
	defer func() { <-uc.enrichSlots }()
	if err := uc.runEnrichClaimed(ctx, s, id, token, full); err != nil {
		uc.log.Warnf("handbook enrich %s: %v", id, err)
	}
}

// runEnrichClaimed runs the LLM layer under a held lease, bounded by the run timeout, and
// stores its state. A run cancelled through ctx, or whose repo cannot be read or no longer
// has a current handbook, releases the lease without recording anything. Local errors before
// the model runs record last_error only, so the next pass retries; a model that cannot be
// resolved or a failed run is recorded as failed. A run that changed the cache gets a new
// rev and a deterministic re-render.
func (uc *HandbookUsecase) runEnrichClaimed(ctx context.Context, s *handbookLLMSettings, id, token string, full bool) error {
	start := uc.now()
	r, err := uc.getRepo(ctx, id)
	if err != nil {
		return errors.Join(err, uc.releaseEnrich(ctx, id, token))
	}
	name := modelFor(s, r)
	if name == "" || r.Status != RepoStatusActive || !handbookCurrent(r) {
		return uc.releaseEnrich(ctx, id, token)
	}
	prevRev, _ := r.HandbookLLM["rev"].(string)
	stamp := func(llm map[string]any, cause error) map[string]any {
		if llm == nil {
			llm = make(map[string]any, len(r.HandbookLLM)+4)
			for k, v := range r.HandbookLLM {
				llm[k] = v
			}
		}
		if cause != nil {
			llm["last_error"] = truncateRunes(cause.Error(), maxHandbookErrorLen)
		}
		llm["run_at"] = start.UTC().Format(time.RFC3339)
		llm["duration_ms"] = uc.now().Sub(start).Milliseconds()
		return llm
	}
	retryLater := func(cause error) error {
		return errors.Join(cause, uc.storeEnrich(ctx, id, token, stamp(nil, cause), prevRev))
	}
	fail := func(cause error, res *handbook.EnrichResult) error {
		llm := stamp(nil, cause)
		llm["state"] = handbook.LLMStateFailed
		llm["failed_commit"] = r.HandbookCommit
		llm["model"] = name
		if res != nil && res.Changed {
			llm["rev"] = uc.newLLMRev()
		}
		return errors.Join(cause, uc.storeEnrich(ctx, id, token, llm, prevRev))
	}
	m, err := s.resolve(ctx, name)
	if err != nil {
		return fail(fmt.Errorf("解析模型 %s: %w", name, err), nil)
	}
	facts, err := uc.store.ReadFacts(r.ID, r.HandbookVersion)
	if err != nil {
		return retryLater(err)
	}
	root, err := uc.registry.ResolveRepoPath(r)
	if err != nil {
		return retryLater(err)
	}
	cache, err := uc.store.LLMCache(r.ID)
	if err != nil {
		return retryLater(err)
	}
	rctx, cancel := context.WithTimeout(ctx, s.runTimeout)
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
		return fail(err, res)
	}
	if res.PruneError != nil {
		uc.log.Warnf("handbook enrich %s: prune cards: %v", id, res.PruneError)
	}
	llm := stamp(map[string]any{
		"state": res.State, "model": name, "commit": r.HandbookCommit, "prompt_version": handbook.LLMPromptVersion,
		"cards_total": res.CardsTotal, "cards_done": res.CardsDone, "cards_new": res.CardsNew,
		"card_errors": res.CardErrors, "card_transport_errors": res.CardTransportErrors,
		"stages": res.Stages, "fallback": res.Fallback, "fallback_reason": res.FallbackReason,
		"skeleton_rebuilt": res.SkeletonRebuilt, "rebuild_reason": res.RebuildReason,
		"tokens_in": res.TokensIn, "tokens_out": res.TokensOut,
	}, nil)
	if res.State != handbook.LLMStateComplete {
		// Synthesis did not run: the cached skeleton, and what describes it, is unchanged.
		for _, k := range []string{"stages", "fallback", "fallback_reason"} {
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
	llm["rev"] = prevRev
	if res.Changed || prevRev == "" {
		llm["rev"] = uc.newLLMRev()
	}
	return uc.storeEnrich(ctx, id, token, llm, prevRev)
}

func (uc *HandbookUsecase) newLLMRev() string { return strconv.FormatInt(uc.now().UnixNano(), 36) }

// storeEnrich records llm and releases the lease, then re-renders the handbook when the rev
// moved. A result whose lease was taken over is dropped.
func (uc *HandbookUsecase) storeEnrich(ctx context.Context, id, token string, llm map[string]any, prevRev string) error {
	err := uc.repo.FinishHandbookEnrich(context.WithoutCancel(ctx), id, token, llm)
	if errors.Is(err, ErrHandbookLeaseLost) {
		uc.log.Warnf("handbook enrich %s: lease lost, dropping %v result", id, llm["state"])
		return nil
	}
	if err != nil {
		return err
	}
	if rev, _ := llm["rev"].(string); rev != prevRev {
		if err := uc.RequestRebuild(context.WithoutCancel(ctx), id); err != nil && !errors.Is(err, ErrHandbookBuilding) {
			uc.log.Warnf("handbook re-render %s: %v", id, err)
		}
	}
	return nil
}

func (uc *HandbookUsecase) releaseEnrich(ctx context.Context, id, token string) error {
	err := uc.repo.ReleaseHandbookEnrich(context.WithoutCancel(ctx), id, token)
	if errors.Is(err, ErrHandbookLeaseLost) {
		return nil
	}
	return err
}
