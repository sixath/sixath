package investigate

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/sixath/framework/model"
	"github.com/sixath/framework/tool"
)

func stubTool(name string) tool.Tool {
	return tool.Tool{
		Name:       name,
		Parameters: map[string]any{"type": "object", "properties": map[string]any{}},
		Execute:    func(ctx context.Context, params map[string]any) (any, error) { return map[string]any{"ok": true}, nil },
	}
}

func TestGate_RequiresBothGroups(t *testing.T) {
	cases := []struct {
		name    string
		tools   []string
		visible bool
	}{
		{"none", nil, false},
		{"code only", []string{"rca_grep"}, false},
		{"log only", []string{"es_log_query"}, false},
		{"both", []string{"rca_grep", "es_log_query"}, true},
		{"both via other names", []string{"rca_read", "vm_run_cmd"}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			reg := tool.NewEmptyRegistry()
			for _, n := range c.tools {
				if err := reg.Register(stubTool(n)); err != nil {
					t.Fatal(err)
				}
			}
			g := gate(reg)
			err := g(context.Background())
			if c.visible && err != nil {
				t.Fatalf("want visible, got err=%v", err)
			}
			if !c.visible && err == nil {
				t.Fatal("want hidden, got nil err")
			}
		})
	}
}

func TestGate_RespectsToolCheckFn(t *testing.T) {
	reg := tool.NewEmptyRegistry()
	gated := stubTool("es_log_query")
	gated.CheckFn = func(ctx context.Context) error { return errors.New("gated off") }
	_ = reg.Register(stubTool("rca_grep"))
	_ = reg.Register(gated)
	if err := gate(reg)(context.Background()); err == nil {
		t.Fatal("tool whose CheckFn fails must not satisfy the gate")
	}
}

func TestGate_RespectsEnabledToolsets(t *testing.T) {
	reg := tool.NewEmptyRegistry()
	rca := stubTool("rca_grep")
	rca.Toolset = tool.ToolsetRCA
	es := stubTool("es_log_query")
	es.Toolset = tool.ToolsetRCA
	_ = reg.Register(rca)
	_ = reg.Register(es)
	// 白名单只放行 core → rca 工具被过滤 → 门控失败
	ctx := context.WithValue(context.Background(), tool.ContextKeyEnabledToolsets, []string{tool.ToolsetCore})
	if err := gate(reg)(ctx); err == nil {
		t.Fatal("tools filtered by enabled_toolsets must not satisfy the gate")
	}
	// 白名单含 rca → 通过
	ctx = context.WithValue(context.Background(), tool.ContextKeyEnabledToolsets, []string{tool.ToolsetRCA})
	if err := gate(reg)(ctx); err != nil {
		t.Fatalf("want pass, got %v", err)
	}
}

func TestRegister_ToolAppearsOnlyWhenGated(t *testing.T) {
	m := &fakeToolModel{finalText: "结论\n状态: 证据充分"}
	reg := tool.NewEmptyRegistry()
	if err := Register(reg, Config{Model: m}); err != nil {
		t.Fatal(err)
	}
	// 无底层工具 → 不可见
	if n := len(reg.ListForAPI(context.Background(), nil)); n != 0 {
		t.Fatalf("gate should hide tool, got %d tools in schema", n)
	}
	// 补齐两组 → 可见
	_ = reg.Register(stubTool("rca_grep"))
	_ = reg.Register(stubTool("es_log_query"))
	api := reg.ListForAPI(context.Background(), nil)
	if len(api) != 3 {
		t.Fatalf("want 3 tools visible, got %d", len(api))
	}
}

func TestRegister_RejectsNilModel(t *testing.T) {
	if err := Register(tool.NewEmptyRegistry(), Config{}); err == nil {
		t.Fatal("nil model must be rejected")
	}
}

func TestRegister_RejectsNonToolCallingModel(t *testing.T) {
	if err := Register(tool.NewEmptyRegistry(), Config{Model: &plainModel{}}); err == nil {
		t.Fatal("model without ChatWithTools must be rejected")
	}
}

// plainModel 只实现 model.Model，不支持 ChatWithTools。
type plainModel struct{}

func (plainModel) Generate(ctx context.Context, prompt string, opts ...model.Option) (*model.Generation, error) {
	return nil, errors.New("not implemented")
}
func (plainModel) Chat(ctx context.Context, msgs []model.Message, opts ...model.Option) (*model.Generation, error) {
	return &model.Generation{Text: "plain"}, nil
}
func (plainModel) Embed(ctx context.Context, texts []string, opts ...model.Option) ([]model.Embedding, error) {
	return nil, nil
}

// fakeToolModel 实现 harness.ToolCallingModel：第一次调用发起 rca_grep 工具调用，
// 之后返回最终文本。lastReg 记录模型实际收到的 registry（用于验证子 registry 过滤）。
type fakeToolModel struct {
	calls     int
	finalText string
	lastReg   *tool.Registry
}

func (f *fakeToolModel) Generate(ctx context.Context, prompt string, opts ...model.Option) (*model.Generation, error) {
	return nil, errors.New("not implemented")
}
func (f *fakeToolModel) Chat(ctx context.Context, msgs []model.Message, opts ...model.Option) (*model.Generation, error) {
	return &model.Generation{Text: f.finalText}, nil
}
func (f *fakeToolModel) Embed(ctx context.Context, texts []string, opts ...model.Option) ([]model.Embedding, error) {
	return nil, nil
}
func (f *fakeToolModel) ChatWithTools(ctx context.Context, msgs []model.Message, reg *tool.Registry, opts ...model.Option) (*model.Generation, error) {
	f.lastReg = reg
	f.calls++
	if f.calls == 1 {
		return &model.Generation{Raw: model.ToolStep{
			Used: true,
			ToolCalls: []model.ToolCall{{
				ID:        "call-1",
				Name:      "rca_grep",
				Arguments: map[string]any{"pattern": "x"},
			}},
		}}, nil
	}
	return &model.Generation{Text: f.finalText, Raw: model.ToolStep{Used: false}}, nil
}

func TestRegister_RejectsNilRegistry(t *testing.T) {
	if err := Register(nil, Config{Model: &fakeToolModel{}}); err == nil {
		t.Fatal("nil registry must be rejected")
	}
}

func TestConfig_Defaults(t *testing.T) {
	c := Config{}
	if c.maxSteps() != DefaultMaxSteps || c.timeout() != 15*time.Minute || c.maxOutputTokens() != 4096 {
		t.Fatalf("bad defaults: steps=%d timeout=%v maxOut=%d", c.maxSteps(), c.timeout(), c.maxOutputTokens())
	}
	custom := Config{MaxSteps: 5, Timeout: 123 * time.Second, MaxOutputTokens: 2048}
	if custom.maxSteps() != 5 || custom.maxOutputTokens() != 2048 {
		t.Fatal("custom values must win")
	}
}
