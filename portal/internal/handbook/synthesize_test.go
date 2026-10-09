package handbook

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestSummarizeStage(t *testing.T) {
	m := (&fakeModel{}).on("阶段说明", func(string) string { return `{"summary":"处理下单：校验后写 orders 表。"}` })
	cards := map[string]*Card{"internal/order/store.go": {Purpose: "订单存储", Functions: []CardFunc{{Name: "(*Store).Get"}}}}
	var u usage
	got, err := summarizeStage(context.Background(), m, "svc", Stage{ID: "order", Title: "下单"}, []string{"internal/order/store.go", "internal/order/x.go"}, cards, &u)
	if err != nil || got != "处理下单：校验后写 orders 表。" {
		t.Fatalf("%q %v", got, err)
	}
	if !strings.Contains(m.calls[0], "internal/order/store.go：订单存储；关键函数：(*Store).Get") || !strings.Contains(m.calls[0], "internal/order/x.go：（无卡片）") {
		t.Fatalf("prompt %s", m.calls[0])
	}
}

func TestWriteOverview(t *testing.T) {
	m := (&fakeModel{}).on("仓库总览", func(string) string { return `{"overview":"## 主流程\n下单 → 支付"}` })
	f := &Facts{Module: &GoModule{Path: "example.com/svc"}, Packages: []GoPackage{{Dir: "cmd/server", Main: true}},
		Registers: []RegisterHit{{Kind: RegTable, Name: "orders", Access: AccessWrite, Path: "a.go", Line: 1}}}
	sk := &Skeleton{Stages: []Stage{{ID: "order", Title: "下单", Summary: "写订单"}}}
	var u usage
	got, err := writeOverview(context.Background(), m, "svc", f, sk, &u)
	if err != nil || !strings.Contains(got, "下单 → 支付") {
		t.Fatalf("%q %v", got, err)
	}
	for _, want := range []string{"example.com/svc", "cmd/server", "下单：写订单", "orders"} {
		if !strings.Contains(m.calls[0], want) {
			t.Fatalf("prompt missing %q: %s", want, m.calls[0])
		}
	}
}

func TestRegisterNotes_SelectsAndValidates(t *testing.T) {
	var hits []RegisterHit
	for i := 0; i < 3; i++ {
		hits = append(hits, RegisterHit{Kind: RegTable, Name: "orders", Access: AccessWrite, Path: "internal/order/store.go", Line: 10 + i})
	}
	hits = append(hits, RegisterHit{Kind: RegTopic, Name: "order-events", Access: AccessRef, Path: "internal/mq/topics.go", Line: 3})
	m := (&fakeModel{}).on("用途", func(user string) string {
		b, _ := json.Marshal(map[string]any{"notes": map[string]string{"table:orders": "订单主表", "table:unknown": "x"}})
		return string(b)
	})
	cards := map[string]*Card{"internal/order/store.go": {Purpose: "订单存储"}}
	var u usage
	notes, err := registerNotes(context.Background(), m, "svc", hits, cards, nil, map[string]string{"topic:order-events": "旧说明"}, &u)
	if err != nil {
		t.Fatal(err)
	}
	if notes["table:orders"] != "订单主表" || notes["topic:order-events"] != "旧说明" || len(notes) != 2 {
		t.Fatalf("notes %#v", notes)
	}
	if !strings.Contains(m.calls[0], "table:orders") || strings.Contains(m.calls[0], "topic:order-events") {
		t.Fatalf("only registers without notes are asked when nothing changed: %s", m.calls[0])
	}
	if !strings.Contains(m.calls[0], "写 internal/order/store.go:10") || !strings.Contains(m.calls[0], "订单存储") {
		t.Fatalf("prompt must carry locations and file purposes: %s", m.calls[0])
	}
}

func TestSummarizeStage_TruncatesOversizedFirstLine(t *testing.T) {
	m := (&fakeModel{}).on("阶段说明", func(string) string { return `{"summary":"s"}` })
	cards := map[string]*Card{"a.go": {Purpose: strings.Repeat("长", stageInputBudget)}}
	var u usage
	if _, err := summarizeStage(context.Background(), m, "svc", Stage{ID: "x", Title: "X"}, []string{"a.go", "b.go"}, cards, &u); err != nil {
		t.Fatal(err)
	}
	p := m.calls[0]
	if !strings.Contains(p, "- a.go：长") || !strings.Contains(p, "其余文件省略") || len(p) > stageInputBudget+1024 {
		t.Fatalf("oversized line must be truncated, not dropped (len %d)", len(p))
	}
}

func TestRegisterNotes_TransportErrorPropagates(t *testing.T) {
	boom := errors.New("boom")
	hits := []RegisterHit{{Kind: RegTable, Name: "orders", Access: AccessWrite, Path: "a.go", Line: 1}}
	var u usage
	if _, err := registerNotes(context.Background(), &fakeModel{failErr: boom}, "svc", hits, nil, nil, nil, &u); !errors.Is(err, boom) || errors.Is(err, errBadReply) {
		t.Fatalf("err %v", err)
	}
}

func TestRegisterNotes_RefreshesRegistersInChangedFiles(t *testing.T) {
	hits := []RegisterHit{{Kind: RegTable, Name: "orders", Access: AccessWrite, Path: "a.go", Line: 1}}
	m := (&fakeModel{}).on("用途", func(string) string { return `{"notes":{"table:orders":"新说明"}}` })
	var u usage
	notes, err := registerNotes(context.Background(), m, "svc", hits, nil, map[string]bool{"a.go": true}, map[string]string{"table:orders": "旧"}, &u)
	if err != nil || notes["table:orders"] != "新说明" {
		t.Fatalf("%#v %v", notes, err)
	}
}
