package memory

import (
	"context"
	"math"
	"sort"
	"sync"
	"testing"

	qdrant "github.com/qdrant/go-client/qdrant"
)

type fakeQdrantPoint struct {
	vec     []float32
	payload map[string]*qdrant.Value
}

type fakeQdrantClient struct {
	mu         sync.Mutex
	exists     bool
	dim        uint64
	collection string
	points     map[string]*fakeQdrantPoint
	closed     bool
}

func newFakeQdrantClient() *fakeQdrantClient {
	return &fakeQdrantClient{points: make(map[string]*fakeQdrantPoint)}
}

func (f *fakeQdrantClient) CollectionExists(ctx context.Context, collectionName string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.exists && (f.collection == "" || f.collection == collectionName), nil
}

func (f *fakeQdrantClient) CreateCollection(ctx context.Context, request *qdrant.CreateCollection) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.exists = true
	f.collection = request.GetCollectionName()
	if cfg := request.GetVectorsConfig(); cfg != nil {
		if params := cfg.GetParams(); params != nil {
			f.dim = params.GetSize()
		}
	}
	return nil
}

func (f *fakeQdrantClient) Upsert(ctx context.Context, request *qdrant.UpsertPoints) (*qdrant.UpdateResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, p := range request.GetPoints() {
		id := pointIDString(p.GetId())
		vec := denseFromVectors(p.GetVectors())
		cp := make([]float32, len(vec))
		copy(cp, vec)
		pl := p.GetPayload()
		f.points[id] = &fakeQdrantPoint{vec: cp, payload: pl}
	}
	return &qdrant.UpdateResult{}, nil
}

func (f *fakeQdrantClient) Delete(ctx context.Context, request *qdrant.DeletePoints) (*qdrant.UpdateResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	sel := request.GetPoints()
	if sel == nil {
		return &qdrant.UpdateResult{}, nil
	}
	if ids := sel.GetPoints(); ids != nil {
		for _, id := range ids.GetIds() {
			delete(f.points, pointIDString(id))
		}
	}
	return &qdrant.UpdateResult{}, nil
}

func (f *fakeQdrantClient) Query(ctx context.Context, request *qdrant.QueryPoints) ([]*qdrant.ScoredPoint, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	qvec := denseFromQuery(request.GetQuery())
	type hit struct {
		id    string
		score float32
	}
	var hits []hit
	for id, p := range f.points {
		if !payloadMatchesFilter(p.payload, request.GetFilter()) {
			continue
		}
		hits = append(hits, hit{id: id, score: cosine32(qvec, p.vec)})
	}
	sort.Slice(hits, func(i, j int) bool { return hits[i].score > hits[j].score })
	limit := len(hits)
	if request.Limit != nil && int(*request.Limit) < limit {
		limit = int(*request.Limit)
	}
	out := make([]*qdrant.ScoredPoint, 0, limit)
	for i := 0; i < limit; i++ {
		out = append(out, &qdrant.ScoredPoint{
			Id:    qdrant.NewID(hits[i].id),
			Score: hits[i].score,
		})
	}
	return out, nil
}

func (f *fakeQdrantClient) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.closed = true
	return nil
}

func denseFromVectors(v *qdrant.Vectors) []float32 {
	if v == nil {
		return nil
	}
	if vec := v.GetVector(); vec != nil {
		if dense := vec.GetDense(); dense != nil {
			return dense.GetData()
		}
		// legacy flatten field (deprecated)
		return vec.GetData()
	}
	return nil
}

func denseFromQuery(q *qdrant.Query) []float32 {
	if q == nil {
		return nil
	}
	if nearest := q.GetNearest(); nearest != nil {
		if dense := nearest.GetDense(); dense != nil {
			return dense.GetData()
		}
	}
	return nil
}

func payloadMatchesFilter(payload map[string]*qdrant.Value, filter *qdrant.Filter) bool {
	if filter == nil {
		return true
	}
	for _, cond := range filter.GetMust() {
		field := cond.GetField()
		if field == nil {
			return false
		}
		key := field.GetKey()
		want := ""
		if m := field.GetMatch(); m != nil {
			want = m.GetKeyword()
		}
		got := ""
		if v := payload[key]; v != nil {
			got = v.GetStringValue()
		}
		if got != want {
			return false
		}
	}
	return true
}

