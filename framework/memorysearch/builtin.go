package memorysearch

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	_ "modernc.org/sqlite"
)

const (
	schemaVersion          = "memory_index_meta_v1"
	sessionDirtyDebounceMs = 500
)

type sessionPendingState struct {
	bytes    int
	messages int
}

// MemoryIndexManager builtin 实现。
type MemoryIndexManager struct {
	cfg              *ResolvedMemorySearchConfig
	workspace        string
	agentID          string
	db               *sql.DB
	embedder         Embedder
	sessionProvider  SessionTranscriptProvider
	mu               sync.Mutex
	syncing          bool
	dirty            bool
	sessionsDirty    bool
	sessionsDirtySet map[string]struct{}
	sessionPending   map[string]sessionPendingState
	closed           bool
	watcher          *fsnotify.Watcher
	watchCancel      func()
	intervalTicker   *time.Ticker
	intervalCancel   func()
}

// NewMemoryIndexManager 创建 builtin 管理器。
// sessionProvider 可选，当 sources 含 "sessions" 时用于获取会话转录。
func NewMemoryIndexManager(cfg *ResolvedMemorySearchConfig, workspace, agentID string, embedder Embedder, sessionProvider SessionTranscriptProvider) (*MemoryIndexManager, error) {
	if cfg == nil || cfg.StorePath == "" {
		return nil, fmt.Errorf("memorysearch: config or store path is empty")
	}
	dir := filepath.Dir(cfg.StorePath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("memorysearch: create store dir: %w", err)
	}
	db, err := sql.Open("sqlite", cfg.StorePath+"?_journal_mode=WAL")
	if err != nil {
		return nil, fmt.Errorf("memorysearch: open db: %w", err)
	}
	m := &MemoryIndexManager{
		cfg:              cfg,
		workspace:        workspace,
		agentID:          agentID,
		db:               db,
		embedder:         embedder,
		sessionProvider:  sessionProvider,
		sessionsDirtySet: make(map[string]struct{}),
		sessionPending:   make(map[string]sessionPendingState),
	}
	if err := m.ensureSchema(); err != nil {
		db.Close()
		return nil, err
	}
	m.ensureWatcher()
	m.ensureIntervalSync()
	return m, nil
}

func (m *MemoryIndexManager) ensureSchema() error {
	_, err := m.db.Exec(`
		CREATE TABLE IF NOT EXISTS meta (key TEXT PRIMARY KEY, value TEXT);
		CREATE TABLE IF NOT EXISTS files (path TEXT PRIMARY KEY, source TEXT, hash TEXT, mtime INTEGER, size INTEGER);
		CREATE TABLE IF NOT EXISTS chunks (
			id TEXT PRIMARY KEY, path TEXT, source TEXT, start_line INTEGER, end_line INTEGER,
			hash TEXT, model TEXT, text TEXT, embedding BLOB, updated_at INTEGER
		);
		CREATE VIRTUAL TABLE IF NOT EXISTS chunks_fts USING fts5(text, path, start_line, end_line, source);
		CREATE TABLE IF NOT EXISTS embedding_cache (
			hash_key TEXT PRIMARY KEY, embedding BLOB, dims INTEGER, updated_at INTEGER
		);
	`)
	return err
}

// Search 实现 MemorySearchManager。
func (m *MemoryIndexManager) Search(ctx context.Context, query string, opts *SearchOpts) ([]MemorySearchResult, error) {
	if m.closed {
		return nil, fmt.Errorf("memorysearch: manager closed")
	}
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, nil
	}
	maxResults := m.cfg.MaxResults
	minScore := m.cfg.MinScore
	if opts != nil {
		if opts.MaxResults > 0 {
			maxResults = opts.MaxResults
		}
		if opts.MinScore > 0 {
			minScore = opts.MinScore
		}
	}
	if (m.dirty || m.sessionsDirty) && m.cfg.OnSearch {
		go func() {
			_ = m.Sync(context.Background(), &SyncParams{Reason: "search"})
		}()
	}
	if m.embedder != nil && m.cfg.HybridEnabled {
		return m.searchHybrid(ctx, query, maxResults, minScore)
	}
	if m.embedder != nil {
		return m.searchVector(ctx, query, maxResults, minScore)
	}
	return m.searchFTS(ctx, query, maxResults, minScore)
}

