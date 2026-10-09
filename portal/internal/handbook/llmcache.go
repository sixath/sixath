package handbook

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// LLMPromptVersion changes whenever card or skeleton prompts change; cached output of an older
// version is ignored and regenerated.
const LLMPromptVersion = "p2b-1"

var cardLangs = map[string]bool{
	"go": true, "proto": true, "sql": true, "python": true, "java": true, "javascript": true,
	"typescript": true, "shell": true, "lua": true, "c": true, "cpp": true, "rust": true,
	"php": true, "ruby": true, "kotlin": true, "csharp": true, "scala": true, "vue": true,
}

// CardEligible reports whether a file gets an LLM card: non-empty, non-test source code.
func CardEligible(f File) bool { return !f.Test && f.Size > 0 && cardLangs[f.Lang] }

// CardFunc is one key function of a card; Name is always one of the file's symbols.
type CardFunc struct {
	Name    string `json:"name"`
	Summary string `json:"summary"`
}

// Card is the LLM description of one file content (keyed by its sha256).
type Card struct {
	Purpose       string     `json:"purpose"`
	Description   string     `json:"description,omitempty"`
	Role          string     `json:"role,omitempty"`
	Lifecycle     string     `json:"lifecycle,omitempty"`
	Functions     []CardFunc `json:"functions,omitempty"`
	Hash          string     `json:"hash"`
	PromptVersion string     `json:"prompt_version"`
	Model         string     `json:"model,omitempty"`
	CreatedAt     time.Time  `json:"created_at"`
}

// Stage is one behavioral stage of a repository.
type Stage struct {
	ID      string `json:"id"`
	Title   string `json:"title"`
	Summary string `json:"summary,omitempty"`
}

// FileAssign records a card-eligible file's stage ("" = unassigned) and the content hash of
// the card it was organized with.
type FileAssign struct {
	Stage    string `json:"stage,omitempty"`
	CardHash string `json:"card_hash"`
}

// Skeleton is the LLM organization of a repository: stages, file assignment, overview and
// register notes. It lives outside versioned dirs and is updated incrementally.
type Skeleton struct {
	PromptVersion       string                `json:"prompt_version"`
	Commit              string                `json:"commit"`
	BuiltAt             time.Time             `json:"built_at"`
	UpdatedAt           time.Time             `json:"updated_at"`
	BaseFiles           int                   `json:"base_files"`
	ChangedSinceRebuild int                   `json:"changed_since_rebuild"`
	TopDirs             []string              `json:"top_dirs"`
	FallbackAreas       bool                  `json:"fallback_areas,omitempty"`
	Stages              []Stage               `json:"stages"`
	Files               map[string]FileAssign `json:"files"`
	Overview            string                `json:"overview,omitempty"`
	RegisterNotes       map[string]string     `json:"register_notes,omitempty"`
}

// LLMCache stores LLM output of one repository outside its versioned dirs.
type LLMCache struct{ Dir string }

var (
	errBadHash     = errors.New("handbook: invalid content hash")
	errCorruptJSON = errors.New("handbook: corrupt JSON file")
)

