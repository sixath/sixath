package handbook

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/sixath/framework/model"
)

const (
	LLMStatePartial  = "partial"
	LLMStateComplete = "complete"
	LLMStateFailed   = "failed"

	maxConsecutiveCallErrors = 5
	synthesisRetries         = 2
	// cardTransportRounds runs with a failed card call put a hash in the failed set, so a card
	// the provider always rejects cannot hold back synthesis.
	cardTransportRounds = 3
	maxErrorRunes       = 300
)

// ErrModelUnavailable aborts a run whose model calls keep failing.
var ErrModelUnavailable = errors.New("handbook: model calls keep failing")

// EnrichOptions bounds one LLM run; zero values take defaults.
type EnrichOptions struct {
	Concurrency         int
	MaxCardsPerRun      int // card model calls; files with identical content share one
	MaxFileBytes        int
	SkeletonRebuildDays int
	Full                bool          // rebuild the skeleton and retry failed cards regardless of thresholds
	RetryBackoff        time.Duration // base delay between retries of a failed synthesis call
}

func (o EnrichOptions) withDefaults() EnrichOptions {
	if o.Concurrency <= 0 {
		o.Concurrency = 4
	}
	if o.MaxCardsPerRun <= 0 {
		o.MaxCardsPerRun = 600
	}
	if o.MaxFileBytes <= 0 {
		o.MaxFileBytes = 24 << 10
	}
	if o.SkeletonRebuildDays <= 0 {
		o.SkeletonRebuildDays = 30
	}
	if o.RetryBackoff <= 0 {
		o.RetryBackoff = time.Second
	}
	return o
}

// EnrichInput is one LLM run over the facts of a published version.
type EnrichInput struct {
	RelPath   string
	Root      string // repository checkout; files whose content no longer matches Facts are skipped
	Commit    string
	ModelName string
	Facts     *Facts
	Cache     LLMCache
	Model     model.Model
	Opts      EnrichOptions
	Now       time.Time
}

// EnrichResult summarizes one run. Card counts are in files; files with identical content
// share one card.
type EnrichResult struct {
	State               string
	CardsTotal          int
	CardsDone           int
	CardsNew            int
	CardErrors          int    // files whose card failed for good (unusable reply or persistent transport errors); retried when their content changes
	CardTransportErrors int    // files whose card call failed this run and will be retried next run
	LastTransportError  string // last failed model call of the run (card or synthesis), clipped
	Stages              int
	Fallback            bool
	FallbackReason      string // Skeleton.FallbackReason when Fallback
	SkeletonRebuilt     bool
	RebuildReason       string
	SkeletonBuiltAt     time.Time
	TokensIn            int64
	TokensOut           int64
	Changed             bool  // cache content changed; the handbook needs a re-render
	PruneError          error // cleanup of unused cards failed; the run itself succeeded
}

// cardJob is one model call: a content hash and the files that have it.
type cardJob struct {
	hash  string
	files []File
}

