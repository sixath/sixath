package service

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	agentv1 "backend/api/agent/v1"
	"backend/api/common"
	"backend/internal/biz"
	"backend/internal/chat"
	"backend/internal/validator"

	"github.com/go-kratos/kratos/v2/errors"
	"github.com/go-kratos/kratos/v2/log"
	agent "github.com/sixath/framework/harness"
	"github.com/sixath/framework/model"
	"github.com/sixath/framework/tool"
)

func baseSuccess() *common.BaseResponse {
	return &common.BaseResponse{Code: 0, Message: "ok"}
}

func baseFail(code int32, msg string) *common.BaseResponse {
	return &common.BaseResponse{Code: code, Message: msg}
}

func protoToBizModelConfig(mc *agentv1.ModelConfig) biz.ModelConfig {
	if mc == nil {
		return biz.ModelConfig{}
	}
	return biz.ModelConfig{
		Provider:        mc.GetProvider(),
		Model:           mc.GetModel(),
		APIKey:          mc.GetApiKey(),
		BaseURL:         mc.GetBaseUrl(),
		MaxOutputTokens: int(mc.GetMaxOutputTokens()),
		CodeProvider:    mc.GetCodeProvider(),
		CodeModel:       mc.GetCodeModel(),
		CodeAPIKey:      mc.GetCodeApiKey(),
		CodeBaseURL:     mc.GetCodeBaseUrl(),
	}
}

// AgentService implements agent.v1.AgentHTTPServer and agent.v1.AgentServer
type AgentService struct {
	agentv1.UnimplementedAgentServer
	uc          *biz.AgentUsecase
	toolUC      *biz.ToolUsecase
	mcpServerUC *biz.McpServerUsecase
	skillUC     *biz.SkillResourceUsecase
	channelUC   *biz.ChannelUsecase
	codeRoots   []string
	log         *log.Helper
}

// NewAgentService creates an AgentService
func NewAgentService(uc *biz.AgentUsecase, toolUC *biz.ToolUsecase, mcpServerUC *biz.McpServerUsecase, skillUC *biz.SkillResourceUsecase, channelUC *biz.ChannelUsecase, codeRoots []string, logger log.Logger) *AgentService {
	return &AgentService{uc: uc, toolUC: toolUC, mcpServerUC: mcpServerUC, skillUC: skillUC, channelUC: channelUC, codeRoots: codeRoots, log: log.NewHelper(logger)}
}

func (s *AgentService) sharedSkillDirs(ctx context.Context, agentID string) ([]string, error) {
	if s.skillUC == nil {
		return nil, nil
	}
	return s.skillUC.SharedSkillDirs(ctx, agentID)
}

// BindWecomChannel 将 Agent 绑定到指定 wecom Channel（写入 agents.wecom_channel_id）。
func (s *AgentService) BindWecomChannel(ctx context.Context, agentID, channelID string) error {
	if agentID == "" || channelID == "" {
		return nil
	}
	if err := s.validateWecomChannel(ctx, channelID); err != nil {
		return err
	}
	_, err := s.uc.Update(ctx, agentID, map[string]any{"wecom_channel_id": channelID})
	return err
}

func (s *AgentService) validateWecomChannel(ctx context.Context, channelID string) error {
	if channelID == "" {
		return nil
	}
	ch, err := s.channelUC.Get(ctx, channelID)
	if err != nil {
		return err
	}
	if ch.Type != "wecom" || !ch.Enabled {
		return errors.BadRequest("INVALID", "wecom_channel_id must refer to an enabled wecom channel")
	}
	return nil
}

