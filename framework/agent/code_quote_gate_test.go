package agent

import (
	"strings"
	"testing"

	"github.com/sixath/framework/tool"
)

const helperReadContent = "" +
	"966|\t// 注册区域用户\n" +
	"967|\tresult, errcode, err := RequestRegisterAreaUser(address, port, flowID, thirdinfo.ThirdID, basicinfo.Name, thirdinfo.ThirdType, thirdinfo.BizType, basicinfo.UseType, areatype)\n" +
	"968|\tif err != nil {\n" +
	"969|\t\tvlog.Errorf(\"request register area user failed\")\n" +
	"970|\t\treturn info, errcode, errors.New(\"request register area user failed\")\n" +
	"971|\t}\n" +
	"974|\tinfo = new(dbwrap.DBUnionUserAreaInfo)\n" +
	"977|\tinfo.UID, _ = strconv.ParseUint(result.UserID, 10, 64)\n" +
	"978|\tinfo.State = 1\n" +
	"984|\tif errcode == 0 {\n" +
	"985|\t\terrcode, err = InsertUnionUserAreaInfo(info, flowID)\n" +
	"986|\t}\n" +
	"987|\treturn\n"

func c304ControlFlow() []tool.ControlFlowFunc {
	return tool.ExtractControlFlow([]byte(`package union
func RegisterUnionUserToArea() {
	result, errcode, err := RequestRegisterAreaUser()
	if err != nil {
		vlog.Errorf("failed")
		return
	}
	info := new(DBUnionUserAreaInfo)
	info.UID, _ = strconv.ParseUint(result.UserID, 10, 64)
	info.State = 1
	if errcode == 0 {
		errcode, err = InsertUnionUserAreaInfo(info)
	}
}
`), "helper.go", 1, 40)
}

func helperSource() []CodeQuoteSource {
	return []CodeQuoteSource{{
		Path:        "helper.go",
		Content:     helperReadContent,
		ControlFlow: c304ControlFlow(),
	}}
}

func TestEvaluateCodeQuoteGate_reconstructedSnippetDropsIf(t *testing.T) {
	final := "" +
		"正常写入本地映射\n\n" +
		"`helper.go:967-986`：\n" +
		"```go\n" +
		"result, errcode, err := RequestRegisterAreaUser(...)\n" +
		"if err != nil {\n" +
		"    ...\n" +
		"}\n" +
		"info.UID, _ = strconv.ParseUint(result.UserID, 10, 64)\n" +
		"info.State = 1\n" +
		"InsertUnionUserAreaInfo(info, flowID)\n" +
		"```\n"
	got := EvaluateCodeQuoteGate([]CodeQuoteSource{{
		Path:    "union-access/handler/helper.go",
		Content: helperReadContent,
	}}, final)
	if got.Allow {
		t.Fatalf("reconstructed snippet that drops if errcode == 0 must not allow, got %#v", got)
	}
	if got.Action != "inject" {
		t.Fatalf("action=%q want inject", got.Action)
	}
	if !strings.Contains(got.Prompt, "errcode") && !strings.Contains(got.Prompt, "伪源码") && !strings.Contains(got.Prompt, "verbatim") {
		t.Fatalf("inject prompt should name the mismatch, got %q", got.Prompt)
	}
}

func TestEvaluateCodeQuoteGate_verbatimIfPasses(t *testing.T) {
	final := "" +
		"```go\n" +
		"if errcode == 0 {\n" +
		"    errcode, err = InsertUnionUserAreaInfo(info, flowID)\n" +
		"}\n" +
		"```\n"
	got := EvaluateCodeQuoteGate([]CodeQuoteSource{{
		Path:    "helper.go",
		Content: helperReadContent,
	}}, final)
	if !got.Allow {
		t.Fatalf("verbatim if-guard quote must allow, got %#v", got)
	}
}

func TestEvaluateCodeQuoteGate_singleLineGuardedCallFails(t *testing.T) {
	final := "会写库：\n```go\nInsertUnionUserAreaInfo(info, flowID)\n```\n"
	got := EvaluateCodeQuoteGate([]CodeQuoteSource{{
		Path:    "helper.go",
		Content: helperReadContent,
	}}, final)
	if got.Allow {
		t.Fatalf("quoting a call inside if errcode==0 without the if must fail, got %#v", got)
	}
}