// Enrich generates missing file cards within budget and, once every card-eligible file has
// been attempted, synthesizes the skeleton. Cancelling ctx stops the run and reports partial
// progress without error; everything already generated, including synthesis checkpoints,
// stays cached. Any other error reports state failed; local cache errors wrap ErrCacheWrite
// or ErrCacheRead.
func Enrich(ctx context.Context, in EnrichInput) (*EnrichResult, error) {
	o := in.Opts.withDefaults()
	var u usage
	res := &EnrichResult{}
	defer func() { res.TokensIn, res.TokensOut = u.in.Load(), u.out.Load() }()
	if in.Model == nil {
		res.State = LLMStateFailed
		return res, errors.New("handbook: no model")
	}
	fail := func(err error) (*EnrichResult, error) {
		res.State = LLMStateFailed
		return res, err
	}

	stored, err := in.Cache.CardFailures()
	if err != nil {
		return fail(err)
	}
	prior := stored
	if o.Full {
		prior = &CardFailures{}
	}
	failures := &CardFailures{Failed: map[string]bool{}, Transport: map[string]int{}}
	files := eligibleFiles(in.Facts)
	res.CardsTotal = len(files)
	cards := map[string]*Card{}
	byHash := map[string]*cardJob{}
	var todo []*cardJob
	for _, f := range files {
		if !validHash(f.Hash) {
			continue
		}
		if j := byHash[f.Hash]; j != nil {
			j.files = append(j.files, f)
			continue
		}
		c, err := in.Cache.Card(f.Hash)
		if err != nil {
			return fail(err)
		}
		j := &cardJob{hash: f.Hash, files: []File{f}}
		byHash[f.Hash] = j
		switch {
		case c != nil:
			cards[f.Path] = c
		case prior.Failed[f.Hash]:
			failures.Failed[f.Hash] = true
		default:
			if n := prior.Transport[f.Hash]; n > 0 {
				failures.Transport[f.Hash] = n
			}
			todo = append(todo, j)
		}
	}
	for _, j := range byHash {
		if c := cards[j.files[0].Path]; c != nil {
			for _, f := range j.files[1:] {
				cards[f.Path] = c
			}
		}
	}
	for _, j := range todo {
		sort.Slice(j.files, func(a, b int) bool { return j.files[a].Path < j.files[b].Path })
	}
	sort.SliceStable(todo, func(i, j int) bool {
		a, b := todo[i].files[0], todo[j].files[0]
		if (a.Lang == "go") != (b.Lang == "go") {
			return a.Lang == "go"
		}
		return a.Path < b.Path
	})
	cut, err := generateCards(ctx, in, o, todo, cards, failures, res, &u)
	res.CardsDone = len(cards)
	for _, f := range files {
		if failures.Failed[f.Hash] {
			res.CardErrors++
		}
	}
	res.Changed = res.CardsNew > 0
	if !maps.Equal(failures.Failed, stored.Failed) || !maps.Equal(failures.Transport, stored.Transport) {
		if perr := in.Cache.PutCardFailures(failures); perr != nil && err == nil {
			err = perr
		}
	}
	if err != nil {
		return fail(err)
	}
	// Cards that failed in transit are retried next run, like cards cut by the budget, until
	// they move to the failed set; only failed-set hashes may stay missing when synthesizing.
	if cut || ctx.Err() != nil || res.CardTransportErrors > 0 {
		res.State = LLMStatePartial
		return res, nil
	}
	if err := synthesizeAll(ctx, in, o, cards, res, &u); err != nil {
		if ctx.Err() != nil {
			res.State = LLMStatePartial
			return res, nil
		}
		if errors.Is(err, ErrModelUnavailable) {
			res.LastTransportError = transportMessage(err)
		}
		return fail(err)
	}
	res.State = LLMStateComplete
	return res, nil
}