// validHash accepts a lowercase hex sha256, the form CollectFacts produces.
func validHash(h string) bool {
	if len(h) != 64 {
		return false
	}
	for i := 0; i < len(h); i++ {
		if c := h[i]; (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

func (c LLMCache) cardPath(hash string) string {
	return filepath.Join(c.Dir, "cards", hash[:2], hash+"-"+LLMPromptVersion+".json")
}

func (c LLMCache) skeletonPath() string { return filepath.Join(c.Dir, "skeleton.json") }

// readCacheJSON is readJSON for regenerable cache files: a file that does not decode is
// removed (best effort) and reported as missing.
func readCacheJSON(p string, v any) (bool, error) {
	ok, err := readJSON(p, v)
	if errors.Is(err, errCorruptJSON) {
		_ = os.Remove(p)
		return false, nil
	}
	return ok, err
}

// Card returns the cached card of a content hash, or nil when there is none.
func (c LLMCache) Card(hash string) (*Card, error) {
	if !validHash(hash) {
		return nil, errBadHash
	}
	var card Card
	if ok, err := readCacheJSON(c.cardPath(hash), &card); !ok || err != nil {
		return nil, err
	}
	return &card, nil
}

// PutCard stores the card of a content hash. Concurrent writers of the same hash may collide
// on Windows, where renaming over a file being replaced fails; cards of one hash are
// interchangeable, so a readable card left by another writer counts as success.
func (c LLMCache) PutCard(hash string, card *Card) error {
	if !validHash(hash) {
		return errBadHash
	}
	var err error
	for attempt := range 3 {
		if err = writeJSON(c.cardPath(hash), card); err == nil {
			return nil
		}
		if got, rerr := c.Card(hash); rerr == nil && got != nil {
			return nil
		}
		time.Sleep(time.Duration(attempt+1) * 10 * time.Millisecond)
	}
	return err
}

// Skeleton returns the cached skeleton, or nil when there is none.
func (c LLMCache) Skeleton() (*Skeleton, error) {
	var sk Skeleton
	if ok, err := readCacheJSON(c.skeletonPath(), &sk); !ok || err != nil {
		return nil, err
	}
	if sk.Files == nil {
		sk.Files = map[string]FileAssign{}
	}
	return &sk, nil
}

// PutSkeleton stores the skeleton.
func (c LLMCache) PutSkeleton(sk *Skeleton) error { return writeJSON(c.skeletonPath(), sk) }

// PruneCards removes cached cards whose content hash is not in keep and returns how many
// were removed.
func (c LLMCache) PruneCards(keep map[string]bool) (int, error) {
	n := 0
	err := filepath.WalkDir(filepath.Join(c.Dir, "cards"), func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return nil
			}
			return err
		}
		if d.IsDir() {
			return nil
		}
		if strings.HasPrefix(d.Name(), ".") {
			if info, err := d.Info(); err == nil && time.Since(info.ModTime()) > staleTmpAge {
				_ = os.Remove(p)
			}
			return nil
		}
		hash, _, _ := strings.Cut(d.Name(), "-")
		if keep[hash] && strings.HasSuffix(d.Name(), "-"+LLMPromptVersion+".json") {
			return nil
		}
		if err := os.Remove(p); err != nil {
			return err
		}
		n++
		return nil
	})
	return n, err
}

func readJSON(p string, v any) (bool, error) {
	b, err := os.ReadFile(p)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if err := json.Unmarshal(b, v); err != nil {
		return false, fmt.Errorf("%w: decode %s: %v", errCorruptJSON, filepath.Base(p), err)
	}
	return true, nil
}

func writeJSON(p string, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return WriteFileAtomic(p, b)
}

// LLMLayer is the LLM content available to one render.
type LLMLayer struct {
	Cards    map[string]*Card // path -> card of the file's current content
	Stale    map[string]*Card // path -> last card of a file whose content changed since
	Skeleton *Skeleton        // nil until the first synthesis
}

// Empty reports whether the layer adds nothing to a render.
func (l *LLMLayer) Empty() bool {
	return l == nil || (len(l.Cards) == 0 && len(l.Stale) == 0 && l.Skeleton == nil)
}

// LoadLLMLayer reads the cached cards of f's card-eligible files and the skeleton.
func LoadLLMLayer(c LLMCache, f *Facts) (*LLMLayer, error) {
	l := &LLMLayer{Cards: map[string]*Card{}, Stale: map[string]*Card{}}
	sk, err := c.Skeleton()
	if err != nil {
		return nil, err
	}
	if sk != nil && sk.PromptVersion == LLMPromptVersion {
		l.Skeleton = sk
	}
	for _, file := range f.Files {
		if !CardEligible(file) || !validHash(file.Hash) {
			continue
		}
		card, err := c.Card(file.Hash)
		if err != nil {
			return nil, err
		}
		if card != nil {
			l.Cards[file.Path] = card
			continue
		}
		if l.Skeleton == nil {
			continue
		}
		if a, ok := l.Skeleton.Files[file.Path]; ok && a.CardHash != "" && a.CardHash != file.Hash && validHash(a.CardHash) {
			if old, err := c.Card(a.CardHash); err == nil && old != nil {
				l.Stale[file.Path] = old
			}
		}
	}
	return l, nil
}
