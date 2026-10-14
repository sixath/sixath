package vault

import (
	"context"
	"path/filepath"

	"github.com/sixath/framework/memory"
)

const (
	vaultScope   memory.Scope = "vault"
	vaultScopeID              = "vault"
)

// Sync 把 root 下所有 promoted 笔记 embed 后 upsert 进向量索引。
// unitID 约定 = 笔记相对 root 的路径（用 filepath.Rel）。
// prev 是「上一次 Sync 时的 unitID 集合」；其中已不在当前 Scan 结果里的 unitID 会被 Delete。
// 首次调用 prev 传 nil。
func Sync(ctx context.Context, embed memory.UnitEmbedder, idx memory.UnitVectorIndex, root string, prev []string) ([]string, error) {
	notes, err := Scan(root)
	if err != nil {
		return nil, err
	}
	current := make([]string, 0, len(notes))
	currentSet := make(map[string]bool, len(notes))
	for _, n := range notes {
		rel, err := filepath.Rel(root, n.Path)
		if err != nil {
			rel = n.Path
		}
		rel = filepath.ToSlash(rel) // 统一用正斜杠，跨平台稳定（与 Recall 的 unitID 约定一致）
		current = append(current, rel)
		currentSet[rel] = true

		vecs, err := embed.Embed(ctx, "", []string{n.Body})
		if err != nil {
			return nil, err
		}
		if len(vecs) == 0 || len(vecs[0]) == 0 {
			continue
		}
		if err := idx.Upsert(ctx, memory.UnitVectorEntry{
			Scope: vaultScope, ScopeID: vaultScopeID, UnitID: rel, Vector: vecs[0],
		}); err != nil {
			return nil, err
		}
	}
	// 清理已删除的笔记。
	for _, old := range prev {
		if !currentSet[old] {
			_ = idx.Delete(ctx, vaultScope, vaultScopeID, old)
		}
	}
	return current, nil
}