// generateCards runs the card jobs in order with at most o.MaxCardsPerRun model calls and
// reports whether jobs were left untried for lack of budget. Unusable replies, and transport
// errors in cardTransportRounds runs, add the hash to the failed set; maxConsecutiveCallErrors
// failed calls in a row abort with ErrModelUnavailable and a failed cache write aborts with
// that error.
func generateCards(ctx context.Context, in EnrichInput, o EnrichOptions, todo []*cardJob, cards map[string]*Card, failures *CardFailures, res *EnrichResult, u *usage) (bool, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	var (
		mu          sync.Mutex
		consecutive int
		calls       int
		exhausted   bool
		fatal       error
		wg          sync.WaitGroup
		stopOnce    sync.Once
	)
	stop := make(chan struct{})
	abort := func(err error) {
		if fatal == nil {
			fatal = err
			cancel()
		}
	}
	jobs := make(chan *cardJob)
	for i := 0; i < o.Concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range jobs {
				if ctx.Err() != nil {
					continue
				}
				f, content, ok := readJob(in.Root, j)
				if !ok {
					continue
				}
				mu.Lock()
				if calls >= o.MaxCardsPerRun {
					exhausted = true
					mu.Unlock()
					stopOnce.Do(func() { close(stop) })
					continue
				}
				calls++
				mu.Unlock()
				card, err := generateCard(ctx, in.Model, in.ModelName, in.RelPath, f, in.Facts.Symbols[f.Path], content, o.MaxFileBytes, u)
				var putErr error
				if err == nil {
					putErr = in.Cache.PutCard(j.hash, card)
				}
				mu.Lock()
				switch {
				case putErr != nil:
					abort(fmt.Errorf("handbook: cache card: %w", putErr))
				case err == nil:
					for _, f := range j.files {
						cards[f.Path] = card
					}
					res.CardsNew += len(j.files)
					delete(failures.Transport, j.hash)
					consecutive = 0
				case ctx.Err() != nil:
				case errors.Is(err, errBadReply):
					failures.Failed[j.hash] = true
					delete(failures.Transport, j.hash)
					consecutive = 0
				default:
					res.LastTransportError = transportMessage(err)
					if failures.Transport[j.hash]++; failures.Transport[j.hash] >= cardTransportRounds {
						failures.Failed[j.hash] = true
						delete(failures.Transport, j.hash)
					} else {
						res.CardTransportErrors += len(j.files)
					}
					consecutive++
					if consecutive >= maxConsecutiveCallErrors {
						abort(fmt.Errorf("%w: %w", ErrModelUnavailable, err))
					}
				}
				mu.Unlock()
			}
		}()
	}
feed:
	for _, j := range todo {
		select {
		case jobs <- j:
		case <-stop:
			break feed
		case <-ctx.Done():
			break feed
		}
	}
	close(jobs)
	wg.Wait()
	return exhausted, fatal
}

// readJob returns the first file of a job whose checkout content still matches its hash.
func readJob(root string, j *cardJob) (File, []byte, bool) {
	for _, f := range j.files {
		if b, ok := readMatching(root, f); ok {
			return f, b, true
		}
	}
	return File{}, nil, false
}

// readMatching reads a regular file of the checkout and reports whether it still matches the
// facts.
func readMatching(root string, f File) ([]byte, bool) {
	fh, err := os.OpenInRoot(root, filepath.FromSlash(f.Path))
	if err != nil {
		return nil, false
	}
	defer fh.Close()
	if st, err := fh.Stat(); err != nil || !st.Mode().IsRegular() {
		return nil, false
	}
	b, err := io.ReadAll(io.LimitReader(fh, MaxFileBytes+1))
	if err != nil || len(b) > MaxFileBytes {
		return nil, false
	}
	sum := sha256.Sum256(b)
	return b, hex.EncodeToString(sum[:]) == f.Hash
}

// retryModel retries failed calls (transport errors, not unusable replies) with a linear
// backoff, stopping early when ctx is done.
type retryModel struct {
	model.Model
	retries int
	backoff time.Duration
}

func (m retryModel) Chat(ctx context.Context, msgs []model.Message, opts ...model.Option) (*model.Generation, error) {
	for attempt := 0; ; attempt++ {
		g, err := m.Model.Chat(ctx, msgs, opts...)
		if err == nil || attempt == m.retries || ctx.Err() != nil {
			return g, err
		}
		t := time.NewTimer(m.backoff * time.Duration(attempt+1))
		select {
		case <-ctx.Done():
			t.Stop()
			return nil, err
		case <-t.C:
		}
	}
}

func modelErr(err error) error { return fmt.Errorf("%w: %w", ErrModelUnavailable, err) }

// transportMessage is the clipped message of a failed model call.
func transportMessage(err error) string {
	return clipRunes(strings.TrimPrefix(err.Error(), ErrModelUnavailable.Error()+": "), maxErrorRunes)
}

