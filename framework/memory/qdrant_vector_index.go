package memory

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"sync"

	qdrant "github.com/qdrant/go-client/qdrant"
)

const (
	payloadScopeType = "scope_type"
	payloadScopeID   = "scope_id"
	defaultQdrantCollection = "sixath_memory_units"
	defaultQdrantGRPCPort   = 6334
)

// QdrantConfig configures the Qdrant units vector sidecar (P2-H).
type QdrantConfig struct {
	URL        string // e.g. http://localhost:6333 (REST); gRPC defaults to host:6334
	Collection string
	APIKey     string
	GRPCPort   int // optional override; 0 → derive from URL / default 6334
}

// qdrantClient is the subset of Qdrant operations used by QdrantVectorIndex (for tests).
type qdrantClient interface {
	CollectionExists(ctx context.Context, collectionName string) (bool, error)
	CreateCollection(ctx context.Context, request *qdrant.CreateCollection) error
	Upsert(ctx context.Context, request *qdrant.UpsertPoints) (*qdrant.UpdateResult, error)
	Delete(ctx context.Context, request *qdrant.DeletePoints) (*qdrant.UpdateResult, error)
	Query(ctx context.Context, request *qdrant.QueryPoints) ([]*qdrant.ScoredPoint, error)
	Close() error
}

// QdrantVectorIndex stores unit embeddings in Qdrant.
type QdrantVectorIndex struct {
	client     qdrantClient
	collection string
	mu         sync.Mutex
	ensured    bool
	dim        uint64
}

// NewQdrantVectorIndex connects to Qdrant via the official gRPC client.
func NewQdrantVectorIndex(cfg QdrantConfig) (*QdrantVectorIndex, error) {
	host, port, err := parseQdrantAddr(cfg)
	if err != nil {
		return nil, err
	}
	collection := strings.TrimSpace(cfg.Collection)
	if collection == "" {
		collection = defaultQdrantCollection
	}
	qc, err := qdrant.NewClient(&qdrant.Config{
		Host:   host,
		Port:   port,
		APIKey: strings.TrimSpace(cfg.APIKey),
	})
	if err != nil {
		return nil, err
	}
	return newQdrantVectorIndex(qc, collection), nil
}

func newQdrantVectorIndex(client qdrantClient, collection string) *QdrantVectorIndex {
	return &QdrantVectorIndex{client: client, collection: collection}
}

func parseQdrantAddr(cfg QdrantConfig) (host string, port int, err error) {
	raw := strings.TrimSpace(cfg.URL)
	if raw == "" {
		return "", 0, fmt.Errorf("memory: qdrant url required")
	}
	if !strings.Contains(raw, "://") {
		raw = "http://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", 0, fmt.Errorf("memory: qdrant url: %w", err)
	}
	host = u.Hostname()
	if host == "" {
		return "", 0, fmt.Errorf("memory: qdrant url missing host")
	}
	if cfg.GRPCPort > 0 {
		return host, cfg.GRPCPort, nil
	}
	if u.Port() != "" {
		p, err := strconv.Atoi(u.Port())
		if err != nil {
			return "", 0, fmt.Errorf("memory: qdrant url port: %w", err)
		}
		// REST default 6333 → gRPC 6334
		if p == 6333 {
			return host, defaultQdrantGRPCPort, nil
		}
		return host, p, nil
	}
	return host, defaultQdrantGRPCPort, nil
}

func (idx *QdrantVectorIndex) ensureCollection(ctx context.Context, dim int) error {
	idx.mu.Lock()
	defer idx.mu.Unlock()
	if idx.ensured && idx.dim == uint64(dim) {
		return nil
	}
	exists, err := idx.client.CollectionExists(ctx, idx.collection)
	if err != nil {
		return err
	}
	if !exists {
		size := uint64(dim)
		dist := qdrant.Distance_Cosine
		if err := idx.client.CreateCollection(ctx, &qdrant.CreateCollection{
			CollectionName: idx.collection,
			VectorsConfig: qdrant.NewVectorsConfig(&qdrant.VectorParams{
				Size:     size,
				Distance: dist,
			}),
		}); err != nil {
			return err
		}
	}
	idx.ensured = true
	idx.dim = uint64(dim)
	return nil
}

func (idx *QdrantVectorIndex) Upsert(ctx context.Context, rec UnitVectorRecord) error {
	if strings.TrimSpace(rec.UnitID) == "" {
		return fmt.Errorf("memory: unit id required")
	}
	if len(rec.Embedding) == 0 {
		return fmt.Errorf("memory: embedding required")
	}
	if err := idx.ensureCollection(ctx, len(rec.Embedding)); err != nil {
		return err
	}
	wait := true
	_, err := idx.client.Upsert(ctx, &qdrant.UpsertPoints{
		CollectionName: idx.collection,
		Wait:           &wait,
		Points: []*qdrant.PointStruct{{
			Id:      qdrant.NewID(rec.UnitID),
			Vectors: qdrant.NewVectorsDense(rec.Embedding),
			Payload: qdrant.NewValueMap(map[string]any{
				payloadScopeType: string(rec.Scope),
				payloadScopeID:   rec.ScopeID,
			}),
		}},
	})
	return err
}

func (idx *QdrantVectorIndex) Delete(ctx context.Context, memoryUnitID string) error {
	memoryUnitID = strings.TrimSpace(memoryUnitID)
	if memoryUnitID == "" {
		return nil
	}
	wait := true
	_, err := idx.client.Delete(ctx, &qdrant.DeletePoints{
		CollectionName: idx.collection,
		Wait:           &wait,
		Points:         qdrant.NewPointsSelector(qdrant.NewID(memoryUnitID)),
	})
	return err
}

func (idx *QdrantVectorIndex) Search(ctx context.Context, q VectorSearchQuery) ([]ScoredUnitID, error) {
	if len(q.Embedding) == 0 || q.Limit <= 0 {
		return nil, nil
	}
	if err := idx.ensureCollection(ctx, len(q.Embedding)); err != nil {
		return nil, err
	}
	limit := uint64(q.Limit)
	res, err := idx.client.Query(ctx, &qdrant.QueryPoints{
		CollectionName: idx.collection,
		Query:          qdrant.NewQueryDense(q.Embedding),
		Filter: &qdrant.Filter{
			Must: []*qdrant.Condition{
				qdrant.NewMatch(payloadScopeType, string(q.Scope)),
				qdrant.NewMatch(payloadScopeID, q.ScopeID),
			},
		},
		Limit: &limit,
	})
	if err != nil {
		return nil, err
	}
	out := make([]ScoredUnitID, 0, len(res))
	for _, sp := range res {
		id := pointIDString(sp.GetId())
		if id == "" {
			continue
		}
		out = append(out, ScoredUnitID{UnitID: id, Score: float64(sp.GetScore())})
	}
	return out, nil
}

func (idx *QdrantVectorIndex) Close() error {
	if idx == nil || idx.client == nil {
		return nil
	}
	return idx.client.Close()
}

func pointIDString(id *qdrant.PointId) string {
	if id == nil {
		return ""
	}
	switch v := id.GetPointIdOptions().(type) {
	case *qdrant.PointId_Uuid:
		return v.Uuid
	case *qdrant.PointId_Num:
		return strconv.FormatUint(v.Num, 10)
	default:
		return ""
	}
}