func (m *MemoryIndexManager) searchFTS(ctx context.Context, query string, maxResults int, minScore float64) ([]MemorySearchResult, error) {
	keywords := extractKeywords(query)
	if len(keywords) == 0 {
		keywords = []string{strings.TrimSpace(query)}
	}
	if len(keywords) == 0 {
		return nil, nil
	}
	matchExpr := strings.Join(keywords, " OR ")
	rows, err := m.db.QueryContext(ctx, `
		SELECT path, start_line, end_line, text, source
		FROM chunks_fts
		WHERE chunks_fts MATCH ?
		LIMIT ?
	`, matchExpr, maxResults*3)
	if err != nil {
		return nil, fmt.Errorf("fts query: %w", err)
	}
	defer rows.Close()
	var results []MemorySearchResult
	for rows.Next() {
		var path, text, source string
		var startLine, endLine int
		if err := rows.Scan(&path, &startLine, &endLine, &text, &source); err != nil {
			continue
		}
		score := 1.0
		if score < minScore {
			continue
		}
		snippet := text
		if len(snippet) > 200 {
			snippet = snippet[:200] + "..."
		}
		results = append(results, MemorySearchResult{
			Path:      path,
			StartLine: startLine,
			EndLine:   endLine,
			Score:     score,
			Snippet:   snippet,
			Source:    source,
			Citation:  fmt.Sprintf("%s#L%d-%d", path, startLine, endLine),
		})
		if len(results) >= maxResults {
			break
		}
	}
	return results, nil
}

func (m *MemoryIndexManager) searchVector(ctx context.Context, query string, maxResults int, minScore float64) ([]MemorySearchResult, error) {
	vecs, err := m.embedder.Embed(ctx, []string{query})
	if err != nil || len(vecs) == 0 {
		return m.searchFTS(ctx, query, maxResults, minScore)
	}
	qvec := vecs[0]
	rows, err := m.db.QueryContext(ctx, `SELECT id, path, source, start_line, end_line, text, embedding FROM chunks WHERE embedding IS NOT NULL`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var scoredList []scoredResult
	for rows.Next() {
		var id, path, source, text string
		var startLine, endLine int
		var embBlob []byte
		if err := rows.Scan(&id, &path, &source, &startLine, &endLine, &text, &embBlob); err != nil {
			continue
		}
		if len(embBlob) < 8 {
			continue
		}
		n := binary.LittleEndian.Uint32(embBlob)
		if int(n)*4+4 > len(embBlob) {
			continue
		}
		vec := make([]float32, n)
		for i := range vec {
			vec[i] = math.Float32frombits(binary.LittleEndian.Uint32(embBlob[4+i*4:]))
		}
		sim := cosineSimilarity(vec, qvec)
		if float64(sim) < minScore {
			continue
		}
		snippet := text
		if len(snippet) > 200 {
			snippet = snippet[:200] + "..."
		}
		scoredList = append(scoredList, scoredResult{
			r: MemorySearchResult{
				Path:      path,
				StartLine: startLine,
				EndLine:   endLine,
				Score:     float64(sim),
				Snippet:   snippet,
				Source:    source,
				Citation:  fmt.Sprintf("%s#L%d-%d", path, startLine, endLine),
			},
			score: float64(sim),
		})
	}
	return topK(scoredList, maxResults), nil
}

func (m *MemoryIndexManager) searchHybrid(ctx context.Context, query string, maxResults int, minScore float64) ([]MemorySearchResult, error) {
	candidates := maxResults * m.cfg.CandidateMultiplier
	if candidates >= 200 {
		candidates = 200
	}
	if candidates < 1 {
		candidates = 1
	}
	var ftsResults []MemorySearchResult
	var vecResults []MemorySearchResult
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		ftsResults, _ = m.searchFTS(ctx, query, candidates, 0)
		wg.Done()
	}()
	go func() {
		vecResults, _ = m.searchVector(ctx, query, candidates, 0)
		wg.Done()
	}()
	wg.Wait()
	merged := mergeHybridResults(ftsResults, vecResults, m.cfg.VectorWeight, m.cfg.TextWeight, maxResults, minScore)
	return merged, nil
}

