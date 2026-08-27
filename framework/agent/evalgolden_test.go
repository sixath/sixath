package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/sixath/framework/tool"
)

func TestEvalGolden_e9d4(t *testing.T) {
	cat := tool.ToolCatalog{Entries: []tool.ToolCatalogEntry{{
		Name: "execute_read", Available: true,
		Bindings:    map[string]string{"datasource_id": "prod_mysql", "type": "mysql"},
		SearchHints: []string{"mysql", "数据库", "host", "password"},
	}}}
	ctx := context.WithValue(context.Background(), tool.ContextKeyToolCatalog, cat)
	trace := &RunTrace{ToolCalls: []ToolCallRecord{{
		ToolName: "es_log_query", Allowed: true, Result: map[string]any{"ok": true},
	}}}
	ask := "请提供 MySQL 的 Host、Port、用户名、密码"
	if _, _, ok := credentialSolicitationRedirect(ctx, ask, 0, trace, "GetGameInfo 失败原因"); ok {
		t.Fatal("already used bound evidence this turn; must not redirect")
	}
}

func TestEvalGolden_c304(t *testing.T) {
	src := helperSource()
	got := EvaluateScenarioPathGate("区域已有用户（errcode=1105）会不会写本地映射？",
		"区域已有用户时会把 UID 写入本地映射表。", src)
	if got.Allow {
		t.Fatalf("1105 write prose must fail, got %#v", got)
	}
}

func TestEvalGolden_c7aa(t *testing.T) {
	// E2：去掉 Skip，在本测试断言入边 / inbound_empty。
	t.Skip("E2 inbound")
}

func emptyESTrace() *RunTrace {
	return &RunTrace{ToolCalls: []ToolCallRecord{{
		ToolName: "es_log_query",
		Result:   map[string]any{"ok": true, "hit_status": "empty", "queried_index": "vm-manager-*"},
	}}}
}

func TestEvalGolden_empty_hit(t *testing.T) {
	tr := emptyESTrace()
	got := EvaluateEmptyHitSpeakGate(tr, "该服务从未参与")
	if got.Allow || got.Reason != "empty_hit_speak" {
		t.Fatalf("T1 %#v", got)
	}
	got = EvaluateEmptyHitSpeakGate(tr, "该索引 0 条，不能据此说从未参与，查了 vm-manager-*")
	if !got.Allow {
		t.Fatalf("T2 %#v", got)
	}
	got = EvaluateEmptyHitSpeakGate(tr, "Redis 里 key 不存在")
	if !got.Allow {
		t.Fatalf("T3 %#v", got)
	}
	got = EvaluateEmptyHitSpeakGate(&RunTrace{ToolCalls: []ToolCallRecord{{
		ToolName: "es_log_query",
		Result:   map[string]any{"hits": []any{}},
	}}}, "该服务从未参与")
	if !got.Allow {
		t.Fatalf("T7 fail-open %#v", got)
	}
}

func TestEvalGolden_empty_hit_unscoped(t *testing.T) {
	got := EvaluateEmptyHitSpeakGate(emptyESTrace(), "这条链路没有参与")
	if got.Allow {
		t.Fatalf("T4 %#v", got)
	}
}

func TestEvalGolden_empty_hit_scoped_ok(t *testing.T) {
	got := EvaluateEmptyHitSpeakGate(emptyESTrace(), "vm-manager-* 上没有匹配行")
	if !got.Allow {
		t.Fatalf("T5 %#v", got)
	}
}

func TestEvalGolden_empty_hit_grep_ignored(t *testing.T) {
	got := EvaluateEmptyHitSpeakGate(&RunTrace{ToolCalls: []ToolCallRecord{{
		ToolName: "rca_grep",
		Result:   map[string]any{"hit_status": "empty", "repo": "service-a"},
	}}}, "该服务从未参与")
	if !got.Allow {
		t.Fatalf("T6 %#v", got)
	}
}

func TestEvalGolden_empty_hit_deny_a_with_index(t *testing.T) {
	got := EvaluateEmptyHitSpeakGate(emptyESTrace(), "vm-manager-* 上该服务从未参与")
	if got.Allow {
		t.Fatalf("Deny A must fire even with index: %#v", got)
	}
}

func TestEvalGolden_empty_hit_injects(t *testing.T) {
	a := &ReActAgent{}
	tr := emptyESTrace()
	got := a.checkAnswerGates(context.Background(), nil, nil, tr, "该服务从未参与", true, true, nil)
	if !got.Inject || got.Prompt == "" || !strings.Contains(got.Prompt, "vm-manager-*") {
		t.Fatalf("inject %#v", got)
	}
	if got.EmptyHit != true {
		t.Fatalf("EmptyHit flag %#v", got)
	}
	applyAnswerGateInject(tr, got)
	if tr.EmptyHitNudges != 1 {
		t.Fatalf("EmptyHitNudges=%d", tr.EmptyHitNudges)
	}

	got = a.checkAnswerGates(context.Background(), nil, nil, tr, "该服务从未参与", false, false, nil)
	if got.Inject || !got.Incomplete {
		t.Fatalf("forceFinal %#v", got)
	}
	found := false
	for _, e := range tr.Errors {
		if strings.Contains(e, "empty_hit_speak") {
			found = true
		}
	}
	if !found {
		t.Fatalf("trace.Errors=%v", tr.Errors)
	}

	t.Setenv("SATH_EMPTY_HIT_GATE", "0")
	got = a.checkAnswerGates(context.Background(), nil, nil, emptyESTrace(), "该服务从未参与", true, true, nil)
	if got.Inject {
		t.Fatal("env 0 must skip")
	}
}

func TestEvalGolden_close_gate(t *testing.T) {
	cfg := EvidenceGateConfig{Enabled: true}
	got := EvaluateEvidenceGate(cfg, nil, "root cause is OOM in svc-a")
	if got.Allow || got.Action != "inject" {
		t.Fatalf("no refs must inject: %#v", got)
	}

	got = EvaluateEvidenceGate(cfg, []tool.EvidenceRef{{Kind: "es_log_query", Summary: "no hits"}}, "root cause is OOM")
	if !got.Allow {
		t.Fatalf("empty ES ref still closes: %#v", got)
	}

	got = EvaluateEvidenceGate(cfg, []tool.EvidenceRef{{Kind: "jaeger_trace", TraceID: "abc"}}, "ok")
	if !got.Allow {
		t.Fatalf("jaeger ref: %#v", got)
	}

	got = EvaluateEvidenceGate(cfg, []tool.EvidenceRef{{Kind: "rca_grep", Path: "main.go"}}, "found a line")
	if got.Allow || got.Action != "inject" {
		t.Fatalf("grep alone must inject: %#v", got)
	}

	got = EvaluateEvidenceGate(cfg, nil, "本次无法定位，证据不足。")
	if !got.Allow {
		t.Fatalf("exemption: %#v", got)
	}
}