func agentMetaToReply(m *biz.AgentMeta) *agentv1.AgentReply {
	return &agentv1.AgentReply{
		Ret:          baseSuccess(),
		Id:           m.ID,
		Name:         m.Name,
		Description:  m.Description,
		SystemPrompt: m.SystemPrompt,
		ModelConfig: &agentv1.ModelConfig{
			Provider:        m.ModelConfig.Provider,
			Model:           m.ModelConfig.Model,
			ApiKey:          m.ModelConfig.APIKey,
			BaseUrl:         m.ModelConfig.BaseURL,
			MaxOutputTokens: int32(m.ModelConfig.MaxOutputTokens),
			CodeProvider:    m.ModelConfig.CodeProvider,
			CodeModel:       m.ModelConfig.CodeModel,
			CodeApiKey:      m.ModelConfig.CodeAPIKey,
			CodeBaseUrl:     m.ModelConfig.CodeBaseURL,
		},
		Workspace:      m.Workspace,
		ToolIds:        m.ToolIDs,
		McpServerIds:   m.McpServerIDs,
		DebugRun:       m.DebugRun,
		WecomChannelId: m.WecomChannelID,
		RuntimeTools:   biz.RuntimeToolsToProto(m.RuntimeTools),
		CreatedAt:      m.CreatedAt.Format(time.RFC3339),
		UpdatedAt:      m.UpdatedAt.Format(time.RFC3339),
	}
}

// CreateAgent implements agent.v1.AgentHTTPServer
func (s *AgentService) CreateAgent(ctx context.Context, req *agentv1.CreateAgentRequest) (*agentv1.AgentReply, error) {
	mc := req.GetModelConfig()
	modelConfig := protoToBizModelConfig(mc)
	if err := s.validateWecomChannel(ctx, req.GetWecomChannelId()); err != nil {
		return nil, err
	}
	rt := biz.RuntimeToolsFromProto(req.GetRuntimeTools())
	workspace := strings.TrimSpace(req.GetWorkspace())
	if chat.WorkspaceUnderCodeRoots(workspace, s.codeRoots) {
		return nil, biz.ErrWorkspaceWholeRepoRetired
	}
	agent, err := s.uc.Create(ctx, req.GetName(), req.GetDescription(), req.GetSystemPrompt(), workspace, modelConfig, req.GetDebugRun(), req.GetWecomChannelId(), rt, req.GetToolIds())
	if err != nil {
		s.log.Errorf("CreateAgent failed: name=%s workspace=%s err=%v", req.GetName(), req.GetWorkspace(), err)
		return nil, err
	}
	return agentMetaToReply(agent), nil
}

// GetAgent implements agent.v1.AgentHTTPServer
func (s *AgentService) GetAgent(ctx context.Context, req *agentv1.GetAgentRequest) (*agentv1.AgentReply, error) {
	agent, err := s.uc.Get(ctx, req.GetId())
	if err != nil {
		s.log.Errorf("GetAgent failed: agent_id=%s err=%v", req.GetId(), err)
		return nil, err
	}
	return agentMetaToReply(agent), nil
}

// ListAgents implements agent.v1.AgentHTTPServer
func (s *AgentService) ListAgents(ctx context.Context, req *agentv1.ListAgentsRequest) (*agentv1.ListAgentsReply, error) {
	items, total, err := s.uc.List(ctx, req.GetPage(), req.GetPageSize())
	if err != nil {
		s.log.Errorf("ListAgents failed: page=%d page_size=%d err=%v", req.GetPage(), req.GetPageSize(), err)
		return nil, err
	}
	replies := make([]*agentv1.AgentReply, len(items))
	for i, m := range items {
		replies[i] = agentMetaToReply(m)
	}
	return &agentv1.ListAgentsReply{Ret: baseSuccess(), Items: replies, Total: int32(total)}, nil
}

