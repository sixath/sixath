package middleware

import (
	"context"
	"testing"
	"time"

	agent "github.com/sixath/framework/harness"
	"github.com/sixath/framework/model"
)

func TestAgentContext_CacheHitSource(t *testing.T) {
	var status string
	old := observeAgentRequest
	observeAgentRequest = func(_ string, st string, _ time.Duration) {
		status = st
	}
	defer func() { observeAgentRequest = old }()

	store := NewCacheStore(0)
	req := &agent.Request{AgentName: "dq"}
	// Metrics 须在 Cache 外侧，cache hit 时仍能上报 source=cache。
	h := Chain(
		func(ctx context.Context, req *agent.Request) (*agent.Response, error) {
			return &agent.Response{Text: "live"}, nil
		},
		MetricsMiddleware,
		CacheMiddleware(store),
	)
	_, _ = h(context.Background(), req)
	_, _ = h(context.Background(), req)
	if status != "cache" {
		t.Fatalf("status = %q, want cache", status)
	}
}

func TestAgentContext_BlockedSource(t *testing.T) {
	var status string
	old := observeAgentRequest
	observeAgentRequest = func(_ string, st string, _ time.Duration) {
		status = st
	}
	defer func() { observeAgentRequest = old }()

	filter := &SimpleBlocklistFilter{Blocked: []string{"bad"}}
	h := Chain(
		func(ctx context.Context, req *agent.Request) (*agent.Response, error) {
			return &agent.Response{Text: "ok"}, nil
		},
		MetricsMiddleware,
		ContentSafetyMiddleware(filter),
	)
	_, _ = h(context.Background(), &agent.Request{
		Messages: []model.Message{{Role: "user", Content: "bad"}},
	})
	if status != "blocked" {
		t.Fatalf("status = %q, want blocked", status)
	}
}

func TestAgentContext_NotInheritedAcrossRequests(t *testing.T) {
	h := Chain(func(ctx context.Context, req *agent.Request) (*agent.Response, error) {
		ac := agent.ContextFrom(ctx)
		if ac != nil {
			ac.SetExtra("flag", true)
		}
		return &agent.Response{}, nil
	})
	_, _ = h(context.Background(), &agent.Request{})
	ctx2, ac2 := agent.EnsureContext(context.Background())
	if _, ok := ac2.Extra("flag"); ok {
		t.Fatal("extra leaked to fresh context")
	}
	_ = ctx2
}
