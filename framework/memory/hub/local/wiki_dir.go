package local

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode"
)

const (
	wikiMaxFileBytes = 512 << 10 // 512 KiB per file
	wikiSnippetRunes = 240
)

// DirWiki indexes markdown/text files under a local directory (P3b).
// Search is case-insensitive substring match over relative path + content.
// Hit ID is the slash-normalized path relative to Root.
type DirWiki struct {
	Root string
}

// NewDirWiki validates root exists and is a directory.
func NewDirWiki(root string) (*DirWiki, error) {
	root = strings.TrimSpace(root)
	if root == "" {
		return nil, fmt.Errorf("hub/local: wiki root is empty")
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("hub/local: wiki root: %w", err)
	}
	st, err := os.Stat(abs)
	if err != nil {
		return nil, fmt.Errorf("hub/local: wiki root: %w", err)
	}
	if !st.IsDir() {
		return nil, fmt.Errorf("hub/local: wiki root is not a directory: %s", abs)
	}
	return &DirWiki{Root: abs}, nil
}

func (w *DirWiki) Search(ctx context.Context, query string, limit int) ([]KnowledgeHit, error) {
	if w == nil || w.Root == "" {
		return nil, nil
	}
	q := strings.TrimSpace(query)
	if q == "" {
		return nil, nil
	}
	if limit <= 0 {
		limit = 5
	}
	ql := strings.ToLower(q)
	var hits []KnowledgeHit
	err := filepath.WalkDir(w.Root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil // skip unreadable entries
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if d.IsDir() {
			name := d.Name()
			if name == ".git" || name == "node_modules" || name == ".svn" {
				return filepath.SkipDir
			}
			return nil
		}
		if !isWikiFile(d.Name()) {
			return nil
		}
		if IsWikiDraftFile(d.Name()) {
			return nil
		}
		rel, err := filepath.Rel(w.Root, path)
		if err != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)
		body, err := readCapped(path, wikiMaxFileBytes)
		if err != nil {
			return nil
		}
		hay := strings.ToLower(rel + "\n" + body)
		if !strings.Contains(hay, ql) {
			return nil
		}
		hits = append(hits, KnowledgeHit{
			ID:      rel,
			Source:  "wiki",
			Content: wikiSnippet(body, q),
			Score:   wikiScore(rel, body, ql),
		})
		if len(hits) >= limit {
			return errStopWalk
		}
		return nil
	})
	if err != nil && err != errStopWalk {
		return hits, err
	}
	return hits, nil
}

// WikiDraftMeta describes a pending wiki draft (formal id + preview).
type WikiDraftMeta struct {
	ID        string // canonical formal id (*.md)
	Preview   string
	UpdatedAt string // RFC3339 or empty
}

// Read returns full (capped) content for a relative wiki path id.
func (w *DirWiki) Read(_ context.Context, id string) (*KnowledgeHit, error) {
	if w == nil || w.Root == "" {
		return nil, fmt.Errorf("hub/local: wiki not configured")
	}
	rel := strings.TrimSpace(strings.ReplaceAll(id, "\\", "/"))
	if rel == "" || strings.Contains(rel, "..") {
		return nil, fmt.Errorf("hub/local: invalid wiki id")
	}
	absFull, err := w.absUnderRoot(rel)
	if err != nil {
		return nil, err
	}
	body, err := readCapped(absFull, wikiMaxFileBytes)
	if err != nil {
		return nil, err
	}
	return &KnowledgeHit{ID: rel, Source: "wiki", Content: body, Score: 1}, nil
}