// UpdateAgent implements agent.v1.AgentHTTPServer
func (s *AgentService) UpdateAgent(ctx context.Context, req *agentv1.UpdateAgentRequest) (*agentv1.AgentReply, error) {
	updates := make(map[string]any)
	if req.Name != nil {
		updates["name"] = *req.Name
	}
	if req.Description != nil {
		updates["description"] = *req.Description
	}
	if req.SystemPrompt != nil {
		updates["system_prompt"] = *req.SystemPrompt
	}
	if req.ModelConfig != nil {
		updates["model_config"] = protoToBizModelConfig(req.ModelConfig)
	}
	if req.Workspace != nil {
		updates["workspace"] = *req.Workspace
	}
	if req.DebugRun != nil {
		updates["debug_run"] = *req.DebugRun
	}
	if req.RuntimeTools != nil {
		rt := biz.RuntimeToolsFromProto(req.RuntimeTools)
		// Old clients may omit optional presence fields; preserve stored values.
		needPreserve := req.RuntimeTools.HybridRecall == nil ||
			req.RuntimeTools.HubGovernance == nil ||
			req.RuntimeTools.HubKnowledge == nil ||
			req.RuntimeTools.HubFallbackToDefaultOnReadError == nil
		if needPreserve {
			existing, err := s.uc.GetForEdit(ctx, req.GetId())
			if err != nil {
				return nil, err
			}
			if req.RuntimeTools.HybridRecall == nil && existing.RuntimeTools.HybridRecall != nil {
				v := *existing.RuntimeTools.HybridRecall
				rt.HybridRecall = &v
			}
			if req.RuntimeTools.HubGovernance == nil && existing.RuntimeTools.HubGovernance != nil {
				v := *existing.RuntimeTools.HubGovernance
				rt.HubGovernance = &v
			}
			if req.RuntimeTools.HubKnowledge == nil && existing.RuntimeTools.HubKnowledge != nil {
				v := *existing.RuntimeTools.HubKnowledge
				rt.HubKnowledge = &v
			}
			if req.RuntimeTools.HubFallbackToDefaultOnReadError == nil && existing.RuntimeTools.HubFallbackToDefaultOnReadError != nil {
				v := *existing.RuntimeTools.HubFallbackToDefaultOnReadError
				rt.HubFallbackToDefaultOnReadError = &v
			}
		}
		updates["runtime_tools"] = rt
	}
	if req.WecomChannelId != nil {
		if err := s.validateWecomChannel(ctx, *req.WecomChannelId); err != nil {
			return nil, err
		}
		updates["wecom_channel_id"] = *req.WecomChannelId
	}
	agent, err := s.uc.Update(ctx, req.GetId(), updates)
	if err != nil {
		s.log.Errorf("UpdateAgent failed: agent_id=%s err=%v", req.GetId(), err)
		return nil, err
	}
	return agentMetaToReply(agent), nil
}

// DeleteAgent implements agent.v1.AgentHTTPServer
func (s *AgentService) DeleteAgent(ctx context.Context, req *agentv1.DeleteAgentRequest) (*agentv1.DeleteAgentReply, error) {
	if err := s.uc.Delete(ctx, req.GetId()); err != nil {
		s.log.Errorf("DeleteAgent failed: agent_id=%s err=%v", req.GetId(), err)
		return nil, err
	}
	return &agentv1.DeleteAgentReply{Ret: baseSuccess()}, nil
}

// BindTools implements agent.v1.AgentHTTPServer (id from path param)
func (s *AgentService) BindTools(ctx context.Context, req *agentv1.BindToolsRequest) (*agentv1.BindToolsReply, error) {
	agentID := req.GetId()
	if agentID == "" {
		return nil, biz.ErrAgentNotFound
	}
	if err := s.uc.BindTools(ctx, agentID, req.GetToolIds()); err != nil {
		s.log.Errorf("BindTools failed: agent_id=%s tool_ids=%v err=%v", agentID, req.GetToolIds(), err)
		return nil, err
	}
	return &agentv1.BindToolsReply{Ret: baseSuccess()}, nil
}

// UnbindTools implements agent.v1.AgentHTTPServer (id from path param)
func (s *AgentService) UnbindTools(ctx context.Context, req *agentv1.UnbindToolsRequest) (*agentv1.UnbindToolsReply, error) {
	agentID := req.GetId()
	if agentID == "" {
		return nil, biz.ErrAgentNotFound
	}
	if err := s.uc.UnbindTools(ctx, agentID, req.GetToolIds()); err != nil {
		s.log.Errorf("UnbindTools failed: agent_id=%s tool_ids=%v err=%v", agentID, req.GetToolIds(), err)
		return nil, err
	}
	return &agentv1.UnbindToolsReply{Ret: baseSuccess()}, nil
}

