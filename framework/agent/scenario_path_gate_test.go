package agent

import (
	"strings"
	"testing"

	"github.com/sixath/framework/tool"
)

func TestEvaluateScenarioPathGate_1105WriteWithoutCallName(t *testing.T) {
	src := helperSource()
	got := EvaluateScenarioPathGate("区域已有用户（errcode=1105）会不会写本地映射？",
		"区域已有用户时会把 UID 写入本地映射表。", src)
	if got.Allow {
		t.Fatalf("1105 write prose must fail even without Insert name, got %#v", got)
	}
	if got.Action != "inject" || !strings.Contains(got.Prompt, "InsertUnionUserAreaInfo") {
		t.Fatalf("prompt should name unreachable write, got %#v", got)
	}
}

func TestEvaluateScenarioPathGate_existingUserAlias(t *testing.T) {
	src := helperSource()
	got := EvaluateScenarioPathGate("区域已有用户时会怎样", "union 会写入本地映射表。", src)
	if got.Allow {
		t.Fatalf("已有用户 alias must pin !=0 path, got %#v", got)
	}
}

func TestEvaluateScenarioPathGate_skipOrOtherWhenAllows(t *testing.T) {
	src := helperSource()
	if got := EvaluateScenarioPathGate("1105 会不会写", "errcode=1105 时不写入映射表。", src); !got.Allow {
		t.Fatalf("skip wording must allow: %#v", got)
	}
	if got := EvaluateScenarioPathGate("1105 会不会写", "只有 errcode == 0 才会 InsertUnionUserAreaInfo。", src); !got.Allow {
		t.Fatalf("other-path when must allow: %#v", got)
	}
}

func TestEvaluateScenarioPathGate_noScenarioAllows(t *testing.T) {
	src := helperSource()
	got := EvaluateScenarioPathGate("这段函数做什么", "会把 UID 写入本地映射表。", src)
	if !got.Allow {
		t.Fatalf("no error-code in the question must fail-open, got %#v", got)
	}
}

func TestEvaluateScenarioPathGate_zeroPathAllowsWrite(t *testing.T) {
	src := helperSource()
	got := EvaluateScenarioPathGate("errcode 为 0 时会不会写映射", "会写入本地映射表。", src)
	if !got.Allow {
		t.Fatalf("success path (0) may claim the write, got %#v", got)
	}
}

func TestEvaluateScenarioPathGate_bothCodesNoPin(t *testing.T) {
	src := helperSource()
	got := EvaluateScenarioPathGate("对比 errcode 0 和 1105 分别怎样", "会写入本地映射表。", src)
	if !got.Allow {
		t.Fatalf("mentioning both 0 and 1105 must not pin a single path, got %#v", got)
	}
}

func TestEvaluateScenarioPathGate_switch1105(t *testing.T) {
	cf := tool.ExtractControlFlow([]byte(`package p
func F() {
	switch errcode {
	case 0:
		InsertUnionUserAreaInfo()
	case 1105:
		reuseExistingUID()
	}
}
`), "h.go", 1, 30)
	src := []CodeQuoteSource{{Path: "h.go", Content: "1|switch", ControlFlow: cf}}
	got := EvaluateScenarioPathGate("1105 已有用户会不会写库", "会写入映射表。", src)
	if got.Allow {
		t.Fatalf("1105 case must not allow Insert write claim, got %#v", got)
	}
}

func TestEvaluateCodeClaimCascade_scenarioVetoSkipsLLM(t *testing.T) {
	stub := &stubClaimModel{reply: `{"verdict":"pass","issues":[]}`}
	got := EvaluateCodeClaimCascade(nil, stub, "区域已有用户 1105 会不会写映射",
		"区域已有用户时会把 UID 写入本地映射表。", helperSource())
	if got.Allow {
		t.Fatal("scenario gate must veto before LLM")
	}
	if stub.last != nil {
		t.Fatal("LLM auditor must not run after scenario veto")
	}
}