func stageSignature(sk *Skeleton) string {
	if sk == nil {
		return ""
	}
	var b strings.Builder
	for _, s := range sk.Stages {
		b.WriteString(s.ID + "\x00" + s.Title + "\x00")
	}
	return b.String()
}

// pointCards makes CardHash name the card shown for each file: the card of its current
// content when one exists, otherwise its previous card (rendered as stale), otherwise none.
// It returns the stages that gained a current card and whether any CardHash changed.
func pointCards(sk, prev *Skeleton, files []File, cards map[string]*Card) (map[string]bool, bool) {
	gained, moved := map[string]bool{}, false
	for _, f := range files {
		a, ok := sk.Files[f.Path]
		if !ok {
			continue
		}
		want := a.CardHash
		switch {
		case cards[f.Path] != nil:
			want = f.Hash
		case want == f.Hash || want == "":
			want = ""
			if prev != nil {
				if p, ok := prev.Files[f.Path]; ok && p.CardHash != f.Hash {
					want = p.CardHash
				}
			}
		}
		if want == a.CardHash {
			continue
		}
		if want == f.Hash && a.Stage != "" {
			gained[a.Stage] = true
		}
		a.CardHash, moved = want, true
		sk.Files[f.Path] = a
	}
	return gained, moved
}

// pendingWork is the synthesis a skeleton records as still owed.
type pendingWork struct {
	stages          []string
	overview, notes bool
}

func pendingOf(sk *Skeleton) pendingWork {
	return pendingWork{stages: slices.Clone(sk.PendingStages), overview: sk.PendingOverview, notes: sk.PendingNotes}
}

func (p pendingWork) equal(q pendingWork) bool {
	return slices.Equal(p.stages, q.stages) && p.overview == q.overview && p.notes == q.notes
}

