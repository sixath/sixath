package harness

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/sixath/framework/events"
	"github.com/sixath/framework/model"
)

func TestStableArgsKey_DifferentKeyOrderSameHash(t *testing.T) {
	a := map[string]any{"z": float64(1), "a": "x", "m": map[string]any{"b": float64(2), "a": float64(1)}}
	b := map[string]any{"a": "x", "m": map[string]any{"a": float64(1), "b": float64(2)}, "z": float64(1)}
	if gotA, gotB := StableArgsKey(a), StableArgsKey(b); gotA != gotB {
		t.Fatalf("expected same stable key, got %q vs %q", gotA, gotB)
	}
}

func TestStableArgsKey_OmittedNullEqualsAbsent(t *testing.T) {
	withNull := map[string]any{"host": "h", "port": nil, "cmd": "ls"}
	without := map[string]any{"host": "h", "cmd": "ls"}
	if StableArgsKey(withNull) != StableArgsKey(without) {
		t.Fatal("null field should be stripped so keys match absent key")
	}
}

func TestStableArgsKey_WholeNumberFloatMatchesIntJSON(t *testing.T) {
	dec := json.NewDecoder(bytes.NewReader([]byte(`{"n":1,"x":2.0}`)))
	dec.UseNumber()
	var m1 map[string]any
	if err := dec.Decode(&m1); err != nil {
		t.Fatal(err)
	}
	m2 := map[string]any{"n": float64(1), "x": float64(2)}
	if StableArgsKey(m1) != StableArgsKey(m2) {
		t.Fatal("json.Number whole values should match float64 counterparts")
	}
}

func TestStableArgsKey_NilMapEqualsEmpty(t *testing.T) {
	if StableArgsKey(nil) != StableArgsKey(map[string]any{}) {
		t.Fatal("nil args should canonicalize like empty object")
	}
}

func TestStableArgsKey_ArrayDropsNullElements(t *testing.T) {
	a := map[string]any{"items": []any{float64(1), nil, "x"}}
	b := map[string]any{"items": []any{float64(1), "x"}}
	if StableArgsKey(a) != StableArgsKey(b) {
		t.Fatal("null array elements should be removed for stable comparison")
	}
}

func TestCanonicalJSON_IsDeterministicSortedKeys(t *testing.T) {
	m := map[string]any{"b": float64(1), "a": float64(2)}
	b1, err := CanonicalJSON(m)
	if err != nil {
		t.Fatal(err)
	}
	b2, err := CanonicalJSON(m)
	if err != nil {
		t.Fatal(err)
	}
	if string(b1) != string(b2) {
		t.Fatalf("canonical bytes differ: %s vs %s", b1, b2)
	}
	// Lexicographic key order: "a" before "b"
	if string(b1) != `{"a":2,"b":1}` {
		t.Fatalf("unexpected canonical form: %s", b1)
	}
}

func TestTailR1FailureStreak_resetsOnDifferentArgs(t *testing.T) {
	h := []ToolCallRecord{
		{ToolName: "t", Error: "e", Arguments: map[string]any{"a": float64(1)}},
		{ToolName: "t", Error: "e", Arguments: map[string]any{"a": float64(2)}},
	}
	if got := tailR1FailureStreak(h); got != 1 {
		t.Fatalf("tail r1 streak want 1, got %d", got)
	}
}

func TestTailR2FailureStreak_countsSameToolAnyArgs(t *testing.T) {
	h := []ToolCallRecord{
		{ToolName: "t", Error: "e1", Arguments: map[string]any{"a": float64(1)}},
		{ToolName: "t", Error: "e2", Arguments: map[string]any{"b": float64(2)}},
	}
	if got := tailR2FailureStreak(h); got != 2 {
		t.Fatalf("tail r2 streak want 2, got %d", got)
	}
}

