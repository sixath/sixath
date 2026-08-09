package memory

import (
	"context"
	"database/sql"
	"encoding/binary"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync"

	_ "modernc.org/sqlite"
)

// SQLiteVectorIndex stores unit embeddings in a local SQLite file (brute-force cosine search).
type SQLiteVectorIndex struct {
	db *sql.DB
	mu sync.Mutex
}

// NewSQLiteVectorIndex opens or creates a SQLite vector sidecar at path.
func NewSQLiteVectorIndex(path string) (*SQLiteVectorIndex, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, fmt.Errorf("memory: sqlite vector index path required")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	idx := &SQLiteVectorIndex{db: db}
	if err := idx.migrate(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return idx, nil
}

func (idx *SQLiteVectorIndex) migrate() error {
	_, err := idx.db.Exec(`
CREATE TABLE IF NOT EXISTS unit_vectors (
  unit_id TEXT PRIMARY KEY,
  scope_type TEXT NOT NULL,
  scope_id TEXT NOT NULL,
  embedding BLOB NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_unit_vectors_scope ON unit_vectors(scope_type, scope_id);
`)
	return err
}

func (idx *SQLiteVectorIndex) Upsert(_ context.Context, rec UnitVectorRecord) error {
	if strings.TrimSpace(rec.UnitID) == "" {
		return fmt.Errorf("memory: unit id required")
	}
	if len(rec.Embedding) == 0 {
		return fmt.Errorf("memory: embedding required")
	}
	blob := encodeUnitEmbedding(rec.Embedding)
	idx.mu.Lock()
	defer idx.mu.Unlock()
	_, err := idx.db.Exec(
		`INSERT INTO unit_vectors(unit_id, scope_type, scope_id, embedding) VALUES(?,?,?,?)
		 ON CONFLICT(unit_id) DO UPDATE SET scope_type=excluded.scope_type, scope_id=excluded.scope_id, embedding=excluded.embedding`,
		rec.UnitID, string(rec.Scope), rec.ScopeID, blob,
	)
	return err
}

func (idx *SQLiteVectorIndex) Delete(_ context.Context, memoryUnitID string) error {
	memoryUnitID = strings.TrimSpace(memoryUnitID)
	if memoryUnitID == "" {
		return nil
	}
	idx.mu.Lock()
	defer idx.mu.Unlock()
	_, err := idx.db.Exec(`DELETE FROM unit_vectors WHERE unit_id=?`, memoryUnitID)
	return err
}

func (idx *SQLiteVectorIndex) Search(_ context.Context, q VectorSearchQuery) ([]ScoredUnitID, error) {
	if len(q.Embedding) == 0 || q.Limit <= 0 {
		return nil, nil
	}
	idx.mu.Lock()
	defer idx.mu.Unlock()
	rows, err := idx.db.Query(
		`SELECT unit_id, embedding FROM unit_vectors WHERE scope_type=? AND scope_id=?`,
		string(q.Scope), q.ScopeID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	type scored struct {
		id    string
		score float64
	}
	var all []scored
	for rows.Next() {
		var id string
		var blob []byte
		if err := rows.Scan(&id, &blob); err != nil {
			return nil, err
		}
		vec, err := decodeUnitEmbedding(blob)
		if err != nil {
			continue
		}
		sim := float64(cosineSimilarity(vec, q.Embedding))
		all = append(all, scored{id: id, score: sim})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := make([]ScoredUnitID, 0, min(q.Limit, len(all)))
	for i := 0; i < q.Limit && len(all) > 0; i++ {
		best := 0
		for j := 1; j < len(all); j++ {
			if all[j].score > all[best].score {
				best = j
			}
		}
		out = append(out, ScoredUnitID{UnitID: all[best].id, Score: all[best].score})
		all[best] = all[len(all)-1]
		all = all[:len(all)-1]
	}
	return out, nil
}

func (idx *SQLiteVectorIndex) Close() error {
	if idx == nil || idx.db == nil {
		return nil
	}
	return idx.db.Close()
}

func encodeUnitEmbedding(v []float32) []byte {
	b := make([]byte, 4+len(v)*4)
	binary.LittleEndian.PutUint32(b, uint32(len(v)))
	for i, f := range v {
		binary.LittleEndian.PutUint32(b[4+i*4:], math.Float32bits(f))
	}
	return b
}

func decodeUnitEmbedding(b []byte) ([]float32, error) {
	if len(b) < 4 {
		return nil, fmt.Errorf("short embedding blob")
	}
	n := int(binary.LittleEndian.Uint32(b))
	if n < 0 || len(b) < 4+n*4 {
		return nil, fmt.Errorf("invalid embedding blob")
	}
	out := make([]float32, n)
	for i := 0; i < n; i++ {
		out[i] = math.Float32frombits(binary.LittleEndian.Uint32(b[4+i*4:]))
	}
	return out, nil
}
