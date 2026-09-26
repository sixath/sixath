package skills

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// testIndexFromMetas 直接由元数据构造 Index（同包测试可写私有字段）。
func testIndexFromMetas(metas ...SkillMeta) *Index {
	idx := &Index{skills: metas, byName: make(map[string]SkillMeta, len(metas))}
	for _, m := range metas {
		idx.byName[m.Name] = m
	}
	return idx
}

// fakeEmbedByKeyword 按文本内容返回确定性向量：
// 含 "alpha" → [1,0,0]；含 "beta" → [0,1,0]；含 "ortho" → [0,0,1]；含 "tilt" → [1,0.8,0]。
// calls 记录每次 embed 的文本数，用于验证缓存。
func fakeEmbedByKeyword(calls *int) EmbedFunc {
	return func(ctx context.Context, texts []string) ([][]float32, error) {
		if calls != nil {
			*calls += len(texts)
		}
		out := make([][]float32, len(texts))
		for i, t := range texts {
			switch {
			case strings.Contains(t, "alpha"):
				out[i] = []float32{1, 0, 0}
			case strings.Contains(t, "beta"):
				out[i] = []float32{0, 1, 0}
			case strings.Contains(t, "ortho"):
				out[i] = []float32{0, 0, 1}
			case strings.Contains(t, "tilt"):
				out[i] = []float32{1, 0.8, 0}
			default:
				out[i] = []float32{0, 0, 1}
			}
		}
		return out, nil
	}
}

func TestNewEmbedRouter_NilInputs(t *testing.T) {
	r, err := NewEmbedRouter(context.Background(), nil, fakeEmbedByKeyword(nil), 0)
	if err != nil || r != nil {
		t.Fatalf("nil index: want (nil,nil), got r=%v err=%v", r, err)
	}
	r, err = NewEmbedRouter(context.Background(), testIndexFromMetas(SkillMeta{Name: "alpha"}), nil, 0)
	if err != nil || r != nil {
		t.Fatalf("nil embed: want (nil,nil), got r=%v err=%v", r, err)
	}
}

func TestNewEmbedRouter_EmptyIndex(t *testing.T) {
	r, err := NewEmbedRouter(context.Background(), testIndexFromMetas(), fakeEmbedByKeyword(nil), 0)
	if err != nil || r != nil {
		t.Fatalf("empty index: want (nil,nil), got r=%v err=%v", r, err)
	}
}

func TestNewEmbedRouter_BuildEmbedFailure(t *testing.T) {
	failEmbed := func(ctx context.Context, texts []string) ([][]float32, error) {
		return nil, errors.New("embed service down")
	}
	r, err := NewEmbedRouter(context.Background(), testIndexFromMetas(SkillMeta{Name: "alpha"}), failEmbed, 0)
	if err == nil || r != nil {
		t.Fatalf("build failure: want (nil,err), got r=%v err=%v", r, err)
	}
}

func TestEmbedRouter_RouteHitsTop1(t *testing.T) {
	idx := testIndexFromMetas(
		SkillMeta{Name: "alpha-skill", Description: "handles alpha"},
		SkillMeta{Name: "beta-skill", Description: "handles beta"},
	)
	r, err := NewEmbedRouter(context.Background(), idx, fakeEmbedByKeyword(nil), 0.5)
	if err != nil || r == nil {
		t.Fatalf("build: err=%v r=%v", err, r)
	}
	// query 含 "alpha" → [1,0,0]，与 alpha-skill 完全同向（cos=1），与 beta 正交
	m, score, ok := r.Route(context.Background(), "please do alpha thing")
	if !ok || m.Name != "alpha-skill" {
		t.Fatalf("want alpha-skill hit, got ok=%v name=%s score=%.3f", ok, m.Name, score)
	}
	if score < 0.99 {
		t.Fatalf("want score≈1, got %.3f", score)
	}
}

func TestEmbedRouter_RouteBelowThreshold(t *testing.T) {
	idx := testIndexFromMetas(SkillMeta{Name: "alpha-skill", Description: "handles alpha"})
	r, err := NewEmbedRouter(context.Background(), idx, fakeEmbedByKeyword(nil), 0.5)
	if err != nil || r == nil {
		t.Fatalf("build: err=%v r=%v", err, r)
	}
	// "ortho" → [0,0,1]，与 alpha 向量 [1,0,0] 正交 → cos=0 < 0.5
	_, score, ok := r.Route(context.Background(), "ortho unrelated question")
	if ok {
		t.Fatalf("orthogonal query must not hit, got score=%.3f", score)
	}
}

