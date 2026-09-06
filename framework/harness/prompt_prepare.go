package harness

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	fwctx "github.com/sixath/framework/context"
	"github.com/sixath/framework/model"
	"github.com/sixath/framework/skills"
)

func (a *ReActAgent) prepareModelMessages(ctx context.Context, messages []model.Message, trace *RunTrace) []model.Message {
	in := fwctx.Input{
		AgentSystem: a.config.SystemPrompt,
		SkillsIndex: a.skillsIndexText(),
		MemoryMD:    readWorkspaceMarkdown(a.config.Workspace, "MEMORY.md"),
		UserMD:      readWorkspaceMarkdown(a.config.Workspace, "USER.md"),
		ToolNames:   a.registryToolNames(),
		Ephemeral:   ephemeralFor(trace),
	}
	built := fwctx.Build(in)
	encoded := fwctx.Encode(built.Stable, built.Ephemeral)
	if inv := lastContextOpsInvocation(trace); inv != nil {
		inv.PromptStableHash = built.StableHash
	}
	out := replaceOrInsertFirstSystem(messages, encoded)
	return fwctx.PrepareCtx(ctx, out, a.pipelineConfig(trace))
}

func (a *ReActAgent) pipelineConfig(trace *RunTrace) *fwctx.PipelineConfig {
	cfg := &fwctx.PipelineConfig{
		MaxContextRunes:      a.config.MaxContextRunes,
		MaxContextTokensSoft: a.config.MaxContextTokensSoft,
		TokenEstimateAlpha:   a.config.TokenEstimateAlpha,
		SnipCompactEnabled:   a.config.SnipCompactEnabled,
		L2:                   a.config.L2Runtime,
	}
	if trace != nil {
		cfg.Trace = contextTraceMerge(trace)
	}
	return cfg
}

func (a *ReActAgent) registryToolNames() []string {
	if a.tools == nil {
		return nil
	}
	list := a.tools.List()
	names := make([]string, 0, len(list))
	for _, t := range list {
		if strings.TrimSpace(t.Name) == "" {
			continue
		}
		names = append(names, t.Name)
	}
	return names
}

func (a *ReActAgent) skillsIndexText() string {
	dirs := skillScanDirs(a.config.Workspace, a.config.SkillsDirs)
	if len(dirs) == 0 {
		return ""
	}
	idx, err := skills.NewIndex(dirs, nil, nil)
	if err != nil || idx == nil || len(idx.All()) == 0 {
		return ""
	}
	return skills.BuildSkillsAwarePrompt(idx)
}

func skillScanDirs(workspace string, extra []string) []string {
	dirs := make([]string, 0, len(extra)+1)
	if workspace != "" {
		skillsDir := filepath.Join(workspace, "skills")
		if st, err := os.Stat(skillsDir); err == nil && st.IsDir() {
			dirs = append(dirs, skillsDir)
		}
	}
	for _, dir := range extra {
		if dir == "" {
			continue
		}
		if st, err := os.Stat(dir); err == nil && st.IsDir() {
			dirs = append(dirs, dir)
		}
	}
	return dirs
}

func readWorkspaceMarkdown(workspace, name string) string {
	if strings.TrimSpace(workspace) == "" {
		return ""
	}
	b, err := os.ReadFile(filepath.Join(workspace, name))
	if err != nil {
		return ""
	}
	return string(b)
}

func (a *ChatAgent) withWorkspacePrompt(messages []model.Message) []model.Message {
	if a == nil {
		return messages
	}
	in := fwctx.Input{
		MemoryMD: readWorkspaceMarkdown(a.config.Workspace, "MEMORY.md"),
		UserMD:   readWorkspaceMarkdown(a.config.Workspace, "USER.md"),
	}
	encoded := fwctx.Encode(fwctx.Build(in).Stable, "")
	return replaceOrInsertFirstSystem(messages, encoded)
}

func ephemeralFor(trace *RunTrace) string {
	if trace == nil || trace.ContextOps == nil {
		return ""
	}
	n := len(trace.ContextOps.Invocations)
	if n < 2 {
		return ""
	}
	prev := trace.ContextOps.Invocations[n-2]
	if prev.L0DroppedMessages > 0 {
		return "上下文已按预算裁剪较早轮次。"
	}
	return ""
}

func replaceOrInsertFirstSystem(msgs []model.Message, content string) []model.Message {
	if strings.TrimSpace(content) == "" {
		return msgs
	}
	for i := range msgs {
		if !strings.EqualFold(msgs[i].Role, "system") {
			continue
		}
		if protectedSystemOrigin(msgs[i]) {
			continue
		}
		out := append([]model.Message(nil), msgs...)
		out[i].Content = content
		return out
	}
	out := make([]model.Message, 0, len(msgs)+1)
	out = append(out, model.Message{Role: "system", Content: content})
	out = append(out, msgs...)
	return out
}

func protectedSystemOrigin(m model.Message) bool {
	if m.Metadata == nil {
		return false
	}
	origin, _ := m.Metadata[model.MetadataKeySixathOrigin].(string)
	return strings.TrimSpace(origin) != ""
}