func TestEvaluateCodeQuoteGate_noFenceOrNoReadAllows(t *testing.T) {
	if got := EvaluateCodeQuoteGate(nil, "会写入映射表"); !got.Allow {
		t.Fatalf("no rca_read: %#v", got)
	}
	if got := EvaluateCodeQuoteGate([]CodeQuoteSource{{Path: "a.go", Content: "1|foo"}}, "plain prose"); !got.Allow {
		t.Fatalf("no fence: %#v", got)
	}
	if got := EvaluateCodeQuoteGate(helperSource(), "区域已有用户时会把 UID 写入本地映射表。"); !got.Allow {
		t.Fatalf("write prose without gated call name is not a machine CFG miss, got %#v", got)
	}
}

func TestEvaluateCodeQuoteGate_proseClaimsGuardedCallFails(t *testing.T) {
	final := "区域返回 1105 后，union 把 1105 当成功，调用 InsertUnionUserAreaInfo 回填 t_union_user_area_info 映射表。"
	got := EvaluateCodeQuoteGate(helperSource(), final)
	if got.Allow {
		t.Fatalf("prose claiming gated Insert without errcode==0 must fail, got %#v", got)
	}
	if !strings.Contains(got.Prompt, "InsertUnionUserAreaInfo") || !strings.Contains(got.Prompt, "control_flow") {
		t.Fatalf("prompt should cite CFG, got %q", got.Prompt)
	}
}

func TestEvaluateCodeQuoteGate_proseSkipOrGuardAllows(t *testing.T) {
	src := helperSource()
	if got := EvaluateCodeQuoteGate(src, "errcode=1105 时 InsertUnionUserAreaInfo 被跳过，不写入映射。"); !got.Allow {
		t.Fatalf("skip wording must allow, got %#v", got)
	}
	if got := EvaluateCodeQuoteGate(src, "只有 if errcode == 0 才会 InsertUnionUserAreaInfo。"); !got.Allow {
		t.Fatalf("explicit guard must allow, got %#v", got)
	}
}

func TestEvaluateCodeQuoteGate_switchCaseProse(t *testing.T) {
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
	if got := EvaluateCodeQuoteGate(src, "区域已有用户时会调用 InsertUnionUserAreaInfo 回填映射。"); got.Allow {
		t.Fatalf("switch-gated Insert without case/errcode==0 must fail, got %#v", got)
	}
	if got := EvaluateCodeQuoteGate(src, "只有 errcode==0 的 case 才会 InsertUnionUserAreaInfo。"); !got.Allow {
		t.Fatalf("errcode==0 must allow, got %#v", got)
	}
}

func TestEvaluateCodeQuoteGate_forRangeProse(t *testing.T) {
	cf := tool.ExtractControlFlow([]byte(`package p
func F() {
	for _, item := range items {
		SyncUnionUser(item)
	}
}
`), "h.go", 1, 20)
	src := []CodeQuoteSource{{Path: "h.go", Content: "1|for", ControlFlow: cf}}
	if got := EvaluateCodeQuoteGate(src, "会调用 SyncUnionUser 写库。"); got.Allow {
		t.Fatalf("range-gated call without loop wording must fail, got %#v", got)
	}
	if got := EvaluateCodeQuoteGate(src, "遍历 items 时调用 SyncUnionUser。"); !got.Allow {
		t.Fatalf("range when must allow, got %#v", got)
	}
}

func TestCollectCodeQuoteSources_controlFlow(t *testing.T) {
	cf := c304ControlFlow()
	got := CollectCodeQuoteSources([]ToolCallRecord{
		{ToolName: "rca_read", Result: map[string]any{
			"file": "helper.go", "content": "1|ok", "control_flow": cf,
		}},
	})
	if len(got) != 1 || len(got[0].ControlFlow) == 0 {
		t.Fatalf("want control_flow attached, got %#v", got)
	}
}