func TestEmbedRouter_RouteEmptyQuery(t *testing.T) {
	calls := 0
	idx := testIndexFromMetas(SkillMeta{Name: "alpha-skill"})
	r, _ := NewEmbedRouter(context.Background(), idx, fakeEmbedByKeyword(&calls), 0.5)
	callsBefore := calls
	if _, _, ok := r.Route(context.Background(), "   "); ok {
		t.Fatal("blank query must not hit")
	}
	if calls != callsBefore {
		t.Fatalf("blank query must not call embed, calls %d → %d", callsBefore, calls)
	}
}

func TestEmbedRouter_RouteCachesQuery(t *testing.T) {
	calls := 0
	idx := testIndexFromMetas(SkillMeta{Name: "alpha-skill", Description: "handles alpha"})
	r, err := NewEmbedRouter(context.Background(), idx, fakeEmbedByKeyword(&calls), 0.5)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	// 构建时批量 embed 1 个 skill → calls=1
	callsBefore := calls
	q := "alpha same question"
	m1, s1, ok1 := r.Route(context.Background(), q)
	m2, s2, ok2 := r.Route(context.Background(), q)
	m3, s3, ok3 := r.Route(context.Background(), q+" ") // TrimSpace 后同一条
	if !ok1 || !ok2 || !ok3 || m1.Name != m2.Name || m2.Name != m3.Name || s1 != s2 || s2 != s3 {
		t.Fatalf("cached routes must be identical: (%s,%.3f,%v) (%s,%.3f,%v) (%s,%.3f,%v)",
			m1.Name, s1, ok1, m2.Name, s2, ok2, m3.Name, s3, ok3)
	}
	if calls != callsBefore+1 {
		t.Fatalf("same query must embed exactly once: want %d, got %d", callsBefore+1, calls)
	}
}

func TestEmbedRouter_RouteEmbedFailureAtQueryTime(t *testing.T) {
	// 构建成功，仅当文本含 "fail-query" 时失败（不影响构建期的批量 embed）
	failAtQuery := func(ctx context.Context, texts []string) ([][]float32, error) {
		for _, txt := range texts {
			if strings.Contains(txt, "fail-query") {
				return nil, errors.New("transient embed error")
			}
		}
		return fakeEmbedByKeyword(nil)(ctx, texts)
	}
	idx := testIndexFromMetas(SkillMeta{Name: "gamma-skill", Description: "handles gamma"})
	r, err := NewEmbedRouter(context.Background(), idx, failAtQuery, 0.5)
	if err != nil || r == nil {
		t.Fatalf("build must succeed: err=%v", err)
	}
	_, _, ok := r.Route(context.Background(), "this is a fail-query case")
	if ok {
		t.Fatal("query-time embed failure must return no-hit, not panic")
	}
}

func TestEmbedRouter_DefaultThreshold(t *testing.T) {
	idx := testIndexFromMetas(SkillMeta{Name: "alpha-skill"})
	// threshold=0 → 默认 0.5；"tilt" 查询向量 [1,0.8,0] 与 skill [1,0,0] 的 cos ≈ 0.78 > 0.5 命中
	r, err := NewEmbedRouter(context.Background(), idx, fakeEmbedByKeyword(nil), 0)
	if err != nil || r == nil {
		t.Fatalf("build: %v", err)
	}
	if r.threshold != DefaultEmbedRouteThreshold {
		t.Fatalf("want default threshold %.2f, got %.2f", DefaultEmbedRouteThreshold, r.threshold)
	}
	_, score, ok := r.Route(context.Background(), "tilt query")
	if !ok {
		t.Fatalf("cos≈0.78 should pass default threshold, got score=%.3f", score)
	}
}

func TestCosineSimilarity32(t *testing.T) {
	cases := []struct {
		name string
		a, b []float32
		want float64
	}{
		{"identical", []float32{1, 0, 0}, []float32{1, 0, 0}, 1.0},
		{"orthogonal", []float32{1, 0}, []float32{0, 1}, 0.0},
		{"opposite", []float32{1, 0}, []float32{-1, 0}, -1.0},
		{"empty", nil, []float32{1}, 0.0},
		{"length mismatch", []float32{1, 0}, []float32{1}, 0.0},
		{"zero vector", []float32{0, 0}, []float32{1, 0}, 0.0},
	}
	for _, c := range cases {
		got := cosineSimilarity32(c.a, c.b)
		diff := got - c.want
		if diff < 0 {
			diff = -diff
		}
		if diff > 1e-6 {
			t.Errorf("%s: want %.3f, got %.6f", c.name, c.want, got)
		}
	}
}
