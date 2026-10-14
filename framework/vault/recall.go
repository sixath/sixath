package vault

import (
	"context"

	"github.com/sixath/framework/memory"
)

// Hit 一条召回到的经验片段。
type Hit struct {
	Note  Note
	Score float64
}

// Recall 对 query 做语义召回，返回命中的笔记。
// byID 把 unitID（笔记相对 vault 根的路径）映射回 Note。
// embed 把 query 向量化；idx 是共享的向量索引，scope 固定用 "vault"/"vault"。
func Recall(ctx context.Context, embed memory.UnitEmbedder, idx memory.UnitVectorIndex, byID map[string]Note, query string, limit int) ([]Hit, error) {
	if embed == nil || idx == nil || query == "" {
		return nil, nil
	}
	vecs, err := embed.Embed(ctx, "", []string{query})
	if err != nil {
		return nil, err
	}
	if len(vecs) == 0 || len(vecs[0]) == 0 {
		return nil, nil
	}
	uhs, err := idx.Search(ctx, memory.UnitVectorQuery{
		Scope: "vault", ScopeID: "vault", Vector: vecs[0], Limit: limit,
	})
	if err != nil {
		return nil, err
	}
	out := make([]Hit, 0, len(uhs))
	for _, uh := range uhs {
		n, ok := byID[uh.UnitID]
		if !ok {
			continue
		}
		out = append(out, Hit{Note: n, Score: uh.Score})
	}
	return out, nil
}