// Chat implements agent.v1.AgentHTTPServer (id from path param)
// 快捷对话：单轮对话，不持久化，使用 framework ReActAgent
func (s *AgentService) Chat(ctx context.Context, req *agentv1.ChatRequest) (*agentv1.ChatReply, error) {
	agentID := req.GetId()
	content := req.GetContent()
	if agentID == "" {
		return nil, biz.ErrAgentNotFound
	}
	if content == "" {
		return &agentv1.ChatReply{Ret: baseFail(400, "content is required"), Content: ""}, nil
	}

	agentMeta, err := s.uc.GetForUse(ctx, agentID)
	if err != nil {
		s.log.Errorf("Chat get agent failed: agent_id=%s err=%v", agentID, err)
		return nil, err
	}
	if err := biz.RequireWorkspaceRoot(agentMeta.Workspace); err != nil {
		return nil, err
	}

	tools, err := s.toolUC.ListByAgent(ctx, agentID)
	if err != nil {
		s.log.Errorf("Chat list tools failed: agent_id=%s err=%v", agentID, err)
		return nil, err
	}
	mcpServerMetas, err := s.listMcpServersByAgent(ctx, agentID)
	if err != nil {
		s.log.Errorf("Chat list mcp servers failed: agent_id=%s err=%v", agentID, err)
		return nil, err
	}

	m, err := chat.BuildModel(
		agentMeta.ModelConfig.Provider,
		agentMeta.ModelConfig.Model,
		agentMeta.ModelConfig.APIKey,
		agentMeta.ModelConfig.BaseURL,
	)
	if err != nil {
		s.log.Errorf("Chat build model failed: agent_id=%s provider=%s model=%s err=%v", agentID, agentMeta.ModelConfig.Provider, agentMeta.ModelConfig.Model, err)
		return nil, err
	}
	reg := tool.NewRegistry()
	regResult, err := chat.BuildRegistry(tools, mcpServerMetas, reg, chat.RegistryBuildOptions{Workspace: agentMeta.Workspace})
	if err != nil {
		s.log.Errorf("Chat build tool registry failed: agent_id=%s err=%v", agentID, err)
		return nil, err
	}
	mcpServers := regResult.McpServers

	extraSkillDirs, err := s.sharedSkillDirs(ctx, agentID)
	if err != nil {
		return nil, err
	}
	skillsIdx, err := chat.BuildSkillsIndex(agentMeta.Workspace, extraSkillDirs)
	if err != nil {
		s.log.Errorf("Chat build skills index failed: agent_id=%s workspace=%s err=%v", agentID, agentMeta.Workspace, err)
		return nil, err
	}
	if err := chat.RegisterAgentRuntimeTools(reg, chat.AgentRuntimeToolsOptions{
		Flags:          chat.HermesP0FlagsPtrForAgent(agentMeta),
		SkillsIdx:      skillsIdx,
		McpServers:     mcpServers,
		AllowScript:    true,
		VisionAnalyzer: chat.VisionAnalyzerForModel(m),
	}); err != nil {
		s.log.Errorf("Chat register runtime tools failed: agent_id=%s err=%v", agentID, err)
		return nil, err
	}
	if err := chat.RegisterLearningTools(reg); err != nil {
		s.log.Errorf("Chat register append_learning failed: agent_id=%s err=%v", agentID, err)
		return nil, err
	}
	registerWeComToolForAgent(ctx, s.channelUC, reg, agentMeta)

	wecomChannelID := resolveAgentWecomChannelID(ctx, s.channelUC, agentMeta)
	catalogInput := chat.CatalogWiringInput{
		Reg:            reg,
		DsBindings:     regResult.DsBindings,
		WecomChannelID: wecomChannelID,
		ChannelType:    "wecom",
		SkillsIdx:      skillsIdx,
	}
	catalog, toolSearchActive, err := chat.WireCatalogAndToolSearch(ctx, catalogInput)
	if err != nil {
		s.log.Errorf("Chat wire catalog/tool_search failed: agent_id=%s err=%v", agentID, err)
		return nil, err
	}

	agentText := chat.AppendAskUserToolPrompt(agentMeta.SystemPrompt)
	agentText = appendWecomBoundSystemPrompt(ctx, s.channelUC, agentText, agentMeta)
	opts := append(chat.ReActOptionsFromAgent(*agentMeta), chat.HarnessReActOptions(agentMeta.Workspace, extraSkillDirs)...)
	a := chat.BuildReActAgent(m, reg, agentText, 20, opts...)

	messages := make([]model.Message, 0, 1)
	messages = append(messages, model.Message{Role: "user", Content: content})

	runCtx := context.WithValue(ctx, tool.ContextKeyWorkspaceRoot, agentMeta.Workspace)
	runCtx = context.WithValue(runCtx, tool.ContextKeyAgentID, agentID)
	runCtx = context.WithValue(runCtx, tool.ContextKeyAgentName, agentMeta.Name)
	runCtx = context.WithValue(runCtx, tool.ContextKeyToolCatalog, catalog)
	if toolSearchActive {
		runCtx = context.WithValue(runCtx, tool.ContextKeyToolSearchActive, true)
	}
	md := chat.RequestMetadataFromContext(runCtx)
	if md == nil {
		md = map[string]any{}
	}
	if agentMeta.Workspace != "" {
		md["workspace_root"] = agentMeta.Workspace
	}
	agentReq := &agent.Request{Messages: messages, Metadata: md}
	resp, err := a.Run(runCtx, agentReq)
	if err != nil {
		isH, vis, _, raw := chat.DecomposeGuardrailRunError(err)
		if isH && !raw && vis != "" {
			return &agentv1.ChatReply{
				Ret:     baseSuccess(),
				Content: vis,
			}, nil
		}
		s.log.Errorf("Chat run agent failed: agent_id=%s err=%v", agentID, err)
		return nil, err
	}

	return &agentv1.ChatReply{
		Ret:     baseSuccess(),
		Content: resp.Text,
	}, nil
}