type scoredResult struct {
	r     MemorySearchResult
	score float64
}

// ReadFile 实现 MemorySearchManager。
func (m *MemoryIndexManager) ReadFile(ctx context.Context, params *ReadFileParams) (*ReadFileResult, error) {
	if m.closed {
		return nil, fmt.Errorf("memorysearch: manager closed")
	}
	if params == nil || params.RelPath == "" {
		return nil, fmt.Errorf("memorysearch: relPath is required")
	}
	cleaned := filepath.Clean(params.RelPath)
	if strings.Contains(cleaned, "..") {
		return nil, fmt.Errorf("memorysearch: path must not escape workspace")
	}
	if !strings.HasSuffix(strings.ToLower(cleaned), ".md") {
		return nil, fmt.Errorf("memorysearch: only .md files allowed")
	}

	var text string
	norm := filepath.ToSlash(cleaned)
	if strings.HasPrefix(norm, "sessions/") && m.sessionProvider != nil && m.hasSource("sessions") {
		trimmed := strings.TrimPrefix(norm, "sessions/")
		sessionID := strings.TrimSuffix(trimmed, ".md")
		var err error
		text, err = m.sessionProvider.GetTranscript(ctx, sessionID)
		if err != nil {
			return nil, err
		}
	} else {
		fullPath := filepath.Join(m.workspace, cleaned)
		absFull, err := filepath.Abs(fullPath)
		if err != nil {
			return nil, err
		}
		absWorkspace, _ := filepath.Abs(m.workspace)
		if !strings.HasPrefix(absFull, absWorkspace+string(filepath.Separator)) && absFull != absWorkspace {
			return nil, fmt.Errorf("memorysearch: path must be under workspace")
		}
		data, err := os.ReadFile(absFull)
		if err != nil {
			return nil, err
		}
		text = string(data)
	}
	if params.From > 0 || params.Lines > 0 {
		lines := strings.Split(text, "\n")
		from := params.From - 1
		if from < 0 {
			from = 0
		}
		to := from + params.Lines
		if params.Lines <= 0 {
			to = len(lines)
		}
		if from >= len(lines) {
			text = ""
		} else {
			if to > len(lines) {
				to = len(lines)
			}
			text = strings.Join(lines[from:to], "\n")
		}
	}
	return &ReadFileResult{Text: text, Path: params.RelPath}, nil
}

