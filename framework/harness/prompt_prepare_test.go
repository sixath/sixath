package harness

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sixath/framework/model"
	"github.com/sixath/framework/skills"
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

type fakeSkillRouter struct {
	meta  skills.SkillMeta
	score float64
	ok    bool
	calls int
}

func (f *fakeSkillRouter) Route(ctx context.Context, query string) (skills.SkillMeta, float64, bool) {
	f.calls++
	return f.meta, f.score, f.ok
}

func TestPrepareModelMessages_InjectsAutoMatchedSkill(t *testing.T) {
	dir := t.TempDir()
	skillFile := filepath.Join(dir, "SKILL.md")
	if err := os.WriteFile(skillFile, []byte("---\nname: demo\n---\nDO THE DEMO THING"), 0o644); err != nil {
		t.Fatal(err)
	}
	r := &fakeSkillRouter{meta: skills.SkillMeta{Name: "demo", Path: skillFile}, score: 0.72, ok: true}
	fake := &fakeOpenAIClient{finalReply: "ok"}
	a := NewReActAgent(fake, nil, nil, WithReActSystemPrompt("sys"), WithReActSkillRouter(r))
	trace := &RunTrace{}
	beginModelInvocation(trace, "plain")
	msgs := a.prepareModelMessages(context.Background(), []model.Message{{Role: "user", Content: "do demo"}}, trace)
	if len(msgs) == 0 || msgs[0].Role != "system" {
		t.Fatalf("expected system first: %#v", msgs)
	}
	if !strings.Contains(msgs[0].Content, "【已自动匹配 Skill：demo】") {
		t.Fatalf("missing auto-match marker: %q", msgs[0].Content)
	}
	if !strings.Contains(msgs[0].Content, "DO THE DEMO THING") {
		t.Fatalf("missing skill body: %q", msgs[0].Content)
	}
	if r.calls != 1 {
		t.Fatalf("router should be called exactly once per prepare, got %d", r.calls)
	}
}

func TestPrepareModelMessages_NoRouterNoInjection(t *testing.T) {
	fake := &fakeOpenAIClient{finalReply: "ok"}
	a := NewReActAgent(fake, nil, nil, WithReActSystemPrompt("sys"))
	trace := &RunTrace{}
	beginModelInvocation(trace, "plain")
	msgs := a.prepareModelMessages(context.Background(), []model.Message{{Role: "user", Content: "hi"}}, trace)
	if strings.Contains(msgs[0].Content, "已自动匹配") {
		t.Fatalf("no router must not inject: %q", msgs[0].Content)
	}
}

func TestPrepareModelMessages_RouterMissNoInjection(t *testing.T) {
	r := &fakeSkillRouter{ok: false}
	fake := &fakeOpenAIClient{finalReply: "ok"}
	a := NewReActAgent(fake, nil, nil, WithReActSystemPrompt("sys"), WithReActSkillRouter(r))
	trace := &RunTrace{}
	beginModelInvocation(trace, "plain")
	msgs := a.prepareModelMessages(context.Background(), []model.Message{{Role: "user", Content: "hi"}}, trace)
	if strings.Contains(msgs[0].Content, "已自动匹配") {
		t.Fatalf("router miss must not inject: %q", msgs[0].Content)
	}
	if r.calls != 1 {
		t.Fatalf("router should still be consulted once, got %d", r.calls)
	}
}

func TestPrepareModelMessages_RouterHitButFileMissing(t *testing.T) {
	r := &fakeSkillRouter{
		meta:  skills.SkillMeta{Name: "ghost", Path: filepath.Join(t.TempDir(), "nonexistent", "SKILL.md")},
		score: 0.9,
		ok:    true,
	}
	fake := &fakeOpenAIClient{finalReply: "ok"}
	a := NewReActAgent(fake, nil, nil, WithReActSystemPrompt("sys"), WithReActSkillRouter(r))
	trace := &RunTrace{}
	beginModelInvocation(trace, "plain")
	msgs := a.prepareModelMessages(context.Background(), []model.Message{{Role: "user", Content: "hi"}}, trace)
	if strings.Contains(msgs[0].Content, "已自动匹配") {
		t.Fatalf("unreadable skill body must not inject: %q", msgs[0].Content)
	}
}