// ValidateSkillPackage implements agent.v1.AgentHTTPServer (id from path param)
func (s *AgentService) ValidateSkillPackage(ctx context.Context, req *agentv1.ValidateSkillPackageRequest) (*agentv1.ValidateSkillPackageReply, error) {
	agentID := req.GetId()
	if agentID == "" {
		return nil, biz.ErrAgentNotFound
	}
	_, err := s.uc.Get(ctx, agentID)
	if err != nil {
		s.log.Errorf("ValidateSkillPackage get agent failed: agent_id=%s err=%v", agentID, err)
		return nil, err
	}
	result := validator.ValidateSkillPackage(req.GetFile())
	return &agentv1.ValidateSkillPackageReply{
		Ret:     baseSuccess(),
		Valid:   result.Valid,
		Message: result.Message,
		Errors:  result.Errors,
	}, nil
}

// UploadSkillPackage implements agent.v1.AgentHTTPServer (id from path param)
func (s *AgentService) UploadSkillPackage(ctx context.Context, req *agentv1.UploadSkillPackageRequest) (*agentv1.UploadSkillPackageReply, error) {
	agentID := req.GetId()
	if agentID == "" {
		return nil, biz.ErrAgentNotFound
	}
	agent, err := s.uc.GetForEdit(ctx, agentID)
	if err != nil {
		s.log.Errorf("UploadSkillPackage get agent failed: agent_id=%s err=%v", agentID, err)
		return nil, err
	}
	if chat.WorkspaceUnderCodeRoots(agent.Workspace, s.codeRoots) {
		const msg = "workspace is under read-only code root; use subdirectory mode (workspace/code)"
		return &agentv1.UploadSkillPackageReply{
			Ret:     baseFail(400, msg),
			Success: false,
			Message: msg,
		}, nil
	}
	result := validator.ValidateSkillPackage(req.GetFile())
	if !result.Valid {
		return &agentv1.UploadSkillPackageReply{
			Ret:     baseFail(400, result.Message),
			Success: false,
			Message: result.Message,
		}, nil
	}
	skillsDir := filepath.Join(agent.Workspace, "skills")
	if err := os.MkdirAll(skillsDir, 0755); err != nil {
		s.log.Errorf("UploadSkillPackage create skills dir failed: agent_id=%s skills_dir=%s err=%v", agentID, skillsDir, err)
		return &agentv1.UploadSkillPackageReply{Ret: baseFail(500, "failed to create skills dir"), Success: false, Message: err.Error()}, nil
	}
	if err := validator.ExtractSkillPackage(req.GetFile(), skillsDir); err != nil {
		s.log.Errorf("UploadSkillPackage extract failed: agent_id=%s skills_dir=%s err=%v", agentID, skillsDir, err)
		return &agentv1.UploadSkillPackageReply{Ret: baseFail(500, "failed to extract"), Success: false, Message: err.Error()}, nil
	}
	return &agentv1.UploadSkillPackageReply{Ret: baseSuccess(), Success: true, Message: "ok"}, nil
}

