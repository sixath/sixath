package middleware

import (
	"context"
	"testing"

	agent "github.com/sixath/framework/harness"
)

func TestMetricsMiddleware_TokenObserved(t *testing.T) {
	tests := []struct {
		name       string
		metadata   map[string]any
		wantCalled bool
		wantInput  int
		wantOutput int
	}{
		{"float64 (json default)", map[string]any{"token_input": float64(100), "token_output": float64(50)}, true, 100, 50},
		{"int", map[string]any{"token_input": 7, "token_output": 8}, true, 7, 8},
		{"only input present", map[string]any{"token_input": float64(100)}, true, 100, 0},
		{"only output present", map[string]any{"token_output": float64(50)}, true, 0, 50},
		{"no token fields", map[string]any{"agent_name": "x"}, false, 0, 0},
		{"nil metadata", nil, false, 0, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var calledIn, calledOut int
			called := false
			oldHook := observeTokenUsage
			observeTokenUsage = func(agent string, in, out int) {
				called = true
				calledIn = in
				calledOut = out
			}
			defer func() { observeTokenUsage = oldHook }()

			final := func(ctx context.Context, req *agent.Request) (*agent.Response, error) {
				return &agent.Response{Metadata: tt.metadata}, nil
			}
			h := MetricsMiddleware(final)
			_, err := h(context.Background(), &agent.Request{})
			if err != nil {
				t.Fatalf("unexpected err: %v", err)
			}
			if called != tt.wantCalled {
				t.Fatalf("called = %v, want %v", called, tt.wantCalled)
			}
			if !called {
				return
			}
			if calledIn != tt.wantInput || calledOut != tt.wantOutput {
				t.Errorf("called(in=%d,out=%d), want (in=%d,out=%d)", calledIn, calledOut, tt.wantInput, tt.wantOutput)
			}
		})
	}
}
