package agent

import (
	"strings"
	"testing"

	"github.com/sixath/framework/tool"
)

func TestEvaluateEvidenceGate(t *testing.T) {
	enabled := EvidenceGateConfig{Enabled: true}
	hard := EvidenceGateConfig{Enabled: true, HardHalt: true}
	disabled := EvidenceGateConfig{Enabled: false}

	tests := []struct {
		name      string
		cfg       EvidenceGateConfig
		refs      []tool.EvidenceRef
		finalText string
		wantAllow bool
		wantAction string
	}{
		{
			name:       "soft inject when no evidence",
			cfg:        enabled,
			refs:       nil,
			finalText:  "root cause is OOM in svc-a",
			wantAllow:  false,
			wantAction: "inject",
		},
		{
			name:       "证据不足 allow",
			cfg:        enabled,
			refs:       nil,
			finalText:  "本次无法定位，证据不足。",
			wantAllow:  true,
			wantAction: "",
		},
		{
			name:       "hard halt when no evidence",
			cfg:        hard,
			refs:       nil,
			finalText:  "root cause is OOM",
			wantAllow:  false,
			wantAction: "halt",
		},
		{
			name:       "has jaeger ref allow",
			cfg:        enabled,
			refs:       []tool.EvidenceRef{{Kind: "jaeger_trace", TraceID: "abc"}},
			finalText:  "trace shows timeout at gateway",
			wantAllow:  true,
			wantAction: "",
		},
		{
			name:       "disabled allow",
			cfg:        disabled,
			refs:       nil,
			finalText:  "guessing without tools",
			wantAllow:  true,
			wantAction: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := EvaluateEvidenceGate(tt.cfg, tt.refs, tt.finalText)
			if got.Allow != tt.wantAllow {
				t.Fatalf("Allow=%v want %v; result=%#v", got.Allow, tt.wantAllow, got)
			}
			if got.Action != tt.wantAction {
				t.Fatalf("Action=%q want %q; result=%#v", got.Action, tt.wantAction, got)
			}
			if tt.wantAction == "inject" {
				if got.Prompt == "" {
					t.Fatal("inject requires non-empty Prompt")
				}
				lower := strings.ToLower(got.Prompt)
				if !strings.Contains(got.Prompt, "证据不足") && !strings.Contains(lower, "insufficient") {
					t.Fatalf("Prompt should mention 证据不足 / insufficient: %q", got.Prompt)
				}
				if !strings.Contains(lower, "jaeger") && !strings.Contains(got.Prompt, "es") && !strings.Contains(lower, "trace") {
					t.Fatalf("Prompt should nudge jaeger/es evidence: %q", got.Prompt)
				}
			}
			if !tt.wantAllow && got.Reason == "" {
				t.Fatal("deny requires Reason")
			}
		})
	}
}

func TestEvaluateEvidenceGate_insufficientEvidenceEnglish(t *testing.T) {
	got := EvaluateEvidenceGate(
		EvidenceGateConfig{Enabled: true},
		nil,
		"Cannot conclude: insufficient evidence from available tools.",
	)
	if !got.Allow || got.Action != "" {
		t.Fatalf("expected allow on English insufficient phrase, got %#v", got)
	}
}

func TestEvaluateEvidenceGate_esLogRefAllow(t *testing.T) {
	got := EvaluateEvidenceGate(
		EvidenceGateConfig{Enabled: true},
		[]tool.EvidenceRef{{Kind: "es_log_query", Summary: "error spike"}},
		"logs show connection reset",
	)
	if !got.Allow || got.Action != "" {
		t.Fatalf("expected allow with es_log_query ref, got %#v", got)
	}
}

func TestEvaluateEvidenceGate_rcaGrepAloneNotEnough(t *testing.T) {
	got := EvaluateEvidenceGate(
		EvidenceGateConfig{Enabled: true},
		[]tool.EvidenceRef{{Kind: "rca_grep", Path: "main.go"}},
		"found a suspicious line",
	)
	if got.Allow || got.Action != "inject" {
		t.Fatalf("rca_grep alone should soft-inject, got %#v", got)
	}
}
