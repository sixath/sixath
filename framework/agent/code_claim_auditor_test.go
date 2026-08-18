package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/sixath/framework/model"
)

type stubClaimModel struct {
	reply string
	err   error
	last  []model.Message
}

func (s *stubClaimModel) Generate(ctx context.Context, prompt string, opts ...model.Option) (*model.Generation, error) {
	return s.Chat(ctx, []model.Message{{Role: "user", Content: prompt}}, opts...)
}

func (s *stubClaimModel) Chat(ctx context.Context, messages []model.Message, opts ...model.Option) (*model.Generation, error) {
	_ = ctx
	_ = opts
	s.last = append([]model.Message(nil), messages...)
	if s.err != nil {
		return nil, s.err
	}
	return &model.Generation{Text: s.reply}, nil
}

func (s *stubClaimModel) Embed(ctx context.Context, texts []string, opts ...model.Option) ([]model.Embedding, error) {
	return nil, nil
}

func TestEvaluateCodeClaimAuditor_failInjects(t *testing.T) {
	stub := &stubClaimModel{reply: `{"verdict":"fail","issues":[{"kind":"dropped_guard","path":"helper.go","symbol":"InsertUnionUserAreaInfo","guard":"errcode == 0","claim":"会写入本地映射"}]}`}
	got := EvaluateCodeClaimAuditor(context.Background(), stub, "区域已有用户时会怎样", "会写入本地映射表", []CodeQuoteSource{{
		Path:    "helper.go",
		Content: helperReadContent,
	}})
	if got.Allow || got.Action != "inject" {
		t.Fatalf("fail verdict must inject, got %#v", got)
	}
	if !strings.Contains(got.Prompt, "InsertUnionUserAreaInfo") || !strings.Contains(got.Prompt, "errcode") {
		t.Fatalf("prompt should cite issue, got %q", got.Prompt)
	}
	if len(stub.last) < 2 {
		t.Fatal("auditor must use a fresh Chat, not empty messages")
	}
	blob := stub.last[0].Content + stub.last[1].Content
	if !strings.Contains(blob, helperReadContent) && !strings.Contains(blob, "InsertUnionUserAreaInfo") {
		t.Fatalf("fresh prompt must include SOURCE, got %q", blob[:min(400, len(blob))])
	}
	if strings.Contains(blob, "思维链") {
		t.Fatal("must not include analysis chain-of-thought labels")
	}
}

func TestEvaluateCodeClaimAuditor_passAllows(t *testing.T) {
	stub := &stubClaimModel{reply: "```json\n{\"verdict\":\"pass\",\"issues\":[]}\n```"}
	got := EvaluateCodeClaimAuditor(context.Background(), stub, "q", "if errcode == 0 { Insert }", []CodeQuoteSource{{Path: "h.go", Content: "1|ok"}})
	if !got.Allow {
		t.Fatalf("pass must allow, got %#v", got)
	}
}

func TestEvaluateCodeClaimAuditor_failOpen(t *testing.T) {
	if got := EvaluateCodeClaimAuditor(context.Background(), nil, "q", "a", []CodeQuoteSource{{Path: "a.go", Content: "1|x"}}); !got.Allow {
		t.Fatalf("nil model fail-open: %#v", got)
	}
	stub := &stubClaimModel{reply: "not-json"}
	if got := EvaluateCodeClaimAuditor(context.Background(), stub, "q", "a", []CodeQuoteSource{{Path: "a.go", Content: "1|x"}}); !got.Allow {
		t.Fatalf("parse fail-open: %#v", got)
	}
}

func TestEvaluateCodeClaimCascade_machineVetoSkipsLLM(t *testing.T) {
	stub := &stubClaimModel{reply: `{"verdict":"pass","issues":[]}`}
	final := "" +
		"```go\n" +
		"info.State = 1\n" +
		"InsertUnionUserAreaInfo(info, flowID)\n" +
		"```\n"
	got := EvaluateCodeClaimCascade(context.Background(), stub, "q", final, []CodeQuoteSource{{
		Path:    "helper.go",
		Content: helperReadContent,
	}})
	if got.Allow {
		t.Fatal("machine veto must not allow reconstructed fence")
	}
	if stub.last != nil {
		t.Fatal("LLM must not run when machine vetoes")
	}
}
