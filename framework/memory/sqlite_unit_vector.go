package memory

import (
	"context"
	"database/sql"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

// SQLiteUnitVectorIndex stores unit vectors in a standalone SQLite file.
type SQLiteUnitVectorIndex struct {
	mu     sync.RWMutex
	db     *sql.DB
	dims   int
	closed bool
}

var _ UnitVectorIndex = (*SQLiteUnitVectorIndex)(nil)

// NewSQLiteUnitVectorIndex opens (or creates) a standalone SQLite file for unit vectors.
// If the file already has rows, the in-process dimension baseline is restored from any
// existing row so reopen keeps rejecting dimension mismatches.
func NewSQLiteUnitVectorIndex(path string) (*SQLiteUnitVectorIndex, error) {
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("memory: unit vector dir: %w", err)
		}
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("memory: open unit vector db: %w", err)
	}
	if _, err := db.Exec(`
		PRAGMA journal_mode=WAL;
		CREATE TABLE IF NOT EXISTS unit_vectors (
			scope_type TEXT NOT NULL,
			scope_id   TEXT NOT NULL,
			unit_id    TEXT NOT NULL,
			dims       INTEGER NOT NULL,
			embedding  BLOB NOT NULL,
			updated_at INTEGER NOT NULL,
			PRIMARY KEY (scope_type, scope_id, unit_id)
		);
		CREATE INDEX IF NOT EXISTS idx_uv_scope ON unit_vectors(scope_type, scope_id);
	`); err != nil {
		db.Close()
		return nil, fmt.Errorf("memory: init unit vector schema: %w", err)
	}

	idx := &SQLiteUnitVectorIndex{db: db}
	// Restore the dimension baseline so a reopened index keeps rejecting mismatches.
	var dims int
	if err := db.QueryRow(`SELECT dims FROM unit_vectors LIMIT 1`).Scan(&dims); err == nil {
		idx.dims = dims
	} else if !errors.Is(err, sql.ErrNoRows) {
		db.Close()
		return nil, fmt.Errorf("memory: read unit vector dims: %w", err)
	}
	return idx, nil
}

