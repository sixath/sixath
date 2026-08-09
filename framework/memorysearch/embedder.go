package memorysearch

import "context"

// Embedder 用于生成文本向量的接口。可为 nil 表示仅 FTS 模式。
type Embedder interface {
	Embed(ctx context.Context, texts []string) ([][]float32, error)
}