// WriteDraft writes content to the draft path for id and returns the canonical formal id.
func (w *DirWiki) WriteDraft(_ context.Context, id, content string) (string, error) {
	if w == nil || w.Root == "" {
		return "", fmt.Errorf("hub/local: wiki not configured")
	}
	canonical, err := CanonicalWikiID(id)
	if err != nil {
		return "", err
	}
	if len(content) > wikiMaxFileBytes {
		return "", fmt.Errorf("hub/local: wiki content exceeds %d bytes", wikiMaxFileBytes)
	}
	draftRel := DraftPathForWikiID(canonical)
	draftAbs, err := w.absUnderRoot(draftRel)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(draftAbs), 0o755); err != nil {
		return "", fmt.Errorf("hub/local: write wiki draft: %w", err)
	}
	if err := os.WriteFile(draftAbs, []byte(content), 0o644); err != nil {
		return "", fmt.Errorf("hub/local: write wiki draft: %w", err)
	}
	return canonical, nil
}

// ApproveDraft promotes a draft to the formal page. If the formal page exists,
// overwrite must be true. Draft removal after a successful formal write is best-effort.
func (w *DirWiki) ApproveDraft(_ context.Context, id string, overwrite bool) error {
	if w == nil || w.Root == "" {
		return fmt.Errorf("hub/local: wiki not configured")
	}
	canonical, err := CanonicalWikiID(id)
	if err != nil {
		return err
	}
	draftRel := DraftPathForWikiID(canonical)
	draftAbs, err := w.absUnderRoot(draftRel)
	if err != nil {
		return err
	}
	body, err := os.ReadFile(draftAbs)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("hub/local: wiki draft not found: %s", canonical)
		}
		return fmt.Errorf("hub/local: read wiki draft: %w", err)
	}
	if len(body) > wikiMaxFileBytes {
		return fmt.Errorf("hub/local: wiki content exceeds %d bytes", wikiMaxFileBytes)
	}
	formalAbs, err := w.absUnderRoot(canonical)
	if err != nil {
		return err
	}
	if _, err := os.Stat(formalAbs); err == nil {
		if !overwrite {
			return fmt.Errorf("hub/local: wiki formal page exists; set overwrite=true")
		}
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("hub/local: stat wiki formal: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(formalAbs), 0o755); err != nil {
		return fmt.Errorf("hub/local: write wiki formal: %w", err)
	}
	if err := os.WriteFile(formalAbs, body, 0o644); err != nil {
		return fmt.Errorf("hub/local: write wiki formal: %w", err)
	}
	if err := os.Remove(draftAbs); err != nil && !os.IsNotExist(err) {
		log.Printf("hub/local: remove wiki draft %s: %v", draftRel, err)
	}
	return nil
}

// ListDrafts walks *.draft.md under Root and returns metadata keyed by formal id.
func (w *DirWiki) ListDrafts(ctx context.Context, limit int) ([]WikiDraftMeta, error) {
	if w == nil || w.Root == "" {
		return nil, fmt.Errorf("hub/local: wiki not configured")
	}
	if limit <= 0 {
		limit = 50
	}
	var out []WikiDraftMeta
	err := filepath.WalkDir(w.Root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if d.IsDir() {
			name := d.Name()
			if name == ".git" || name == "node_modules" || name == ".svn" {
				return filepath.SkipDir
			}
			return nil
		}
		if !IsWikiDraftFile(d.Name()) {
			return nil
		}
		rel, err := filepath.Rel(w.Root, path)
		if err != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)
		canonical, err := CanonicalWikiID(rel)
		if err != nil {
			return nil
		}
		body, err := readCapped(path, wikiMaxFileBytes)
		if err != nil {
			return nil
		}
		meta := WikiDraftMeta{
			ID:      canonical,
			Preview: wikiDraftPreview(body),
		}
		if info, err := d.Info(); err == nil {
			meta.UpdatedAt = info.ModTime().UTC().Format(time.RFC3339)
		}
		out = append(out, meta)
		if len(out) >= limit {
			return errStopWalk
		}
		return nil
	})
	if err != nil && err != errStopWalk {
		return out, err
	}
	return out, nil
}

