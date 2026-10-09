package skills

import (
	"context"
	"math"
	"strings"
	"sync"
)

// EmbedFunc 对一批文本生成向量，与 memorysearch.Embedder 的签名对齐。
type EmbedFunc func(ctx context.Context, texts []string) ([][]float32, error)

// DefaultEmbedRouteThreshold 是语义路由的默认余弦相似度阈值（bge-m3 中文场景经验值）。
const DefaultEmbedRouteThreshold = 0.5

// embedRouteCacheCap 防止 router 被多会话共享时缓存无限增长；达到上限后整体清空重建
//（单条缓存重建只需一次 embed 调用，代价可接受）。
const embedRouteCacheCap = 256

// EmbedRouter 基于 embedding 余弦相似度做 Skill 语义路由。
// 构建时对全部 Skill 的 "name + description + tags" 做一次批量 embed；
// 运行时对 query 单次 embed 并取 top-1，超过阈值才视为命中。
// 所有 embed 失败都返回未命中，由调用方静默回退到模型自主 load_skill。
type EmbedRouter struct {
	entries   []embedRouteEntry
	threshold float64
	embed     EmbedFunc

	mu    sync.Mutex
	cache map[string]embedRouteResult
}

type embedRouteEntry struct {
	meta   SkillMeta
	vector []float32
}

type embedRouteResult struct {
	meta  SkillMeta
	score float64
	ok    bool
}

// NewEmbedRouter 构建语义路由器。idx 为空、无 Skill、embed 为 nil 或批量 embed 失败时返回 (nil, err)/(nil, nil)，
// 调用方应将 nil router 视为「未启用语义路由」。
// threshold <= 0 时使用 DefaultEmbedRouteThreshold。
func NewEmbedRouter(ctx context.Context, idx *Index, embed EmbedFunc, threshold float64) (*EmbedRouter, error) {
	if idx == nil || embed == nil {
		return nil, nil
	}
	metas := VisibleSkills(idx.All())
	if len(metas) == 0 {
		return nil, nil
	}
	if threshold <= 0 {
		threshold = DefaultEmbedRouteThreshold
	}

	texts := make([]string, len(metas))
	for i, m := range metas {
		texts[i] = embedRouteText(m)
	}
	vectors, err := embed(ctx, texts)
	if err != nil {
		return nil, err
	}
	if len(vectors) != len(metas) {
		return nil, nil
	}

	entries := make([]embedRouteEntry, 0, len(metas))
	for i, m := range metas {
		if len(vectors[i]) == 0 {
			continue
		}
		entries = append(entries, embedRouteEntry{meta: m, vector: vectors[i]})
	}
	if len(entries) == 0 {
		return nil, nil
	}
	return &EmbedRouter{
		entries:   entries,
		threshold: threshold,
		embed:     embed,
		cache:     make(map[string]embedRouteResult),
	}, nil
}

// embedRouteText 拼接参与向量化的 Skill 文本：名称 + 描述 + 标签。
func embedRouteText(m SkillMeta) string {
	var b strings.Builder
	b.WriteString(m.Name)
	if d := strings.TrimSpace(m.Description); d != "" {
		b.WriteString("\n")
		b.WriteString(d)
	}
	if len(m.Tags) > 0 {
		b.WriteString("\n")
		b.WriteString(strings.Join(m.Tags, " "))
	}
	return b.String()
}

// Route 返回与 query 语义最接近的 Skill；仅当 top-1 相似度 >= 阈值时 ok=true。
// query 为空或 embed 失败时返回未命中。同一 query 在 router 生命周期内只 embed 一次。
func (r *EmbedRouter) Route(ctx context.Context, query string) (SkillMeta, float64, bool) {
	if r == nil || len(r.entries) == 0 {
		return SkillMeta{}, 0, false
	}
	q := strings.TrimSpace(query)
	if q == "" {
		return SkillMeta{}, 0, false
	}

	r.mu.Lock()
	if hit, ok := r.cache[q]; ok {
		r.mu.Unlock()
		return hit.meta, hit.score, hit.ok
	}
	r.mu.Unlock()

	vectors, err := r.embed(ctx, []string{q})
	if err != nil || len(vectors) == 0 || len(vectors[0]) == 0 {
		return SkillMeta{}, 0, false
	}
	qv := vectors[0]

	best := -1
	var bestScore float64
	for i := range r.entries {
		score := cosineSimilarity32(qv, r.entries[i].vector)
		if best < 0 || score > bestScore {
			best = i
			bestScore = score
		}
	}

	result := embedRouteResult{ok: false}
	if best >= 0 && bestScore >= r.threshold {
		result = embedRouteResult{meta: r.entries[best].meta, score: bestScore, ok: true}
	}

	r.mu.Lock()
	if len(r.cache) >= embedRouteCacheCap {
		r.cache = make(map[string]embedRouteResult)
	}
	r.cache[q] = result
	r.mu.Unlock()

	return result.meta, result.score, result.ok
}

// cosineSimilarity32 计算两个向量的余弦相似度；任一向量长度为 0 或长度不一致时返回 0。
func cosineSimilarity32(a, b []float32) float64 {
	if len(a) == 0 || len(a) != len(b) {
		return 0
	}
	var dot, na, nb float64
	for i := range a {
		fa, fb := float64(a[i]), float64(b[i])
		dot += fa * fb
		na += fa * fa
		nb += fb * fb
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return dot / (math.Sqrt(na) * math.Sqrt(nb))
}
