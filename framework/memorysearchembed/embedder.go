package memorysearchembed

import (
	"context"

	"github.com/sixath/framework/memorysearch"
	"github.com/sixath/framework/model"
)

// NewModelEmbedder 从 model.Model 创建 Embedder。
func NewModelEmbedder(m model.Model) memorysearch.Embedder {
	if m == nil {
		return nil
	}
	return &modelEmbedder{m: m}
}

type modelEmbedder struct {
	m model.Model
}

func (e *modelEmbedder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	if e == nil || e.m == nil {
		return nil, nil
	}
	embs, err := e.m.Embed(ctx, texts)
	if err != nil {
		return nil, err
	}
	out := make([][]float32, len(embs))
	for i, emb := range embs {
		out[i] = emb.Vector
	}
	return out, nil
}