func cosine32(a, b []float32) float32 {
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	var dot, na, nb float64
	for i := 0; i < n; i++ {
		dot += float64(a[i]) * float64(b[i])
		na += float64(a[i]) * float64(a[i])
		nb += float64(b[i]) * float64(b[i])
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return float32(dot / (math.Sqrt(na) * math.Sqrt(nb)))
}

func TestParseQdrantAddr(t *testing.T) {
	host, port, err := parseQdrantAddr(QdrantConfig{URL: "http://localhost:6333"})
	if err != nil || host != "localhost" || port != 6334 {
		t.Fatalf("rest→grpc: host=%q port=%d err=%v", host, port, err)
	}
	host, port, err = parseQdrantAddr(QdrantConfig{URL: "localhost:6333", GRPCPort: 7000})
	if err != nil || host != "localhost" || port != 7000 {
		t.Fatalf("grpc override: host=%q port=%d err=%v", host, port, err)
	}
	if _, _, err := parseQdrantAddr(QdrantConfig{}); err == nil {
		t.Fatal("empty url should error")
	}
}

func TestQdrantVectorIndex_UpsertSearchDelete(t *testing.T) {
	fake := newFakeQdrantClient()
	idx := newQdrantVectorIndex(fake, "test_units")
	defer idx.Close()

	ctx := context.Background()
	a := []float32{1, 0, 0}
	b := []float32{0.9, 0.1, 0}
	c := []float32{0, 1, 0}

	if err := idx.Upsert(ctx, UnitVectorRecord{UnitID: "u1", Scope: ScopeSession, ScopeID: "s1", Embedding: a}); err != nil {
		t.Fatal(err)
	}
	if err := idx.Upsert(ctx, UnitVectorRecord{UnitID: "u2", Scope: ScopeSession, ScopeID: "s1", Embedding: b}); err != nil {
		t.Fatal(err)
	}
	if err := idx.Upsert(ctx, UnitVectorRecord{UnitID: "u3", Scope: ScopeSession, ScopeID: "s2", Embedding: c}); err != nil {
		t.Fatal(err)
	}

	hits, err := idx.Search(ctx, VectorSearchQuery{Scope: ScopeSession, ScopeID: "s1", Embedding: a, Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 2 {
		t.Fatalf("want 2 hits, got %#v", hits)
	}
	if hits[0].UnitID != "u1" {
		t.Fatalf("top should be u1, got %#v", hits)
	}
	for _, h := range hits {
		if h.UnitID == "u3" {
			t.Fatalf("leaked other scope: %#v", hits)
		}
	}

	if err := idx.Delete(ctx, "u1"); err != nil {
		t.Fatal(err)
	}
	hits, err = idx.Search(ctx, VectorSearchQuery{Scope: ScopeSession, ScopeID: "s1", Embedding: a, Limit: 5})
	if err != nil {
		t.Fatal(err)
	}
	for _, h := range hits {
		if h.UnitID == "u1" {
			t.Fatalf("deleted u1 still returned: %#v", hits)
		}
	}
}

func TestQdrantVectorIndex_UpsertReplaces(t *testing.T) {
	fake := newFakeQdrantClient()
	idx := newQdrantVectorIndex(fake, "test_units")
	defer idx.Close()
	ctx := context.Background()
	if err := idx.Upsert(ctx, UnitVectorRecord{UnitID: "u1", Scope: ScopeUser, ScopeID: "u", Embedding: []float32{1, 0}}); err != nil {
		t.Fatal(err)
	}
	if err := idx.Upsert(ctx, UnitVectorRecord{UnitID: "u1", Scope: ScopeUser, ScopeID: "u", Embedding: []float32{0, 1}}); err != nil {
		t.Fatal(err)
	}
	hits, err := idx.Search(ctx, VectorSearchQuery{Scope: ScopeUser, ScopeID: "u", Embedding: []float32{0, 1}, Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 || hits[0].UnitID != "u1" {
		t.Fatalf("got %#v", hits)
	}
	if hits[0].Score < 0.99 {
		t.Fatalf("expected near 1 after replace, score=%v", hits[0].Score)
	}
}