// ExecuteSkill implements agent.v1.AgentHTTPServer (id from path param)
// 直接执行 Agent workspace/skills 下的脚本，path 格式为 skill-name/scripts/xxx.sh
func (s *AgentService) ExecuteSkill(ctx context.Context, req *agentv1.ExecuteSkillRequest) (*agentv1.ExecuteSkillReply, error) {
	agentID := req.GetId()
	path := strings.TrimSpace(req.GetPath())
	if agentID == "" {
		return nil, biz.ErrAgentNotFound
	}
	if path == "" {
		return &agentv1.ExecuteSkillReply{Ret: baseFail(400, "path is required"), Output: ""}, nil
	}

	agentMeta, err := s.uc.GetForUse(ctx, agentID)
	if err != nil {
		s.log.Errorf("ExecuteSkill get agent failed: agent_id=%s err=%v", agentID, err)
		return nil, err
	}

	// path 格式: skill-name/scripts/run.sh
	parts := strings.SplitN(path, "/", 2)
	if len(parts) < 2 {
		return &agentv1.ExecuteSkillReply{Ret: baseFail(400, "path must be skill-name/scripts/xxx.sh"), Output: ""}, nil
	}
	skillName, relPath := parts[0], parts[1]
	if skillName == "" || relPath == "" {
		return &agentv1.ExecuteSkillReply{Ret: baseFail(400, "path must be skill-name/scripts/xxx.sh"), Output: ""}, nil
	}
	if !strings.HasPrefix(relPath, "scripts") {
		return &agentv1.ExecuteSkillReply{Ret: baseFail(400, "path must be under scripts/"), Output: ""}, nil
	}

	extraSkillDirs, err := s.sharedSkillDirs(ctx, agentID)
	if err != nil {
		return nil, err
	}
	output, err := chat.ExecuteSkillScript(ctx, agentMeta.Workspace, extraSkillDirs, skillName, relPath, req.GetInput())
	if err != nil {
		s.log.Errorf("ExecuteSkill failed: agent_id=%s skill=%s path=%s err=%v", agentID, skillName, relPath, err)
		return &agentv1.ExecuteSkillReply{Ret: baseFail(500, err.Error()), Output: err.Error()}, nil
	}
	return &agentv1.ExecuteSkillReply{
		Ret:    baseSuccess(),
		Output: output,
	}, nil
}

// skillNamePattern 限制 skill_name 为字母、数字、下划线、连字符，防止路径穿越
var skillNamePattern = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