// synthesizeAll rebuilds or incrementally updates the skeleton, then rewrites affected stage
// summaries, the overview and register notes. The updated skeleton is stored with the work
// still owed (Pending*) before any summary is written and again after each summary and the
// overview, so a run cut short by its deadline or a model error leaves a usable skeleton and
// the next run resumes only what is pending. Pruning unused cards afterwards is best effort.
func synthesizeAll(ctx context.Context, in EnrichInput, o EnrichOptions, cards map[string]*Card, res *EnrichResult, u *usage) error {
	m := retryModel{Model: in.Model, retries: synthesisRetries, backoff: o.RetryBackoff}
	sk, err := in.Cache.Skeleton()
	if err != nil {
		return err
	}
	prev, prevStages, storedCommit := sk, stageSignature(sk), ""
	var resume pendingWork
	if sk != nil {
		storedCommit = sk.Commit
		if sk.PromptVersion == LLMPromptVersion {
			resume = pendingOf(sk)
		}
	}
	onDisk := resume
	reason := "manual"
	if !o.Full {
		// Decide what does not depend on the incremental update first, so a skeleton that is
		// rebuilt anyway is not updated in vain; "unassigned" needs new files organized.
		if reason = rebuildReason(sk, in.Facts, in.Now, o.SkeletonRebuildDays); reason == "unassigned" {
			reason = ""
		}
	}
	var affected, changed map[string]bool
	if reason == "" {
		affected, changed = updateSkeleton(sk, in.Facts, in.Commit, in.Now)
		reason = rebuildReason(sk, in.Facts, in.Now, o.SkeletonRebuildDays)
	}
	rebuilt := reason != ""
	if rebuilt {
		sk, err = inferSkeleton(ctx, m, in.RelPath, in.Facts, cards, in.Commit, in.Now, u)
		if err != nil {
			return modelErr(err)
		}
		if prev != nil && prev.PromptVersion == LLMPromptVersion {
			sk.RegisterNotes = prev.RegisterNotes
		}
		affected, changed = map[string]bool{}, nil
		for _, s := range sk.Stages {
			affected[s.ID] = true
		}
	}
	gained, moved := pointCards(sk, prev, eligibleFiles(in.Facts), cards)
	for s := range gained {
		affected[s] = true
	}
	for _, s := range resume.stages {
		affected[s] = true
	}
	notesChanged := changed
	if rebuilt || resume.notes {
		notesChanged = map[string]bool{}
		for p := range sk.Files {
			notesChanged[p] = true
		}
	}
	sk.PendingStages = nil
	for _, st := range sk.Stages {
		if affected[st.ID] || st.Summary == "" {
			sk.PendingStages = append(sk.PendingStages, st.ID)
		}
	}
	sk.PendingOverview = len(sk.Stages) > 0 && (resume.overview || sk.Overview == "" || stageSignature(sk) != prevStages)
	sk.PendingNotes = len(notesChanged) > 0

	report := func() {
		res.Stages, res.Fallback, res.FallbackReason, res.SkeletonBuiltAt = len(sk.Stages), sk.FallbackAreas, sk.FallbackReason, sk.BuiltAt
		res.SkeletonRebuilt = rebuilt
		if rebuilt {
			res.RebuildReason = reason
		}
	}
	// unsaved marks rendered content not yet stored; storing it changes the handbook.
	unsaved := rebuilt || moved || len(changed) > 0
	save := func() error {
		sk.Commit, sk.UpdatedAt = in.Commit, in.Now
		if err := in.Cache.PutSkeleton(sk); err != nil {
			return err
		}
		report()
		res.Changed = res.Changed || unsaved
		unsaved, storedCommit, onDisk = false, in.Commit, pendingOf(sk)
		return nil
	}
	if unsaved {
		if err := save(); err != nil {
			return err
		}
	}

	filesOf := map[string][]string{}
	for p, a := range sk.Files {
		filesOf[a.Stage] = append(filesOf[a.Stage], p)
	}
	for i := range sk.Stages {
		st := sk.Stages[i]
		if !slices.Contains(sk.PendingStages, st.ID) {
			continue
		}
		files := filesOf[st.ID]
		sort.Strings(files)
		s, err := summarizeStage(ctx, m, in.RelPath, st, files, cards, u)
		if err != nil && !errors.Is(err, errBadReply) {
			return modelErr(err)
		}
		sk.PendingStages = slices.DeleteFunc(sk.PendingStages, func(id string) bool { return id == st.ID })
		if err != nil {
			continue
		}
		sk.Stages[i].Summary = s
		sk.PendingOverview = true
		unsaved = true
		if err := save(); err != nil {
			return err
		}
	}
	if sk.PendingOverview {
		ov, err := writeOverview(ctx, m, in.RelPath, in.Facts, sk, u)
		switch {
		case err == nil:
			unsaved = unsaved || ov != sk.Overview
			sk.Overview = ov
		case !errors.Is(err, errBadReply):
			return modelErr(err)
		}
		sk.PendingOverview = false
		if unsaved {
			if err := save(); err != nil {
				return err
			}
		}
	}
	notes, err := registerNotes(ctx, m, in.RelPath, in.Facts.Registers, cards, notesChanged, sk.RegisterNotes, u)
	if err != nil {
		return modelErr(err)
	}
	unsaved = unsaved || !maps.Equal(notes, sk.RegisterNotes)
	sk.RegisterNotes = notes
	sk.PendingNotes = false
	if unsaved || storedCommit != in.Commit || !pendingOf(sk).equal(onDisk) {
		if err := save(); err != nil {
			return err
		}
	}
	report()

	keep := map[string]bool{}
	for _, f := range in.Facts.Files {
		keep[f.Hash] = true
	}
	for _, a := range sk.Files {
		keep[a.CardHash] = true
	}
	if _, err := in.Cache.PruneCards(keep); err != nil {
		res.PruneError = err
	}
	return nil
}
