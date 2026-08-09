package middleware

import (
	"context"
	"testing"

	"github.com/sixath/framework/agent"
	"github.com/sixath/framework/model"
)

func TestAhoCorasickFilter_BasicMatch(t *testing.T) {
	f := NewAhoCorasickFilter([]string{"bad", "word"})
	if err := f.CheckInput("this is bad"); err == nil {
		t.Fatal("expected block")
	}
	if err := f.CheckInput("good text"); err != nil {
		t.Fatal(err)
	}
}

func TestContentSafetyMiddleware_PartsCheck(t *testing.T) {
	f := NewAhoCorasickFilter([]string{"secret"})
	h := ContentSafetyMiddleware(f)(func(ctx context.Context, req *agent.Request) (*agent.Response, error) {
		return &agent.Response{Text: "ok"}, nil
	})
	_, err := h(context.Background(), &agent.Request{
		Messages: []model.Message{{
			Role:    "user",
			Content: "hello",
			Parts:   []model.ContentPart{{Text: "contains secret word"}},
		}},
	})
	if err == nil {
		t.Fatal("expected block on part text")
	}
}