// Status 实现 MemorySearchManager。
func (m *MemoryIndexManager) Status(ctx context.Context) (*MemoryProviderStatus, error) {
	if m.closed {
		return nil, fmt.Errorf("memorysearch: manager closed")
	}
	var files, chunks int
	_ = m.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM files`).Scan(&files)
	_ = m.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM chunks`).Scan(&chunks)
	var cache int
	_ = m.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM embedding_cache`).Scan(&cache)
	return &MemoryProviderStatus{
		Backend:  "builtin",
		Provider: m.cfg.Provider,
		Model:    m.cfg.Model,
		Files:    files,
		Chunks:   chunks,
		Vector:   m.embedder != nil,
		FTS:      true,
		Cache:    cache,
	}, nil
}

// Sync 实现 MemorySearchManager。
func (m *MemoryIndexManager) Sync(ctx context.Context, params *SyncParams) error {
	if m.closed {
		return fmt.Errorf("memorysearch: manager closed")
	}
	m.mu.Lock()
	if m.syncing {
		m.mu.Unlock()
		return fmt.Errorf("memorysearch: sync already in progress")
	}
	m.syncing = true
	m.mu.Unlock()
	defer func() {
		m.mu.Lock()
		m.syncing = false
		m.dirty = false
		m.sessionsDirty = false
		m.sessionsDirtySet = make(map[string]struct{})
		m.mu.Unlock()
	}()
	return m.runSync(ctx, params)
}

func (m *MemoryIndexManager) runSync(ctx context.Context, params *SyncParams) error {
	progress := func(phase string, cur, total int) {
		if params != nil && params.Progress != nil {
			params.Progress(phase, cur, total)
		}
	}
	activePaths := make(map[string]struct{})

	// memory 源
	if m.hasSource("memory") {
		paths := m.listMemoryFiles()
		progress("indexing", 0, len(paths))
		for i, relPath := range paths {
			fullPath := filepath.Join(m.workspace, relPath)
			activePaths[relPath] = struct{}{}
			if err := m.indexFile(ctx, relPath, fullPath, nil, "memory", params != nil && params.Force, progress); err != nil {
				continue
			}
			progress("indexing", i+1, len(paths))
		}
	}

	// sessions 源（Phase 2）
	if m.hasSource("sessions") && m.sessionProvider != nil {
		if err := m.syncSessionFiles(ctx, params, activePaths, progress); err != nil {
			// 不阻断，仅记录
		}
	}

	return m.pruneDeleted(ctx, activePaths)
}

func (m *MemoryIndexManager) hasSource(name string) bool {
	for _, s := range m.cfg.Sources {
		if s == name {
			return true
		}
	}
	return false
}

func (m *MemoryIndexManager) syncSessionFiles(ctx context.Context, params *SyncParams, activePaths map[string]struct{}, progress func(string, int, int)) error {
	sessionIDs, err := m.sessionProvider.ListSessionsForAgent(ctx, m.agentID)
	if err != nil {
		return err
	}
	m.mu.Lock()
	dirtySet := m.sessionsDirtySet
	indexAll := params != nil && params.Force
	m.mu.Unlock()

	for i, sessionID := range sessionIDs {
		relPath := "sessions/" + sessionID + ".md"
		activePaths[relPath] = struct{}{}
		if !indexAll {
			if _, ok := dirtySet[sessionID]; !ok {
				continue
			}
		}
		content, err := m.sessionProvider.GetTranscript(ctx, sessionID)
		if err != nil {
			continue
		}
		if err := m.indexFileWithContent(ctx, relPath, content, "sessions", progress); err != nil {
			continue
		}
		progress("sessions", i+1, len(sessionIDs))
	}
	return nil
}

// NotifySessionDirty 由上层在会话转录更新时调用（Phase 2 session-delta）。
// bytesDelta、messagesDelta 为本次更新增量；累积超过配置的 deltaBytes/deltaMessages 时触发 sync。
func (m *MemoryIndexManager) NotifySessionDirty(sessionID string, bytesDelta, messagesDelta int) {
	if sessionID == "" || !m.hasSource("sessions") {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	cur := m.sessionPending[sessionID]
	cur.bytes += bytesDelta
	cur.messages += messagesDelta
	m.sessionPending[sessionID] = cur

	deltaB := m.cfg.SessionsDeltaBytes
	deltaM := m.cfg.SessionsDeltaMessages
	if deltaB <= 0 {
		deltaB = 4096
	}
	if deltaM <= 0 {
		deltaM = 5
	}
	if cur.bytes >= deltaB || cur.messages >= deltaM {
		m.sessionsDirtySet[sessionID] = struct{}{}
		m.sessionsDirty = true
		delete(m.sessionPending, sessionID)
		// 防抖后异步 sync
		go func() {
			time.Sleep(time.Duration(sessionDirtyDebounceMs) * time.Millisecond)
			_ = m.Sync(context.Background(), &SyncParams{Reason: "session-delta"})
		}()
	}
}

func (m *MemoryIndexManager) listMemoryFiles() []string {
	var paths []string
	seen := make(map[string]struct{})
	add := func(p string) {
		if _, ok := seen[p]; ok {
			return
		}
		seen[p] = struct{}{}
		paths = append(paths, p)
	}
	// MEMORY.md, memory.md
	for _, name := range []string{"MEMORY.md", "memory.md"} {
		fp := filepath.Join(m.workspace, name)
		if st, err := os.Stat(fp); err == nil && !st.IsDir() {
			add(name)
		}
	}
	// memory/**/*.md
	memDir := filepath.Join(m.workspace, "memory")
	if st, err := os.Stat(memDir); err == nil && st.IsDir() {
		filepath.Walk(memDir, func(path string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() {
				return nil
			}
			if strings.HasSuffix(strings.ToLower(info.Name()), ".md") {
				rel, _ := filepath.Rel(m.workspace, path)
				if !strings.Contains(rel, "..") {
					add(rel)
				}
			}
			return nil
		})
	}
	for _, extra := range m.cfg.ExtraPaths {
		cleaned := filepath.Clean(extra)
		if strings.Contains(cleaned, "..") {
			continue
		}
		fp := filepath.Join(m.workspace, cleaned)
		if st, err := os.Stat(fp); err == nil && !st.IsDir() && strings.HasSuffix(strings.ToLower(cleaned), ".md") {
			add(cleaned)
		} else if err == nil && st.IsDir() {
			filepath.Walk(fp, func(path string, info os.FileInfo, err error) error {
				if err != nil || info.IsDir() {
					return nil
				}
				if strings.HasSuffix(strings.ToLower(info.Name()), ".md") {
					rel, _ := filepath.Rel(m.workspace, path)
					if !strings.Contains(rel, "..") {
						add(rel)
					}
				}
				return nil
			})
		}
	}
	return paths
}

func (m *MemoryIndexManager) indexFile(ctx context.Context, relPath, fullPath string, content *string, source string, force bool, progress func(string, int, int)) error {
	var data []byte
	var mtime int64
	if content != nil {
		data = []byte(*content)
		mtime = time.Now().Unix()
	} else {
		var err error
		data, err = os.ReadFile(fullPath)
		if err != nil {
			return err
		}
		if info, e := os.Stat(fullPath); e == nil {
			mtime = info.ModTime().Unix()
		}
	}
	text := string(data)
	hash := hashFile(text)
	var existingHash string
	_ = m.db.QueryRowContext(ctx, `SELECT hash FROM files WHERE path=? AND source=?`, relPath, source).Scan(&existingHash)
	if existingHash == hash && !force {
		return nil
	}
	chunks := ChunkMarkdown(text, m.cfg.ChunkTokens, m.cfg.ChunkOverlap)
	_, _ = m.db.ExecContext(ctx, `DELETE FROM chunks WHERE path=? AND source=?`, relPath, source)
	_, _ = m.db.ExecContext(ctx, `DELETE FROM chunks_fts WHERE path=?`, relPath)
	for i, ch := range chunks {
		var embBlob []byte
		if m.embedder != nil {
			vecs, err := m.embedder.Embed(ctx, []string{ch.Text})
			if err == nil && len(vecs) > 0 {
				embBlob = encodeEmbedding(vecs[0])
			}
		}
		id := fmt.Sprintf("%s:%d", relPath, i)
		_, err := m.db.ExecContext(ctx, `INSERT INTO chunks (id, path, source, start_line, end_line, hash, model, text, embedding, updated_at) VALUES (?,?,?,?,?,?,?,?,?,?)`,
			id, relPath, source, ch.StartLine, ch.EndLine, ch.Hash, m.cfg.Model, ch.Text, embBlob, time.Now().Unix())
		if err != nil {
			continue
		}
		_, _ = m.db.ExecContext(ctx, `INSERT INTO chunks_fts(text, path, start_line, end_line, source) VALUES (?,?,?,?,?)`,
			ch.Text, relPath, ch.StartLine, ch.EndLine, source)
		progress("indexing", i+1, len(chunks))
	}
	_, _ = m.db.ExecContext(ctx, `INSERT OR REPLACE INTO files (path, source, hash, mtime, size) VALUES (?,?,?,?,?)`,
		relPath, source, hash, mtime, len(data))
	return nil
}

func (m *MemoryIndexManager) indexFileWithContent(ctx context.Context, relPath, content, source string, progress func(string, int, int)) error {
	return m.indexFile(ctx, relPath, "", &content, source, true, progress)
}

func (m *MemoryIndexManager) pruneDeleted(ctx context.Context, activePaths map[string]struct{}) error {
	rows, _ := m.db.QueryContext(ctx, `SELECT path, source FROM files`)
	if rows == nil {
		return nil
	}
	defer rows.Close()
	for rows.Next() {
		var path, source string
		if err := rows.Scan(&path, &source); err != nil {
			continue
		}
		if _, ok := activePaths[path]; !ok {
			_, _ = m.db.ExecContext(ctx, `DELETE FROM chunks WHERE path=? AND source=?`, path, source)
			_, _ = m.db.ExecContext(ctx, `DELETE FROM chunks_fts WHERE path=?`, path)
			_, _ = m.db.ExecContext(ctx, `DELETE FROM files WHERE path=?`, path)
		}
	}
	return nil
}

func (m *MemoryIndexManager) ensureWatcher() {
	if !m.cfg.Watch || m.workspace == "" {
		return
	}
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return
	}
	m.watcher = watcher
	debounce := m.cfg.WatchDebounceMs
	if debounce <= 0 {
		debounce = 300
	}
	var timer *time.Timer
	ctx, cancel := context.WithCancel(context.Background())
	m.watchCancel = cancel

	watchDirs := make(map[string]struct{})
	addDir := func(dir string) {
		if _, ok := watchDirs[dir]; ok {
			return
		}
		if st, err := os.Stat(dir); err == nil && st.IsDir() {
			_ = watcher.Add(dir)
			watchDirs[dir] = struct{}{}
		}
	}
	addDir(filepath.Join(m.workspace, "memory"))
	addDir(m.workspace)
	for _, extra := range m.cfg.ExtraPaths {
		p := filepath.Join(m.workspace, filepath.Clean(extra))
		addDir(p)
	}

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case event, ok := <-watcher.Events:
				if !ok {
					return
				}
				if event.Op&(fsnotify.Write|fsnotify.Create) != 0 && strings.HasSuffix(strings.ToLower(event.Name), ".md") {
					if timer != nil {
						timer.Stop()
					}
					timer = time.AfterFunc(time.Duration(debounce)*time.Millisecond, func() {
						m.mu.Lock()
						m.dirty = true
						m.mu.Unlock()
						_ = m.Sync(context.Background(), &SyncParams{Reason: "watch"})
					})
				}
			case <-watcher.Errors:
				// ignore
			}
		}
	}()
}

func (m *MemoryIndexManager) ensureIntervalSync() {
	if m.cfg.IntervalMinutes <= 0 {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	m.intervalCancel = cancel
	m.intervalTicker = time.NewTicker(time.Duration(m.cfg.IntervalMinutes) * time.Minute)
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-m.intervalTicker.C:
				_ = m.Sync(context.Background(), &SyncParams{Reason: "interval"})
			}
		}
	}()
}

// Close 关闭管理器。
func (m *MemoryIndexManager) Close() error {
	m.mu.Lock()
	m.closed = true
	m.mu.Unlock()
	if m.watchCancel != nil {
		m.watchCancel()
	}
	if m.intervalCancel != nil {
		m.intervalCancel()
	}
	if m.intervalTicker != nil {
		m.intervalTicker.Stop()
	}
	if m.watcher != nil {
		_ = m.watcher.Close()
	}
	if m.db != nil {
		return m.db.Close()
	}
	return nil
}

func extractKeywords(s string) []string {
	words := strings.FieldsFunc(s, func(r rune) bool {
		return r < 'A' || (r > 'Z' && r < 'a') || r > 'z'
	})
	var out []string
	seen := make(map[string]struct{})
	for _, w := range words {
		w = strings.TrimSpace(strings.ToLower(w))
		if len(w) >= 2 && w != "or" && w != "and" {
			if _, ok := seen[w]; !ok {
				seen[w] = struct{}{}
				out = append(out, w)
			}
		}
	}
	return out
}

func cosineSimilarity(a, b []float32) float32 {
	if len(a) == 0 || len(b) == 0 || len(a) != len(b) {
		return 0
	}
	var dot, na, nb float32
	for i := range a {
		dot += a[i] * b[i]
		na += a[i] * a[i]
		nb += b[i] * b[i]
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return dot / float32(math.Sqrt(float64(na*nb)))
}

func encodeEmbedding(v []float32) []byte {
	b := make([]byte, 4+len(v)*4)
	binary.LittleEndian.PutUint32(b, uint32(len(v)))
	for i, f := range v {
		binary.LittleEndian.PutUint32(b[4+i*4:], math.Float32bits(f))
	}
	return b
}

func topK(list []scoredResult, k int) []MemorySearchResult {
	if k <= 0 || len(list) == 0 {
		return nil
	}
	for i := 0; i < k && i < len(list); i++ {
		best := i
		for j := i + 1; j < len(list); j++ {
			if list[j].score > list[best].score {
				best = j
			}
		}
		list[i], list[best] = list[best], list[i]
	}
	out := make([]MemorySearchResult, 0, min(k, len(list)))
	for i := 0; i < k && i < len(list); i++ {
		out = append(out, list[i].r)
	}
	return out
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func mergeHybridResults(fts, vec []MemorySearchResult, vWeight, tWeight float64, maxResults int, minScore float64) []MemorySearchResult {
	byPath := make(map[string]MemorySearchResult)
	for _, r := range fts {
		key := fmt.Sprintf("%s:%d:%d", r.Path, r.StartLine, r.EndLine)
		score := r.Score * tWeight
		if s, ok := byPath[key]; !ok || s.Score < score {
			r.Score = score
			byPath[key] = r
		}
	}
	for _, r := range vec {
		key := fmt.Sprintf("%s:%d:%d", r.Path, r.StartLine, r.EndLine)
		score := r.Score * vWeight
		if s, ok := byPath[key]; !ok {
			r.Score = score
			byPath[key] = r
		} else {
			byPath[key] = MemorySearchResult{
				Path: r.Path, StartLine: r.StartLine, EndLine: r.EndLine,
				Score: s.Score + score, Snippet: r.Snippet, Source: r.Source, Citation: r.Citation,
			}
		}
	}
	var list []MemorySearchResult
	for _, r := range byPath {
		if r.Score >= minScore {
			list = append(list, r)
		}
	}
	for i := 0; i < maxResults && i < len(list); i++ {
		best := i
		for j := i + 1; j < len(list); j++ {
			if list[j].Score > list[best].Score {
				best = j
			}
		}
		list[i], list[best] = list[best], list[i]
	}
	if len(list) > maxResults {
		list = list[:maxResults]
	}
	return list
}

func hashFile(content string) string {
	h := sha256.Sum256([]byte(content))
	return hex.EncodeToString(h[:])
}
