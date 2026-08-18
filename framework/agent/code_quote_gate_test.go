package agent

import (
	"strings"
	"testing"
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
}
