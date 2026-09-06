package templates

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	agent "github.com/sixath/framework/harness"
	"github.com/sixath/framework/memory"
	"github.com/sixath/framework/model"
	"github.com/sixath/framework/tool"
)

type fakeMCPModel struct {
	lastMessages []model.Message
}

func (f *fakeMCPModel) Generate(ctx context.Context, prompt string, opts ...model.Option) (*model.Generation, error) {
	return &model.Generation{Text: ""}, nil
}

func (f *fakeMCPModel) Chat(ctx context.Context, messages []model.Message, opts ...model.Option) (*model.Generation, error) {
	return &model.Generation{Text: "ok"}, nil
}

func (f *fakeMCPModel) Embed(ctx context.Context, texts []string, opts ...model.Option) ([]model.Embedding, error) {
	return nil, nil
}

func (f *fakeMCPModel) ChatWithTools(ctx context.Context, messages []model.Message, reg *tool.Registry, opts ...model.Option) (*model.Generation, error) {
	f.lastMessages = append([]model.Message(nil), messages...)
	return &model.Generation{
		Text: "tool step",
		Raw:  model.ToolStep{Used: false},
	}, nil
}

func TestNewMCPAgentHandler_InjectsWorkspaceMemoryMD(t *testing.T) {
	dir := t.TempDir()
	const token = "s8-mcp-mem-token"
	if err := os.WriteFile(filepath.Join(dir, "MEMORY.md"), []byte(token), 0o644); err != nil {
		t.Fatal(err)
	}
	m := &fakeMCPModel{}
	h := NewMCPAgentHandler(m, memory.NewBufferMemory(5), Config{
		Endpoint:      "http://127.0.0.1:1/mcp",
		ID:            "s8-mcp",
		MaxReActSteps: 2,
		Workspace:     dir,
	})
	if _, err := h(context.Background(), &agent.Request{
		Messages: []model.Message{{Role: "user", Content: "ping"}},
	}); err != nil {
		t.Fatalf("handler error: %v", err)
	}
	sys := firstSystemContent(m.lastMessages)
	if !strings.Contains(sys, "## MEMORY.md") || !strings.Contains(sys, token) {
		t.Fatalf("expected workspace MEMORY.md in system prompt, got:\n%s", sys)
	}
}

func TestNewMCPAgentHandler_BlankWorkspaceSkipsMemoryMD(t *testing.T) {
	dir := t.TempDir()
	const token = "s8-mcp-blank-should-not-appear"
	if err := os.WriteFile(filepath.Join(dir, "MEMORY.md"), []byte(token), 0o644); err != nil {
		t.Fatal(err)
	}
	m := &fakeMCPModel{}
	h := NewMCPAgentHandler(m, memory.NewBufferMemory(5), Config{
		Endpoint:      "http://127.0.0.1:1/mcp",
		ID:            "s8-mcp-blank",
		MaxReActSteps: 2,
		Workspace:     "   ",
	})
	if _, err := h(context.Background(), &agent.Request{
		Messages: []model.Message{{Role: "user", Content: "ping"}},
	}); err != nil {
		t.Fatalf("handler error: %v", err)
	}
	sys := firstSystemContent(m.lastMessages)
	if strings.Contains(sys, token) {
		t.Fatalf("blank workspace must not load MEMORY.md, got:\n%s", sys)
	}
}
