package vault

import (
	"context"
	"testing"

	"github.com/sixath/framework/memory"
)

type fakeEmbedder struct{}

func (fakeEmbedder) Embed(ctx context.Context, agentID string, texts []string) ([][]float32, error) {
	out := make([][]float32, len(texts))
	for i, s := range texts {
		// 简单确定性向量：长度对 7 取模 + 常数，保证可复现。
		out[i] = []float32{float32(len(s) % 7), 1}
	}
	return out, nil
}

func TestRecallHits(t *testing.T) {
	idx := memory.NewInMemoryUnitVectorIndex()
	_ = idx.Upsert(context.Background(), memory.UnitVectorEntry{
		Scope: "vault", ScopeID: "vault", UnitID: "ops/es-flow.md", Vector: []float32{3, 1},
	})
	byID := map[string]Note{
		"ops/es-flow.md": {Path: "ops/es-flow.md", Body: "查流水"},
	}
	hits, err := Recall(context.Background(), fakeEmbedder{}, idx, byID, "es 流水", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) == 0 {
		t.Fatal("expected a hit")
	}
	if hits[0].Note.Path != "ops/es-flow.md" {
		t.Fatalf("bad path: %s", hits[0].Note.Path)
	}
}

func TestRecallNilInputs(t *testing.T) {
	if hits, err := Recall(context.Background(), nil, nil, nil, "", 0); err != nil || hits != nil {
		t.Fatalf("nil inputs should return nil,nil; got %v,%v", hits, err)
	}
}
