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
	"sort"
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
)

// ErrModelUnavailable aborts a run whose model calls keep failing.
var ErrModelUnavailable = errors.New("handbook: model calls keep failing")

// EnrichOptions bounds one LLM run; zero values take defaults.
type EnrichOptions struct {
	Concurrency         int
	MaxCardsPerRun      int // model calls for cards; files with identical content share one
	MaxFileBytes        int
	SkeletonRebuildDays int
	Full                bool          // rebuild the skeleton regardless of thresholds
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
	State           string
	CardsTotal      int
	CardsDone       int
	CardsNew        int
	CardErrors      int
	Stages          int
	Fallback        bool
	SkeletonRebuilt bool
	RebuildReason   string
	SkeletonBuiltAt time.Time
	TokensIn        int64
	TokensOut       int64
	Changed         bool // cache content changed; the handbook needs a re-render
}

// cardJob is one model call: a content hash and the files that have it.
type cardJob struct {
	hash  string
	files []File
}

// Enrich generates missing file cards within budget and, once every card-eligible file has
// been attempted, synthesizes the skeleton. Cancelling ctx stops the run and reports partial
// progress without error; everything already generated stays cached. Any other error leaves
// the cached skeleton untouched and reports state failed.
func Enrich(ctx context.Context, in EnrichInput) (*EnrichResult, error) {
	o := in.Opts.withDefaults()
	var u usage
	res := &EnrichResult{}
	defer func() { res.TokensIn, res.TokensOut = u.in.Load(), u.out.Load() }()
	if in.Model == nil {
		res.State = LLMStateFailed
		return res, errors.New("handbook: no model")
	}

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
			res.State = LLMStateFailed
			return res, err
		}
		j := &cardJob{hash: f.Hash, files: []File{f}}
		byHash[f.Hash] = j
		if c != nil {
			cards[f.Path] = c
		} else {
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
	cut := len(todo) > o.MaxCardsPerRun
	if cut {
		todo = todo[:o.MaxCardsPerRun]
	}
	err := generateCards(ctx, in, o, todo, cards, res, &u)
	res.CardsDone = len(cards)
	res.Changed = res.CardsNew > 0
	if err != nil {
		res.State = LLMStateFailed
		return res, err
	}
	if cut || ctx.Err() != nil {
		res.State = LLMStatePartial
		return res, nil
	}
	if err := synthesizeAll(ctx, in, o, cards, res, &u); err != nil {
		if ctx.Err() != nil {
			res.State = LLMStatePartial
			return res, nil
		}
		res.State = LLMStateFailed
		return res, err
	}
	res.State = LLMStateComplete
	return res, nil
}

// generateCards runs the card jobs concurrently. Unusable replies count as card errors;
// maxConsecutiveCallErrors failed calls in a row abort with ErrModelUnavailable and a failed
// cache write aborts with that error.
func generateCards(ctx context.Context, in EnrichInput, o EnrichOptions, todo []*cardJob, cards map[string]*Card, res *EnrichResult, u *usage) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	var (
		mu          sync.Mutex
		consecutive int
		fatal       error
		wg          sync.WaitGroup
	)
	abort := func(err error) {
		if fatal == nil {
			fatal = err
			cancel()
		}
	}
	limit := min(maxConsecutiveCallErrors, len(todo))
	jobs := make(chan *cardJob)
	for i := 0; i < o.Concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range jobs {
				f, content, ok := readJob(in.Root, j)
				if !ok {
					continue
				}
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
					consecutive = 0
				case ctx.Err() != nil:
				case errors.Is(err, errBadReply):
					res.CardErrors += len(j.files)
					consecutive = 0
				default:
					consecutive++
					if consecutive >= limit {
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
		case <-ctx.Done():
			break feed
		}
	}
	close(jobs)
	wg.Wait()
	return fatal
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

// readMatching reads a file of the checkout and reports whether it still matches the facts.
func readMatching(root string, f File) ([]byte, bool) {
	fh, err := os.OpenInRoot(root, filepath.FromSlash(f.Path))
	if err != nil {
		return nil, false
	}
	defer fh.Close()
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

// synthesizeAll rebuilds or incrementally updates the skeleton, rewrites affected stage
// summaries, the overview and register notes, and stores the skeleton only when every step
// succeeded.
func synthesizeAll(ctx context.Context, in EnrichInput, o EnrichOptions, cards map[string]*Card, res *EnrichResult, u *usage) error {
	m := retryModel{Model: in.Model, retries: synthesisRetries, backoff: o.RetryBackoff}
	sk, err := in.Cache.Skeleton()
	if err != nil {
		return err
	}
	storedCommit := ""
	if sk != nil {
		storedCommit = sk.Commit
	}
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
		old := sk
		sk, err = inferSkeleton(ctx, m, in.RelPath, in.Facts, cards, in.Commit, in.Now, u)
		if err != nil {
			return modelErr(err)
		}
		if old != nil && old.PromptVersion == LLMPromptVersion {
			sk.RegisterNotes = old.RegisterNotes
		}
		affected, changed = map[string]bool{}, nil
		for _, s := range sk.Stages {
			affected[s.ID] = true
		}
	}
	dirty := rebuilt || len(changed) > 0

	filesOf := map[string][]string{}
	for p, a := range sk.Files {
		filesOf[a.Stage] = append(filesOf[a.Stage], p)
	}
	rewritten := 0
	for i, st := range sk.Stages {
		if !affected[st.ID] && st.Summary != "" {
			continue
		}
		files := filesOf[st.ID]
		sort.Strings(files)
		s, err := summarizeStage(ctx, m, in.RelPath, st, files, cards, u)
		if err != nil {
			if errors.Is(err, errBadReply) {
				continue
			}
			return modelErr(err)
		}
		sk.Stages[i].Summary = s
		rewritten++
	}
	if len(sk.Stages) > 0 && (rewritten > 0 || sk.Overview == "") {
		ov, err := writeOverview(ctx, m, in.RelPath, in.Facts, sk, u)
		switch {
		case err == nil:
			dirty = dirty || ov != sk.Overview
			sk.Overview = ov
		case !errors.Is(err, errBadReply):
			return modelErr(err)
		}
	}
	notesChanged := changed
	if rebuilt {
		notesChanged = map[string]bool{}
		for p := range sk.Files {
			notesChanged[p] = true
		}
	}
	notes, err := registerNotes(ctx, m, in.RelPath, in.Facts.Registers, cards, notesChanged, sk.RegisterNotes, u)
	if err != nil {
		return modelErr(err)
	}
	dirty = dirty || rewritten > 0 || !maps.Equal(notes, sk.RegisterNotes)
	sk.RegisterNotes = notes
	if dirty || storedCommit != in.Commit {
		sk.Commit, sk.UpdatedAt = in.Commit, in.Now
		if err := in.Cache.PutSkeleton(sk); err != nil {
			return err
		}
	}
	keep := map[string]bool{}
	for _, f := range in.Facts.Files {
		keep[f.Hash] = true
	}
	for _, a := range sk.Files {
		keep[a.CardHash] = true
	}
	if _, err := in.Cache.PruneCards(keep); err != nil {
		return err
	}
	res.Stages, res.Fallback, res.SkeletonBuiltAt = len(sk.Stages), sk.FallbackAreas, sk.BuiltAt
	res.SkeletonRebuilt = rebuilt
	if rebuilt {
		res.RebuildReason = reason
	}
	res.Changed = res.Changed || dirty
	return nil
}
