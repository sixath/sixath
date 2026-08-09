package middleware

import (
	"context"
	"testing"
	"time"

	"github.com/sixath/framework/agent"
	"github.com/sixath/framework/model"
)

func TestDefaultCacheKey_Deterministic(t *testing.T) {
	k := &DefaultCacheKey{Version: 1}
	req := &agent.Request{
		Messages: []model.Message{{Role: "user", Content: "hi"}},
		Metadata: map[string]any{"model": "gpt-4", "temperature": 0.7},
	}
	a := k.BuildKey(req)
	req2 := &agent.Request{
		Messages: []model.Message{{Role: "user", Content: "hi"}},
		Metadata: map[string]any{"temperature": 0.7, "model": "gpt-4"},
	}
	if a != k.BuildKey(req2) {
		t.Fatal("metadata order should not affect key")
	}
}

func TestDefaultCacheKey_RoleCase(t *testing.T) {
	k := &DefaultCacheKey{Version: 1}
	a := k.BuildKey(&agent.Request{Messages: []model.Message{{Role: "User", Content: "x"}}})
	b := k.BuildKey(&agent.Request{Messages: []model.Message{{Role: "user", Content: "x"}}})
	if a != b {
		t.Fatal("role case should normalize")
	}
}

func TestDefaultCacheKey_PartsAndModel(t *testing.T) {
	k := &DefaultCacheKey{Version: 1}
	base := &agent.Request{
		Messages: []model.Message{{Role: "user", Content: "caption", Parts: []model.ContentPart{{Type: model.ContentTypeImageURL, URL: "http://a/img.png"}}}},
		Metadata: map[string]any{"model": "m1"},
	}
	otherImg := &agent.Request{
		Messages: []model.Message{{Role: "user", Content: "caption", Parts: []model.ContentPart{{Type: model.ContentTypeImageURL, URL: "http://b/img.png"}}}},
		Metadata: map[string]any{"model": "m1"},
	}
	otherModel := &agent.Request{
		Messages: []model.Message{{Role: "user", Content: "caption", Parts: []model.ContentPart{{Type: model.ContentTypeImageURL, URL: "http://a/img.png"}}}},
		Metadata: map[string]any{"model": "m2"},
	}
	if k.BuildKey(base) == k.BuildKey(otherImg) {
		t.Fatal("different image URL must differ")
	}
	if k.BuildKey(base) == k.BuildKey(otherModel) {
		t.Fatal("different model must differ")
	}
}

func TestCacheMiddleware_ImageCacheBug(t *testing.T) {
	store := NewCacheStore(time.Minute)
	calls := 0
	h := CacheMiddleware(store)(func(ctx context.Context, req *agent.Request) (*agent.Response, error) {
		calls++
		url := ""
		if len(req.Messages) > 0 && len(req.Messages[0].Parts) > 0 {
			url = req.Messages[0].Parts[0].URL
		}
		return &agent.Response{Text: url}, nil
	})
	reqA := &agent.Request{Messages: []model.Message{{Role: "user", Content: "same", Parts: []model.ContentPart{{URL: "http://a"}}}}}
	reqB := &agent.Request{Messages: []model.Message{{Role: "user", Content: "same", Parts: []model.ContentPart{{URL: "http://b"}}}}}
	_, _ = h(context.Background(), reqA)
	_, _ = h(context.Background(), reqB)
	if calls != 2 {
		t.Fatalf("expected 2 handler calls (cache miss), got %d", calls)
	}
}
