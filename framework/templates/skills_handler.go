package templates

import (
	"context"
	"fmt"
	"strings"

	"github.com/sixath/framework/config"
	"github.com/sixath/framework/events"
	agent "github.com/sixath/framework/harness"
	"github.com/sixath/framework/memory"
	"github.com/sixath/framework/middleware"
	"github.com/sixath/framework/model"
	"github.com/sixath/framework/skills"
	"github.com/sixath/framework/tool"
	toolskill "github.com/sixath/framework/tool/skillops"
)

// BuildSkillsSummary 转发 skills.BuildSkillsSummary（兼容 templates 测试与旧调用方）。
func BuildSkillsSummary(all []skills.SkillMeta, maxCount int) string {
	return skills.BuildSkillsSummary(all, maxCount)
}

// BuildSkillsAwarePrompt 转发 skills.BuildSkillsAwarePrompt（无 HyperTool 段）。
func BuildSkillsAwarePrompt(skillsIdx *skills.Index) string {
	return skills.BuildSkillsAwarePrompt(skillsIdx)
}

// buildSkillsAwareSystemPrompt 在 skills 索引文案上可选插入 HyperTool 说明。
func buildSkillsAwareSystemPrompt(skillsIdx *skills.Index, hyperToolEnabled bool) string {
	base := skills.BuildSkillsAwarePrompt(skillsIdx)
	if !hyperToolEnabled {
		return base
	}
	extra := "\n" + tool.HyperToolPromptSnippet() + "\n"
	const needle = "排障结束后"
	i := strings.LastIndex(base, needle)
	if i < 0 {
		return base + extra
	}
	return base[:i] + extra + base[i:]
}

// NewSkillsAwareChatHandlerFromConfig 构建一个支持 Skills 的 ReAct 对话 Handler。
// 它使用 ReActAgent + load_skill 工具，并在 System Prompt 中注入 Skills 摘要。
func NewSkillsAwareChatHandlerFromConfig(cfg config.Config, skillsIdx *skills.Index, middlewareByName map[string]middleware.Middleware) (middleware.Handler, error) {
	m, err := model.NewFromIdentifier(cfg.ModelName)
	if err != nil {
		return nil, fmt.Errorf("model: %w", err)
	}
	mem := memory.NewBufferMemory(cfg.MaxHistory)

	mws := make([]middleware.Middleware, 0, len(cfg.Middlewares)+2)
	mws = append(mws, middleware.RecoveryMiddleware, middleware.LoggingMiddleware)
	for _, name := range cfg.Middlewares {
		if mw, ok := middlewareByName[name]; ok {
			mws = append(mws, mw)
		}
	}

	final := func(ctx context.Context, req *agent.Request) (*agent.Response, error) {
		if req == nil {
			return nil, nil
		}

		reg := tool.NewRegistry()
		if bus := events.DefaultBus(); bus != nil {
			reg.SetEventBus(bus)
		}
		_ = registerRCATools(reg, cfg)
		// MCP 不在请求开始时全量注册；仅在模型通过 load_skill 明确使用某 Skill 时，将该 Skill 声明的 MCP 注册到当前上下文。
		var mcpServers []toolskill.McpServerEntry
		for _, srv := range cfg.Skills.MCPServers {
			mcpServers = append(mcpServers, toolskill.McpServerEntry{
				Transport: srv.Transport,
				Endpoint:  srv.Endpoint,
				Id:        srv.ID,
				Backend:   srv.Backend,
				Command:   srv.Command,
				Args:      srv.Args,
				Env:       srv.Env,
			})
		}
		if skillsIdx != nil {
			_ = toolskill.RegisterLoadSkillTool(reg, skillsIdx, mcpServers)
			scriptOpts := &toolskill.ExecuteSkillScriptOptions{
				AllowedExtensions: cfg.Skills.ScriptAllowedExtensions,
				TimeoutSeconds:    cfg.Skills.ScriptTimeoutSeconds,
			}
			_ = toolskill.RegisterExecuteSkillScriptTool(reg, skillsIdx, cfg.Skills.AllowScriptExecution, scriptOpts)
		}
		_ = tool.RegisterHyperTool(reg, &tool.HyperToolOptions{
			Enabled:          cfg.HyperTool.Enabled,
			TimeoutSeconds:   cfg.HyperTool.TimeoutSeconds,
			MaxInternalCalls: cfg.HyperTool.MaxInternalCalls,
			PythonCommand:    cfg.HyperTool.PythonCommand,
			BlockedTools:     cfg.HyperTool.BlockedTools,
		})

		reactOpts := []agent.ReActOption{agent.WithReActMaxSteps(20)}
		if ws := strings.TrimSpace(cfg.Workspace); ws != "" {
			reactOpts = append(reactOpts, agent.WithReActWorkspace(ws))
		}
		if bus := events.DefaultBus(); bus != nil {
			reactOpts = append(reactOpts, agent.WithReActEventBus(bus))
		}
		if tg := agent.ToolGuardrailsFromConfig(cfg.ToolGuardrails); tg != nil {
			cp := *tg
			reactOpts = append(reactOpts, agent.WithReActToolGuardrails(&cp))
		}
		react := agent.NewReActAgent(m, mem, reg, reactOpts...)

		sys := buildSkillsAwareSystemPrompt(skillsIdx, cfg.HyperTool.Enabled)
		llmReq := *req
		llmReq.Messages = append([]model.Message{
			{Role: "system", Content: sys},
		}, req.Messages...)

		if llmReq.Metadata == nil {
			llmReq.Metadata = make(map[string]any)
		}
		if _, ok := llmReq.Metadata[agent.MetaAgentName]; !ok {
			llmReq.Metadata[agent.MetaAgentName] = "chat-skills"
		}
		if llmReq.RequestID != "" {
			ctx = context.WithValue(ctx, tool.ContextKeyRequestID, llmReq.RequestID)
		}

		return react.Run(ctx, &llmReq)
	}

	return middleware.Chain(final, mws...), nil
}