// ListSkills implements agent.v1.AgentHTTPServer (id from path param)
// 列出该 Agent 下所有技能，扫描 workspace/skills 子目录，含 SKILL.md 视为有效
func (s *AgentService) ListSkills(ctx context.Context, req *agentv1.ListSkillsRequest) (*agentv1.ListSkillsReply, error) {
	agentID := req.GetId()
	if agentID == "" {
		return nil, biz.ErrAgentNotFound
	}
	agentMeta, err := s.uc.Get(ctx, agentID)
	if err != nil {
		s.log.Errorf("ListSkills get agent failed: agent_id=%s err=%v", agentID, err)
		return nil, err
	}
	extraSkillDirs, err := s.sharedSkillDirs(ctx, agentID)
	if err != nil {
		return nil, err
	}
	skillsIdx, err := chat.BuildSkillsIndex(agentMeta.Workspace, extraSkillDirs)
	if err != nil {
		s.log.Errorf("ListSkills build skills index failed: agent_id=%s workspace=%s err=%v", agentID, agentMeta.Workspace, err)
		return &agentv1.ListSkillsReply{Ret: baseFail(500, err.Error()), Items: nil}, nil
	}
	if skillsIdx == nil {
		return &agentv1.ListSkillsReply{Ret: baseSuccess(), Items: []*agentv1.SkillMeta{}}, nil
	}
	all := skillsIdx.All()
	items := make([]*agentv1.SkillMeta, len(all))
	for i, m := range all {
		// path 返回技能目录的绝对路径（SKILL.md 的父目录）
		skillDir := filepath.Dir(m.Path)
		items[i] = &agentv1.SkillMeta{
			Name:        m.Name,
			Description: m.Description,
			Path:        skillDir,
		}
	}
	return &agentv1.ListSkillsReply{Ret: baseSuccess(), Items: items}, nil
}

// DeleteSkill implements agent.v1.AgentHTTPServer (id, skill_name from path param)
// 删除指定技能，移除 workspace/skills/{skill_name}/ 目录及其下所有文件
func (s *AgentService) DeleteSkill(ctx context.Context, req *agentv1.DeleteSkillRequest) (*agentv1.DeleteSkillReply, error) {
	agentID := req.GetId()
	skillName := strings.TrimSpace(req.GetSkillName())
	if agentID == "" {
		return nil, biz.ErrAgentNotFound
	}
	if skillName == "" {
		return &agentv1.DeleteSkillReply{Ret: baseFail(400, "skill_name is required")}, nil
	}
	if !skillNamePattern.MatchString(skillName) {
		return &agentv1.DeleteSkillReply{Ret: baseFail(400, "skill_name must contain only letters, numbers, underscores and hyphens")}, nil
	}
	agentMeta, err := s.uc.GetForEdit(ctx, agentID)
	if err != nil {
		s.log.Errorf("DeleteSkill get agent failed: agent_id=%s skill=%s err=%v", agentID, skillName, err)
		return nil, err
	}
	skillsDir := filepath.Join(agentMeta.Workspace, "skills")
	targetDir := filepath.Join(skillsDir, skillName)
	// 校验路径落在 workspace/skills 下，防止路径穿越
	absSkills, err := filepath.Abs(skillsDir)
	if err != nil {
		s.log.Errorf("DeleteSkill resolve skills path failed: agent_id=%s skills_dir=%s err=%v", agentID, skillsDir, err)
		return &agentv1.DeleteSkillReply{Ret: baseFail(500, "failed to resolve skills path")}, nil
	}
	absTarget, err := filepath.Abs(targetDir)
	if err != nil {
		s.log.Errorf("DeleteSkill resolve target path failed: agent_id=%s target_dir=%s err=%v", agentID, targetDir, err)
		return &agentv1.DeleteSkillReply{Ret: baseFail(500, "failed to resolve target path")}, nil
	}
	if !strings.HasPrefix(absTarget, absSkills+string(filepath.Separator)) && absTarget != absSkills {
		return &agentv1.DeleteSkillReply{Ret: baseFail(400, "invalid skill path")}, nil
	}
	if err := os.RemoveAll(targetDir); err != nil {
		s.log.Errorf("DeleteSkill remove skill dir failed: agent_id=%s target_dir=%s err=%v", agentID, targetDir, err)
		return &agentv1.DeleteSkillReply{Ret: baseFail(500, "failed to delete skill: "+err.Error())}, nil
	}
	return &agentv1.DeleteSkillReply{Ret: baseSuccess()}, nil
}

func (s *AgentService) listMcpServersByAgent(ctx context.Context, agentID string) ([]*biz.McpServerMeta, error) {
	if s == nil || s.mcpServerUC == nil {
		return nil, nil
	}
	return s.mcpServerUC.ListByAgent(ctx, agentID)
}