// validateDims checks vector length against the baseline.
// When the index has no baseline yet it returns needAdopt=true; the caller must
// adopt only after a successful write so a failed first Upsert does not pin dims.
func (s *SQLiteUnitVectorIndex) validateDims(n int) (needAdopt bool, err error) {
	if n == 0 {
		return false, fmt.Errorf("memory: empty vector")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.dims == 0 {
		return true, nil
	}
	if s.dims != n {
		return false, fmt.Errorf("%w: vector dim %d != index dim %d", ErrVectorDimMismatch, n, s.dims)
	}
	return false, nil
}

func (s *SQLiteUnitVectorIndex) adoptDims(n int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.dims == 0 {
		s.dims = n
	}
}

func (s *SQLiteUnitVectorIndex) Upsert(ctx context.Context, rec UnitVectorEntry) error {
	if err := s.errIfClosed(); err != nil {
		return err
	}
	needAdopt, err := s.validateDims(len(rec.Vector))
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO unit_vectors (scope_type, scope_id, unit_id, dims, embedding, updated_at)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(scope_type, scope_id, unit_id)
		DO UPDATE SET dims=excluded.dims, embedding=excluded.embedding, updated_at=excluded.updated_at
	`, string(rec.Scope), rec.ScopeID, rec.UnitID, len(rec.Vector), encodeVector(rec.Vector), time.Now().Unix())
	if err != nil {
		return fmt.Errorf("memory: upsert unit vector: %w", err)
	}
	if needAdopt {
		s.adoptDims(len(rec.Vector))
	}
	return nil
}

func (s *SQLiteUnitVectorIndex) Delete(ctx context.Context, scope Scope, scopeID string, unitIDs ...string) error {
	if len(unitIDs) == 0 {
		return nil
	}
	if err := s.errIfClosed(); err != nil {
		return err
	}
	for _, id := range unitIDs {
		if _, err := s.db.ExecContext(ctx,
			`DELETE FROM unit_vectors WHERE scope_type=? AND scope_id=? AND unit_id=?`,
			string(scope), scopeID, id); err != nil {
			return fmt.Errorf("memory: delete unit vector: %w", err)
		}
	}
	return nil
}

func (s *SQLiteUnitVectorIndex) Search(ctx context.Context, q UnitVectorQuery) ([]UnitVectorHit, error) {
	if err := s.errIfClosed(); err != nil {
		return nil, err
	}
	if q.Limit <= 0 || len(q.Vector) == 0 {
		return nil, nil
	}
	if _, err := s.validateDims(len(q.Vector)); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT unit_id, embedding FROM unit_vectors WHERE scope_type=? AND scope_id=?`,
		string(q.Scope), q.ScopeID)
	if err != nil {
		return nil, fmt.Errorf("memory: search unit vectors: %w", err)
	}
	defer rows.Close()

	var hits []UnitVectorHit
	for rows.Next() {
		var unitID string
		var blob []byte
		if err := rows.Scan(&unitID, &blob); err != nil {
			return nil, fmt.Errorf("memory: scan unit vector: %w", err)
		}
		if len(blob) != len(q.Vector)*4 {
			return nil, fmt.Errorf("memory: corrupt embedding for unit %s: blob len=%d, want %d", unitID, len(blob), len(q.Vector)*4)
		}
		score := float64(cosineSimilarity(decodeVector(blob), q.Vector))
		if q.MinScore != 0 && score < q.MinScore {
			continue
		}
		hits = append(hits, UnitVectorHit{UnitID: unitID, Score: score})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("memory: iterate unit vectors: %w", err)
	}
	sort.Slice(hits, func(i, j int) bool {
		if hits[i].Score == hits[j].Score {
			return hits[i].UnitID < hits[j].UnitID
		}
		return hits[i].Score > hits[j].Score
	})
	if len(hits) > q.Limit {
		hits = hits[:q.Limit]
	}
	return hits, nil
}

func (s *SQLiteUnitVectorIndex) Has(ctx context.Context, scope Scope, scopeID string, unitIDs []string) (map[string]bool, error) {
	out := make(map[string]bool, len(unitIDs))
	if len(unitIDs) == 0 {
		return out, nil
	}
	if err := s.errIfClosed(); err != nil {
		return nil, err
	}
	// Build IN clause placeholders.
	args := make([]any, 0, 2+len(unitIDs))
	args = append(args, string(scope), scopeID)
	ph := make([]byte, 0, len(unitIDs)*2)
	for i, id := range unitIDs {
		if i > 0 {
			ph = append(ph, ',')
		}
		ph = append(ph, '?')
		args = append(args, id)
	}
	q := `SELECT unit_id FROM unit_vectors WHERE scope_type=? AND scope_id=? AND unit_id IN (` + string(ph) + `)`
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("memory: has unit vectors: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("memory: scan has unit vector: %w", err)
		}
		out[id] = true
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("memory: iterate has unit vectors: %w", err)
	}
	return out, nil
}

func (s *SQLiteUnitVectorIndex) Close() error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	db := s.db
	s.mu.Unlock()
	return db.Close()
}

func (s *SQLiteUnitVectorIndex) errIfClosed() error {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.closed {
		return fmt.Errorf("memory: unit vector index closed")
	}
	return nil
}

// encodeVector packs float32 values as little-endian bytes with no length prefix;
// the dims column carries the element count.
func encodeVector(v []float32) []byte {
	buf := make([]byte, 4*len(v))
	for i, f := range v {
		binary.LittleEndian.PutUint32(buf[i*4:], math.Float32bits(f))
	}
	return buf
}

// decodeVector unpacks a little-endian float32 blob (no length prefix).
func decodeVector(b []byte) []float32 {
	out := make([]float32, len(b)/4)
	for i := range out {
		out[i] = math.Float32frombits(binary.LittleEndian.Uint32(b[i*4:]))
	}
	return out
}