// ReadPreferDraft returns draft content if present, otherwise the formal page.
// Hit.ID is always the canonical formal id.
func (w *DirWiki) ReadPreferDraft(_ context.Context, id string) (*KnowledgeHit, error) {
	if w == nil || w.Root == "" {
		return nil, fmt.Errorf("hub/local: wiki not configured")
	}
	canonical, err := CanonicalWikiID(id)
	if err != nil {
		return nil, err
	}
	draftAbs, err := w.absUnderRoot(DraftPathForWikiID(canonical))
	if err != nil {
		return nil, err
	}
	if body, err := readCapped(draftAbs, wikiMaxFileBytes); err == nil {
		return &KnowledgeHit{ID: canonical, Source: "wiki", Content: body, Score: 1}, nil
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	formalAbs, err := w.absUnderRoot(canonical)
	if err != nil {
		return nil, err
	}
	body, err := readCapped(formalAbs, wikiMaxFileBytes)
	if err != nil {
		return nil, err
	}
	return &KnowledgeHit{ID: canonical, Source: "wiki", Content: body, Score: 1}, nil
}

func (w *DirWiki) absUnderRoot(rel string) (string, error) {
	full := filepath.Join(w.Root, filepath.FromSlash(rel))
	absFull, err := filepath.Abs(full)
	if err != nil {
		return "", err
	}
	if !pathUnderRoot(w.Root, absFull) {
		return "", fmt.Errorf("hub/local: wiki path escapes root")
	}
	return absFull, nil
}

func wikiDraftPreview(body string) string {
	body = strings.TrimSpace(body)
	if body == "" {
		return ""
	}
	runes := []rune(body)
	if len(runes) > wikiSnippetRunes {
		return string(runes[:wikiSnippetRunes]) + "…"
	}
	return body
}

func isWikiFile(name string) bool {
	ext := strings.ToLower(filepath.Ext(name))
	switch ext {
	case ".md", ".markdown", ".txt", ".mdx":
		return true
	default:
		return false
	}
}

func wikiSnippet(body, query string) string {
	body = strings.TrimSpace(body)
	if body == "" {
		return ""
	}
	ql := strings.ToLower(query)
	lower := strings.ToLower(body)
	idx := strings.Index(lower, ql)
	runes := []rune(body)
	if idx < 0 {
		if len(runes) > wikiSnippetRunes {
			return string(runes[:wikiSnippetRunes]) + "…"
		}
		return body
	}
	// idx is byte index; map to rune start approximately via prefix
	prefix := body[:idx]
	start := len([]rune(prefix))
	from := start - 40
	if from < 0 {
		from = 0
	}
	to := from + wikiSnippetRunes
	if to > len(runes) {
		to = len(runes)
	}
	out := string(runes[from:to])
	if from > 0 {
		out = "…" + out
	}
	if to < len(runes) {
		out += "…"
	}
	return out
}

func wikiScore(rel, body, ql string) float64 {
	score := 0.5
	if strings.Contains(strings.ToLower(rel), ql) {
		score += 0.3
	}
	if strings.Contains(strings.ToLower(body), ql) {
		score += 0.2
	}
	return score
}

func readCapped(path string, max int) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	buf := make([]byte, max+1)
	n, err := f.Read(buf)
	if err != nil && n == 0 {
		return "", err
	}
	if n > max {
		n = max
	}
	// strip non-printable control except \n\t
	s := string(buf[:n])
	return sanitizeText(s), nil
}

func sanitizeText(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if r == '\n' || r == '\t' || r == '\r' || unicode.IsPrint(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func pathUnderRoot(root, full string) bool {
	root = filepath.Clean(root)
	full = filepath.Clean(full)
	sep := string(filepath.Separator)
	return full == root || strings.HasPrefix(full, root+sep)
}

var errStopWalk = fmt.Errorf("stop walk")

var _ WikiSearcher = (*DirWiki)(nil)
var _ WikiWriter = (*DirWiki)(nil)
