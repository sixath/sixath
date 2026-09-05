package agent

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sixath/framework/model"
	"github.com/sixath/framework/tool"
)

func TestReplaceOrInsertFirstSystem_ReplacesFirstOnly(t *testing.T) {
	in := []model.Message{
		{Role: "system", Content: "old"},
		{Role: "user", Content: "hi"},
		{Role: "system", Content: "keep"},
	}
	out := replaceOrInsertFirstSystem(in, "new")
	if len(out) != 3 {
		t.Fatalf("len=%d", len(out))
	}
	if out[0].Content != "new" || out[2].Content != "keep" {
		t.Fatalf("got %#v", out)
	}
}

func TestReplaceOrInsertFirstSystem_SkipsProtectedFence(t *testing.T) {
	in := []model.Message{
		{Role: "system", Content: "fence", Metadata: map[string]any{model.MetadataKeySixathOrigin: model.OriginMemoryFence}},
		{Role: "user", Content: "hi"},
	}
	out := replaceOrInsertFirstSystem(in, "agent sys")
	if len(out) != 3 || out[0].Content != "agent sys" || out[1].Content != "fence" {
		t.Fatalf("got %#v", out)
	}
}

func TestReplaceOrInsertFirstSystem_InsertsWhenMissing(t *testing.T) {
	in := []model.Message{{Role: "user", Content: "hi"}}
	out := replaceOrInsertFirstSystem(in, "sys")
	if len(out) != 2 || out[0].Role != "system" || out[0].Content != "sys" || out[1].Content != "hi" {
		t.Fatalf("got %#v", out)
	}
}

func TestPrepareModelMessages_WritesHashAndTools(t *testing.T) {
	fake := &fakeOpenAIClient{finalReply: "ok"}
	reg := tool.NewRegistry()
	if err := reg.Register(tool.Tool{
		Name:        "calc",
		Description: "d",
		Parameters:  map[string]any{},
		Execute:     func(ctx context.Context, params map[string]any) (any, error) { return 0, nil },
	}); err != nil {
		t.Fatal(err)
	}
	a := NewReActAgent(fake, nil, reg, WithReActSystemPrompt("You are a bot."))
	trace := &RunTrace{}
	beginModelInvocation(trace, "plain")
	msgs := a.prepareModelMessages(context.Background(), []model.Message{{Role: "user", Content: "hi"}}, trace)
	if len(msgs) < 2 || msgs[0].Role != "system" {
		t.Fatalf("expected system first: %#v", msgs)
	}
	if !strings.Contains(msgs[0].Content, "## Tools") || !strings.Contains(msgs[0].Content, "- calc") {
		t.Fatalf("missing tools block: %q", msgs[0].Content)
	}
	if !strings.Contains(msgs[0].Content, "You are a bot.") {
		t.Fatalf("missing agent text: %q", msgs[0].Content)
	}
	inv := lastContextOpsInvocation(trace)
	if inv == nil || len(inv.PromptStableHash) != 16 {
		t.Fatalf("hash=%#v", inv)
	}
}

func TestPrepareModelMessages_ReadsMemoryMD(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "MEMORY.md"), []byte("remember X"), 0o644); err != nil {
		t.Fatal(err)
	}
	fake := &fakeOpenAIClient{finalReply: "ok"}
	a := NewReActAgent(fake, nil, nil, WithReActSystemPrompt("sys"), WithReActWorkspace(dir))
	trace := &RunTrace{}
	beginModelInvocation(trace, "plain")
	msgs := a.prepareModelMessages(context.Background(), []model.Message{{Role: "user", Content: "hi"}}, trace)
	if !strings.Contains(msgs[0].Content, "## MEMORY.md") || !strings.Contains(msgs[0].Content, "remember X") {
		t.Fatalf("missing MEMORY.md: %q", msgs[0].Content)
	}
}