func TestApplyToolGuardrails_plainAfterToolsContinuity_tailScanNoReset(t *testing.T) {
	// 设计 §6.2：护栏按 trace 尾部扫描，无 plain_after_tools 间「隐式清零」状态。
	cfg := &ToolGuardrailsConfig{Enabled: true, HardHalt: false, SameArgsFailureWarn: 2, SameToolFailureWarn: 100}
	h := []ToolCallRecord{
		{Step: 0, ToolName: "ssh_exec", Error: "host is required", Arguments: map[string]any{"cmd": "ls"}},
		{Step: 0, ToolName: "ssh_exec", Error: "host is required", Arguments: map[string]any{"cmd": "ls"}},
	}
	var n int
	emitCount := func(k events.Kind, _ map[string]any) {
		if k == events.ToolGuardrailWarn {
			n++
		}
	}
	if halt := applyToolGuardrails(cfg, h, emitCount, 0); halt {
		t.Fatal("did not expect halt on warn-only")
	}
	if n != 1 {
		t.Fatalf("first eval want 1 warn, got %d", n)
	}
	n = 0
	if halt := applyToolGuardrails(cfg, h, emitCount, 0); halt {
		t.Fatal("did not expect halt on second eval")
	}
	if n != 1 {
		t.Fatalf("second eval want 1 warn (tail unchanged), got %d", n)
	}
}

func TestGuardrailHaltSystemMessage_hasSixathOrigin(t *testing.T) {
	m := GuardrailHaltSystemMessage()
	if m.Role != "system" || m.Metadata[model.MetadataKeySixathOrigin] != model.OriginGuardrailHalt {
		t.Fatalf("unexpected halt message: %#v", m)
	}
}

func TestApplyToolGuardrails_idempotentRelaxesR1Threshold(t *testing.T) {
	cfg := &ToolGuardrailsConfig{
		Enabled: true, HardHalt: false,
		SameArgsFailureWarn: 2,
		SameToolFailureWarn: 100,
	}
	h3 := []ToolCallRecord{
		{ToolName: "memory_search", Error: "e", Arguments: map[string]any{"q": "a"}},
		{ToolName: "memory_search", Error: "e", Arguments: map[string]any{"q": "a"}},
		{ToolName: "memory_search", Error: "e", Arguments: map[string]any{"q": "a"}},
	}
	var n int
	emit := func(k events.Kind, _ map[string]any) {
		if k == events.ToolGuardrailWarn {
			n++
		}
	}
	if applyToolGuardrails(cfg, h3, emit, 0) {
		t.Fatal("unexpected halt")
	}
	if n != 0 {
		t.Fatalf("streak 3 below idempotent R1 warn (2*2), want 0 warns got %d", n)
	}
	h4 := append(append([]ToolCallRecord(nil), h3...), ToolCallRecord{ToolName: "memory_search", Error: "e", Arguments: map[string]any{"q": "a"}})
	n = 0
	if applyToolGuardrails(cfg, h4, emit, 0) {
		t.Fatal("unexpected halt")
	}
	if n != 1 {
		t.Fatalf("streak 4 should emit one R1 warn, got %d", n)
	}
}

func TestApplyToolGuardrails_hardHalt(t *testing.T) {
	cfg := &ToolGuardrailsConfig{
		Enabled: true, HardHalt: true,
		SameArgsFailureWarn: 2, SameArgsFailureHalt: 2,
		SameToolFailureWarn: 100, SameToolFailureHalt: 0,
	}
	h := []ToolCallRecord{
		{Step: 0, ToolName: "t", Error: "e", Arguments: map[string]any{"x": float64(1)}},
		{Step: 0, ToolName: "t", Error: "e", Arguments: map[string]any{"x": float64(1)}},
	}
	if !applyToolGuardrails(cfg, h, func(events.Kind, map[string]any) {}, 0) {
		t.Fatal("expected halt")
	}
}

func TestApplyToolGuardrails_noProgressWarnAndHalt(t *testing.T) {
	cfg := &ToolGuardrailsConfig{
		Enabled: true, HardHalt: true,
		NoProgressToolOnlyWarn: 2, NoProgressToolOnlyHalt: 3,
		SameArgsFailureWarn: 100, SameToolFailureWarn: 100,
	}
	var kinds []events.Kind
	emit := func(k events.Kind, _ map[string]any) { kinds = append(kinds, k) }
	if applyToolGuardrails(cfg, nil, emit, 1) {
		t.Fatal("streak 1 below warn")
	}
	kinds = nil
	if applyToolGuardrails(cfg, nil, emit, 2) {
		t.Fatal("streak 2 warn only")
	}
	if len(kinds) != 1 || kinds[0] != events.ToolGuardrailWarn {
		t.Fatalf("want one warn, got %#v", kinds)
	}
	if !applyToolGuardrails(cfg, nil, func(events.Kind, map[string]any) {}, 3) {
		t.Fatal("expected R3 halt at streak 3")
	}
}
