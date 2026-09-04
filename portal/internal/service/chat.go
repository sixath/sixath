package service

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"time"

	chatv1 "backend/api/chat/v1"
	"backend/api/common"
	"backend/internal/biz"
	"backend/internal/chat"
	"backend/internal/data"

	"github.com/go-kratos/kratos/v2/log"
	"github.com/sixath/framework/agent"
	"github.com/sixath/framework/events"
	"github.com/sixath/framework/mea"
	"github.com/sixath/framework/memory"
	"github.com/sixath/framework/model"
	"github.com/sixath/framework/sessionsearch"
	"github.com/sixath/framework/tool"
	toolskill "github.com/sixath/framework/tool/skillops"
	"github.com/sixath/framework/turntrace"
	"google.golang.org/protobuf/types/known/structpb"
)

// ChatService implements chat.v1.ChatHTTPServer
// 对话服务：会话管理、消息发送与历史（详见 architecture_design.md 5.4、5.5）
type ChatService struct {
	chatv1.UnimplementedChatServer
	chatUC         *biz.ChatUsecase
	agentUC        *biz.AgentUsecase
	toolUC         *biz.ToolUsecase
	mcpServerUC    *biz.McpServerUsecase
	skillUC        *biz.SkillResourceUsecase
	growthUC       *biz.GrowthUsecase
	channelUC      *biz.ChannelUsecase
	sessionHooks   *agent.ChatSessionHookRegistry
	memoryStore    memory.MemoryStore
	turnTraceStore turntrace.Store
	// bgReviewer is the C3 in-process fork (GrowthWorker); optional until newApp wires it.
	bgReviewer BackgroundReviewer
	// bgReviewSpawnHook overrides spawnBackgroundReviewOnce in tests (sync spy).
	bgReviewSpawnHook func(BackgroundReviewParams)
	log               *log.Helper
}

// NewChatService creates a ChatService
func NewChatService(chatUC *biz.ChatUsecase, agentUC *biz.AgentUsecase, toolUC *biz.ToolUsecase, skillUC *biz.SkillResourceUsecase, growthUC *biz.GrowthUsecase, channelUC *biz.ChannelUsecase, logger log.Logger) *ChatService {
	return newChatService(chatUC, agentUC, toolUC, nil, skillUC, growthUC, channelUC, memory.NewSessionMemory(), logger)
}

// NewChatServiceWithMemoryStore creates a ChatService with the durable session
// memory backend supplied by the data layer.
func NewChatServiceWithMemoryStore(chatUC *biz.ChatUsecase, agentUC *biz.AgentUsecase, toolUC *biz.ToolUsecase, mcpServerUC *biz.McpServerUsecase, skillUC *biz.SkillResourceUsecase, growthUC *biz.GrowthUsecase, channelUC *biz.ChannelUsecase, sessionUnits memory.SessionUnitsBackend, logger log.Logger) *ChatService {
	return newChatService(chatUC, agentUC, toolUC, mcpServerUC, skillUC, growthUC, channelUC, sessionUnits, logger)
}

// ProvideChatServiceWithTurnTrace builds ChatService with durable memory and turn-trace store (wire).
func ProvideChatServiceWithTurnTrace(chatUC *biz.ChatUsecase, agentUC *biz.AgentUsecase, toolUC *biz.ToolUsecase, mcpServerUC *biz.McpServerUsecase, skillUC *biz.SkillResourceUsecase, growthUC *biz.GrowthUsecase, channelUC *biz.ChannelUsecase, sessionUnits memory.SessionUnitsBackend, turnTraceStore turntrace.Store, d *data.Data, logger log.Logger) *ChatService {
	WireMemoryHubFromData(d)
	s := NewChatServiceWithMemoryStore(chatUC, agentUC, toolUC, mcpServerUC, skillUC, growthUC, channelUC, sessionUnits, logger)
	s.SetTurnTraceStore(turnTraceStore)
	return s
}

// SetTurnTraceStore sets the optional TurnTrace persistence backend (nil-safe).
func (s *ChatService) SetTurnTraceStore(st turntrace.Store) {
	if s == nil {
		return
	}
	s.turnTraceStore = st
}

func (s *ChatService) persistTurnTrace(ctx context.Context, sessionID, agentID string, tr *agent.RunTrace) {
	if s == nil || tr == nil {
		return
	}
	chat.PersistTurnTraceIfEnabled(ctx, s.turnTraceStore, agent.TurnTraceMeta{
		SessionID: sessionID,
		AgentID:   agentID,
		RequestID: chat.TurnTraceRequestID(ctx, tr),
	}, tr, &chat.TurnTraceIndexOpts{
		ChatUC: s.chatUC,
		SessMeta: sessionsearch.SessionMeta{
			ID:      sessionID,
			AgentID: agentID,
		},
	})
}

// persistCompactBoundary writes a compact_boundary system message when L2 ran.
// Same call sites as persistTurnTrace (Trace present) — not SaveAssistantMessage alone.
func (s *ChatService) persistCompactBoundary(ctx context.Context, sessionID string, tr *agent.RunTrace) {
	if s == nil || s.chatUC == nil {
		return
	}
	chat.PersistCompactBoundaryIfNeeded(ctx, s.chatUC, sessionID, tr)
	if forked := chat.ForkSessionOnCompactIfEnabled(ctx, s.chatUC, sessionID, tr); forked != "" && s.log != nil {
		s.log.Infof("compact fork_session created session_id=%s from=%s", forked, sessionID)
	}
}

func newChatService(chatUC *biz.ChatUsecase, agentUC *biz.AgentUsecase, toolUC *biz.ToolUsecase, mcpServerUC *biz.McpServerUsecase, skillUC *biz.SkillResourceUsecase, growthUC *biz.GrowthUsecase, channelUC *biz.ChannelUsecase, sessionUnits memory.SessionUnitsBackend, logger log.Logger) *ChatService {
	transcriptProvider := chat.NewChatTranscriptProvider(chatUC)
	chat.SetMemoryAgentGetter(agentUC)
	memoryStore := chat.BuildMemoryStore(sessionUnits, nil, transcriptProvider, chat.DefaultMemoryStoreOptions())
	chat.StartUnitVectorBackfill(sessionUnits)
	chatUC.SetSessionSearchBackend(chat.NewSessionSearchBackend(chatUC))
	chat.SetPrefetchSessionProvider(transcriptProvider)
	chat.SetPrefetchMemoryStore(memoryStore)
	chat.SetHubUnitWriter(chat.NewGatedMemoryUnitWriter(memoryStore, agentUC))
	chat.InitLocalMemoryHub() // Catalog defaults=local (+ UnitsWrite); does not change Prefetch
	chat.RebuildPrefetchMemoryOrchestrator()
	s := &ChatService{
		chatUC:       chatUC,
		agentUC:      agentUC,
		toolUC:       toolUC,
		mcpServerUC:  mcpServerUC,
		skillUC:      skillUC,
		growthUC:     growthUC,
		channelUC:    channelUC,
		sessionHooks: agent.NewChatSessionHookRegistry(),
		memoryStore:  memoryStore,
		log:          log.NewHelper(logger),
	}
	s.registerGrowthSessionHooks()
	s.registerBrowserSessionHooks()
	s.registerProcessSessionHooks()
	s.registerProcessNotifyWake()
	return s
}

func (s *ChatService) sharedSkillDirs(ctx context.Context, agentID string) ([]string, error) {
	if s.skillUC == nil {
		return nil, nil
	}
	return s.skillUC.SharedSkillDirs(ctx, agentID)
}

func (s *ChatService) listMcpServersByAgent(ctx context.Context, agentID string) ([]*biz.McpServerMeta, error) {
	if s == nil || s.mcpServerUC == nil {
		return nil, nil
	}
	// Session turns: ownership already checked via GetSession; do not re-ACL on MCP for peers.
	return s.mcpServerUC.ListByAgentForSession(ctx, agentID)
}

// SetChatSessionHooks replaces the on_chat_session_end registry (nil-safe; for tests).
func (s *ChatService) SetChatSessionHooks(r *agent.ChatSessionHookRegistry) {
	if s == nil {
		return
	}
	s.sessionHooks = r
}

// ChatSessionHooks returns the live registry so callers (e.g. Growth) can Register without replacing it.
func (s *ChatService) ChatSessionHooks() *agent.ChatSessionHookRegistry {
	if s == nil {
		return nil
	}
	return s.sessionHooks
}

// CreateSession 新建会话
func (s *ChatService) CreateSession(ctx context.Context, req *chatv1.CreateSessionRequest) (*chatv1.SessionReply, error) {
	agentID := req.GetAgentId()
	if agentID == "" {
		return nil, biz.ErrAgentNotFound
	}
	session, err := s.chatUC.CreateSession(ctx, agentID, req.GetTitle(), req.GetParentSessionId())
	if err != nil {
		s.log.Errorf("CreateSession failed: agent_id=%s err=%v", agentID, err)
		return nil, err
	}
	return sessionToReply(session), nil
}

// ListSessions 获取该 Agent 的会话列表（分页）
func (s *ChatService) ListSessions(ctx context.Context, req *chatv1.ListSessionsRequest) (*chatv1.ListSessionsReply, error) {
	agentID := req.GetAgentId()
	if agentID == "" {
		return nil, biz.ErrAgentNotFound
	}
	page, pageSize := req.GetPage(), req.GetPageSize()
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 10
	}
	includePreview := req.GetIncludePreview()
	items, total, err := s.chatUC.ListSessions(ctx, agentID, req.GetQ(), page, pageSize, includePreview)
	if err != nil {
		s.log.Errorf("ListSessions failed: agent_id=%s page=%d page_size=%d err=%v", agentID, page, pageSize, err)
		return nil, err
	}
	replies := make([]*chatv1.SessionReply, len(items))
	for i, sess := range items {
		replies[i] = sessionToReply(sess)
	}
	return &chatv1.ListSessionsReply{
		Ret:   &common.BaseResponse{Code: 0, Message: "ok"},
		Items: replies,
		Total: int32(total),
	}, nil
}

// ListAllSessions 跨 Agent 分页列出会话
func (s *ChatService) ListAllSessions(ctx context.Context, req *chatv1.ListAllSessionsRequest) (*chatv1.ListAllSessionsReply, error) {
	page, pageSize := req.GetPage(), req.GetPageSize()
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 50
	}
	items, total, err := s.chatUC.ListAllSessions(ctx, page, pageSize, req.GetIncludePreview())
	if err != nil {
		s.log.Errorf("ListAllSessions failed: page=%d page_size=%d err=%v", page, pageSize, err)
		return nil, err
	}
	replies := make([]*chatv1.SessionReply, len(items))
	for i, sess := range items {
		replies[i] = sessionToReply(sess)
	}
	return &chatv1.ListAllSessionsReply{
		Ret:   &common.BaseResponse{Code: 0, Message: "ok"},
		Items: replies,
		Total: int32(total),
	}, nil
}

// SearchSessions 跨 Agent FTS 搜索会话
func (s *ChatService) SearchSessions(ctx context.Context, req *chatv1.SearchSessionsRequest) (*chatv1.SearchSessionsReply, error) {
	hits, msg, err := s.chatUC.SearchSessions(ctx, req.GetQuery(), req.GetAgentId(), int(req.GetLimit()))
	if err != nil {
		s.log.Errorf("SearchSessions failed: query=%q err=%v", req.GetQuery(), err)
		return nil, err
	}
	replies := make([]*chatv1.SearchHitReply, len(hits))
	for i, h := range hits {
		replies[i] = &chatv1.SearchHitReply{
			SessionId:       h.SessionID,
			RootSessionId:   h.RootSessionID,
			AgentId:         h.AgentID,
			AgentName:       h.AgentName,
			Title:           h.Title,
			Preview:         h.Preview,
			MatchedSnippets: h.MatchedSnippets,
			UpdatedAt:       h.UpdatedAt.Format(time.RFC3339),
		}
	}
	return &chatv1.SearchSessionsReply{
		Ret:   &common.BaseResponse{Code: 0, Message: msg},
		Items: replies,
	}, nil
}

// SearchTranscript is the read-only UI API for SearchAnchored (message-level FTS).
// Route: GET /api/v1/agents/{agent_id}/transcript/search (hand-written HTTP; not in chat.proto).
func (s *ChatService) SearchTranscript(ctx context.Context, opts biz.TranscriptSearchOpts) (*biz.TranscriptSearchResult, error) {
	out, err := s.chatUC.SearchTranscript(ctx, opts)
	if err != nil {
		s.log.Errorf("SearchTranscript failed: agent_id=%s query=%q err=%v", opts.AgentID, opts.Query, err)
		return nil, err
	}
	return out, nil
}

// GetSession 获取会话详情
func (s *ChatService) GetSession(ctx context.Context, req *chatv1.GetSessionRequest) (*chatv1.SessionReply, error) {
	session, err := s.chatUC.GetSession(ctx, req.GetId())
	if err != nil {
		s.log.Errorf("GetSession failed: session_id=%s err=%v", req.GetId(), err)
		return nil, err
	}
	return sessionToReply(session), nil
}

// UpdateSession 更新会话（如标题）
func (s *ChatService) UpdateSession(ctx context.Context, req *chatv1.UpdateSessionRequest) (*chatv1.SessionReply, error) {
	session, err := s.chatUC.UpdateSession(ctx, req.GetId(), req.GetTitle())
	if err != nil {
		s.log.Errorf("UpdateSession failed: session_id=%s err=%v", req.GetId(), err)
		return nil, err
	}
	return sessionToReply(session), nil
}

// DeleteSession 删除会话（级联删除消息）
func (s *ChatService) DeleteSession(ctx context.Context, req *chatv1.DeleteSessionRequest) (*chatv1.DeleteSessionReply, error) {
	if err := s.chatUC.DeleteSession(ctx, req.GetId()); err != nil {
		s.log.Errorf("DeleteSession failed: session_id=%s err=%v", req.GetId(), err)
		return nil, err
	}
	if s.sessionHooks != nil {
		if herr := s.sessionHooks.OnChatSessionEnd(ctx, req.GetId()); herr != nil {
			s.log.Warnf("chat session end hooks: session_id=%s err=%v", req.GetId(), herr)
		}
	}
	return &chatv1.DeleteSessionReply{Ret: &common.BaseResponse{Code: 0, Message: "ok"}}, nil
}

// SendMessage 发送用户消息，触发 Agent 回复
func (s *ChatService) SendMessage(ctx context.Context, req *chatv1.SendMessageRequest) (*chatv1.MessageReply, error) {
	sessionID := req.GetSessionId()
	content := req.GetContent()
	if sessionID == "" || content == "" {
		return nil, biz.ErrSessionNotFound
	}

	session, err := s.chatUC.GetSession(ctx, sessionID)
	if err != nil {
		s.log.Errorf("SendMessage get session failed: session_id=%s err=%v", sessionID, err)
		return nil, err
	}
	if session.Readonly {
		return nil, biz.ErrSessionReadonly
	}

	agentMeta, err := s.agentUC.GetForSession(ctx, session.AgentID)
	if err != nil {
		s.log.Errorf("SendMessage get agent failed: session_id=%s agent_id=%s err=%v", sessionID, session.AgentID, err)
		return nil, err
	}

	// 保存 user 消息
	userMsg, err := s.chatUC.CreateMessage(ctx, sessionID, "user", content)
	if err != nil {
		s.log.Errorf("SendMessage save user message failed: session_id=%s err=%v", sessionID, err)
		return nil, err
	}
	go chat.NotifyMemorySessionDirty(ctx, sessionID, len(content), 1, s.chatUC, s.agentUC, chat.NewChatTranscriptProvider(s.chatUC))
	chat.NotifySessionMessageIndexed(ctx, s.chatUC, sessionID, userMsg)

	// 加载工具（session 归属已校验；channel peer 无 agent/tool PermUse）
	tools, err := s.toolUC.ListByAgentForSession(ctx, session.AgentID)
	if err != nil {
		s.log.Errorf("SendMessage list tools failed: session_id=%s agent_id=%s err=%v", sessionID, session.AgentID, err)
		return nil, err
	}
	mcpServerMetas, err := s.listMcpServersByAgent(ctx, session.AgentID)
	if err != nil {
		s.log.Errorf("SendMessage list mcp servers failed: session_id=%s agent_id=%s err=%v", sessionID, session.AgentID, err)
		return nil, err
	}

	// 构建模型
	m, err := chat.BuildModel(
		agentMeta.ModelConfig.Provider,
		agentMeta.ModelConfig.Model,
		agentMeta.ModelConfig.APIKey,
		agentMeta.ModelConfig.BaseURL,
	)
	if err != nil {
		s.log.Errorf("SendMessage build model failed: session_id=%s agent_id=%s provider=%s model=%s err=%v", sessionID, session.AgentID, agentMeta.ModelConfig.Provider, agentMeta.ModelConfig.Model, err)
		return nil, err
	}

	ir := chat.InputResponseFromContext(ctx)
	userForIntent := chat.UserMessageContentForTurn(content, ir)
	active, surfaceRes := chat.PrepareTurnToolSurface(ctx, userForIntent, tools, mcpServerMetas, agentMeta, m)
	s.log.Infof("turn tool surface: session_id=%s source=%s conf=%s active=%v candidates=%v reason=%s",
		sessionID, surfaceRes.Source, surfaceRes.Confidence, surfaceRes.ActiveFamilies, surfaceRes.Candidates, surfaceRes.Reason)
	m, err = chat.ResolveTurnModel(active, m, *agentMeta)
	if err != nil {
		s.log.Errorf("resolve turn model failed: session_id=%s agent_id=%s err=%v", sessionID, session.AgentID, err)
		return nil, err
	}

	reg := tool.NewRegistry()
	var mcpServers []toolskill.McpServerEntry
	regResult, err := chat.BuildRegistry(tools, mcpServerMetas, reg, chat.RegistryBuildOptions{ActiveFamilies: active})
	if err != nil {
		s.log.Errorf("SendMessage build tool registry failed: session_id=%s agent_id=%s err=%v", sessionID, session.AgentID, err)
		return nil, err
	}
	mcpServers = regResult.McpServers

	extraSkillDirs, err := s.sharedSkillDirs(ctx, session.AgentID)
	if err != nil {
		return nil, err
	}
	skillsIdx, err := chat.BuildSkillsIndex(agentMeta.Workspace, extraSkillDirs)
	if err != nil {
		s.log.Errorf("SendMessage build skills index failed: session_id=%s agent_id=%s workspace=%s err=%v", sessionID, session.AgentID, agentMeta.Workspace, err)
		return nil, err
	}
	streamSessionProvider := chat.NewChatTranscriptProvider(s.chatUC)
	if err := chat.RegisterAgentRuntimeTools(reg, chat.AgentRuntimeToolsOptions{
		Flags:           chat.HermesP0FlagsPtrForAgent(agentMeta),
		SkillsIdx:       skillsIdx,
		McpServers:      mcpServers,
		AllowScript:     true,
		MemoryStore:     s.memoryStore,
		SessionProvider: streamSessionProvider,
		VisionAnalyzer:  chat.VisionAnalyzerForModel(m),
		RuntimeTools:    agentMeta.RuntimeTools,
		ActiveFamilies:  active,
	}); err != nil {
		s.log.Errorf("SendMessage register runtime tools failed: session_id=%s agent_id=%s err=%v", sessionID, session.AgentID, err)
		return nil, err
	}
	if err := chat.RegisterLearningTools(reg); err != nil {
		s.log.Errorf("SendMessage register append_learning failed: session_id=%s err=%v", sessionID, err)
		return nil, err
	}
	if err := chat.RegisterAskUserTools(reg); err != nil {
		s.log.Errorf("SendMessage register ask_user failed: session_id=%s err=%v", sessionID, err)
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
		s.log.Errorf("SendMessage wire catalog/tool_search failed: session_id=%s agent_id=%s err=%v", sessionID, session.AgentID, err)
		return nil, err
	}

	toolFamily := chat.BuildToolFamilyIndex(reg)
	mcpExpand := chat.NewMcpExpandOnMiss(chat.McpExpandOnMissOptions{
		Reg:            reg,
		BoundServers:   mcpServerMetas,
		ActiveFamilies: active,
		ToolFamily:     toolFamily,
		Wiring:         catalogInput,
		Catalog:        catalog,
	})
	// 构建 ReActAgent（含成长工具成功钩子，见 growth_chat.go）
	maxHistory := 20
	a := chat.BuildReActAgent(m, reg, agentMeta.SystemPrompt, maxHistory,
		append(chat.ReActOptionsFromAgent(*agentMeta),
			append(s.growthReActOptions(agentMeta.Workspace),
				chat.EvidenceGateTurnOption(reg, active, userForIntent),
				chat.CodeClaimGateTurnOption(reg, active, m),
				chat.TurnIntentGateOption(active, toolFamily),
			)...)...)

	// 加载历史消息（已包含刚保存的 user 消息）
	history, err := s.chatUC.ListMessages(ctx, sessionID, maxHistory*2)
	if err != nil {
		s.log.Errorf("SendMessage list history failed: session_id=%s err=%v", sessionID, err)
		return nil, err
	}

	// 转换为 agent.Request.Messages（工具目录置顶，其后为技能/数据源等说明）
	effectivePrompt := chat.FormatToolCatalogPrompt(catalog)
	if effectivePrompt != "" {
		effectivePrompt += "\n\n---\n\n"
	}
	effectivePrompt += chat.BuildEffectiveSystemPromptForTurnOnSurface(agentMeta.SystemPrompt, skillsIdx, content, session.AgentID, sessionID, active)
	effectivePrompt = chat.AppendTurnIntentPrompt(effectivePrompt)
	effectivePrompt = chat.AppendCodeAnalysisPromptIf(active, effectivePrompt)
	if chat.ShouldAppendWebToolsPrompt(chat.RuntimeToolsForAgent(agentMeta)) {
		effectivePrompt = chat.AppendWebToolsPrompt(effectivePrompt)
	}
	effectivePrompt = chat.AppendAskUserToolPrompt(effectivePrompt)
	if chat.FamilyActive(active, chat.FamilyData) {
		effectivePrompt = chat.AppendDatasourcePrompt(effectivePrompt, regResult.DatasourcePrompt)
	}
	effectivePrompt = appendWecomBoundSystemPrompt(ctx, s.channelUC, effectivePrompt, agentMeta)
	lock := buildTurnTaskLockFromHistory(content, history)
	effectivePrompt = chat.MaybeApplyTaskLock(effectivePrompt, lock)
	messages := make([]model.Message, 0, len(history)+2)
	if effectivePrompt != "" {
		messages = append(messages, model.Message{Role: "system", Content: effectivePrompt})
	}
	for _, h := range history {
		if h.Role == "system" {
			continue
		}
		if strings.TrimSpace(h.Content) == "" {
			continue
		}
		messages = append(messages, model.Message{Role: h.Role, Content: h.Content})
	}

	// 注入 workspace_root、agent_id、session_id、user_id 供 MemoryStore 工具与 Prefetch 使用
	runCtx := context.WithValue(ctx, tool.ContextKeyWorkspaceRoot, agentMeta.Workspace)
	runCtx = context.WithValue(runCtx, tool.ContextKeyAgentID, session.AgentID)
	runCtx = context.WithValue(runCtx, tool.ContextKeyAgentName, agentMeta.Name)
	runCtx = context.WithValue(runCtx, tool.ContextKeySessionID, sessionID)
	userID := chat.ResolveMemoryUserID(ctx, session)
	if userID != "" {
		runCtx = context.WithValue(runCtx, tool.ContextKeyUserID, userID)
	}
	runCtx = context.WithValue(runCtx, tool.ContextKeyToolCatalog, catalog)
	runCtx = chat.WithDiscoveryExpand(runCtx, mcpExpand)
	if toolSearchActive {
		runCtx = context.WithValue(runCtx, tool.ContextKeyToolSearchActive, true)
	}

	// 调用 Agent（附带记忆预取所需 metadata：session/agent/workspace/user_id/identity）
	resp, err := a.Run(runCtx, &agent.Request{
		Messages: messages,
		Metadata: chat.MaybeMergeTaskLockMetadata(prefetchRequestMetadata(sessionID, session.AgentID, agentMeta.Workspace, userID), lock),
	})
	if err != nil {
		isH, vis, persist, raw := chat.DecomposeGuardrailRunError(err)
		if isH && !raw && vis != "" {
			if persist {
				msg, cerr := s.chatUC.CreateMessage(ctx, sessionID, "assistant", vis)
				if cerr != nil {
					s.log.Errorf("SendMessage save guardrail banner failed: session_id=%s err=%v", sessionID, cerr)
					return nil, cerr
				}
				s.notifyGrowthAssistantTurn(sessionID)
				go chat.NotifyMemorySessionDirty(ctx, sessionID, len(vis), 1, s.chatUC, s.agentUC, chat.NewChatTranscriptProvider(s.chatUC))
				chat.NotifySessionMessageIndexed(ctx, s.chatUC, sessionID, msg)
				return &chatv1.MessageReply{
					Ret:       &common.BaseResponse{Code: 0, Message: "guardrail_halt"},
					Id:        msg.ID,
					SessionId: sessionID,
					Role:      "assistant",
					Content:   vis,
					CreatedAt: msg.CreatedAt.Format(time.RFC3339),
				}, nil
			}
			return &chatv1.MessageReply{
				Ret:       &common.BaseResponse{Code: 0, Message: "guardrail_halt"},
				Id:        "",
				SessionId: sessionID,
				Role:      "assistant",
				Content:   vis,
				CreatedAt: time.Now().UTC().Format(time.RFC3339),
			}, nil
		}
		s.log.Errorf("SendMessage run agent failed: session_id=%s agent_id=%s err=%v", sessionID, session.AgentID, err)
		return nil, err
	}
	tr := chat.RunTraceFromMetadata(resp.Metadata)
	s.persistTurnTrace(runCtx, sessionID, session.AgentID, tr)
	s.persistCompactBoundary(runCtx, sessionID, tr)
	s.afterTurnBackgroundReview(runCtx, sessionID, session.AgentID, agentMeta.Workspace, resp.Messages, tr)

	// 保存 assistant 消息
	msg, err := s.chatUC.CreateMessage(ctx, sessionID, "assistant", resp.Text)
	if err != nil {
		s.log.Errorf("SendMessage save assistant message failed: session_id=%s err=%v", sessionID, err)
		return nil, err
	}
	s.notifyGrowthAssistantTurn(sessionID)
	go chat.NotifyMemorySessionDirty(ctx, sessionID, len(resp.Text), 1, s.chatUC, s.agentUC, chat.NewChatTranscriptProvider(s.chatUC))
	chat.NotifyMemoryExtractFromTurn(ctx, s.memoryStore, session, content, resp.Text, agentMeta)
	chat.NotifyMemoryGraphFromTurn(ctx, s.memoryStore, session, content, resp.Text, agentMeta)
	chat.NotifySessionMessageIndexed(ctx, s.chatUC, sessionID, msg)

	return &chatv1.MessageReply{
		Ret:       &common.BaseResponse{Code: 0, Message: "ok"},
		Id:        msg.ID,
		SessionId: sessionID,
		Role:      "assistant",
		Content:   resp.Text,
		CreatedAt: msg.CreatedAt.Format(time.RFC3339),
	}, nil
}

// SendMessageStream 流式发送消息，返回增量文本 channel；调用方读取完毕后需调用 SaveAssistantMessage 保存。
func (s *ChatService) SendMessageStream(ctx context.Context, req *chatv1.SendMessageRequest) (<-chan ChatStreamEvent, string, error) {
	sessionID := req.GetSessionId()
	content := req.GetContent()
	ir := chat.InputResponseFromContext(ctx)
	cr := chat.ConfirmResponseFromContext(ctx)
	if sessionID == "" || (content == "" && ir == nil && cr == nil) {
		return nil, "", biz.ErrSessionNotFound
	}

	session, err := s.chatUC.GetSession(ctx, sessionID)
	if err != nil {
		s.log.Errorf("SendMessageStream get session failed: session_id=%s err=%v", sessionID, err)
		return nil, "", err
	}
	if session.Readonly {
		return nil, "", biz.ErrSessionReadonly
	}

	agentMeta, err := s.agentUC.GetForSession(ctx, session.AgentID)
	if err != nil {
		s.log.Errorf("SendMessageStream get agent failed: session_id=%s agent_id=%s err=%v", sessionID, session.AgentID, err)
		return nil, "", err
	}

	if cr != nil && cr.Kind == "skill_manage" && cr.Token != "" {
		return s.streamSkillManageConfirm(ctx, sessionID, agentMeta.Workspace, *cr)
	}

	tools, err := s.toolUC.ListByAgentForSession(ctx, session.AgentID)
	if err != nil {
		s.log.Errorf("SendMessageStream list tools failed: session_id=%s agent_id=%s err=%v", sessionID, session.AgentID, err)
		return nil, "", err
	}
	mcpServerMetas, err := s.listMcpServersByAgent(ctx, session.AgentID)
	if err != nil {
		s.log.Errorf("SendMessageStream list mcp servers failed: session_id=%s agent_id=%s err=%v", sessionID, session.AgentID, err)
		return nil, "", err
	}

	m, err := chat.BuildModel(
		agentMeta.ModelConfig.Provider,
		agentMeta.ModelConfig.Model,
		agentMeta.ModelConfig.APIKey,
		agentMeta.ModelConfig.BaseURL,
	)
	if err != nil {
		s.log.Errorf("SendMessageStream build model failed: session_id=%s agent_id=%s provider=%s model=%s err=%v", sessionID, session.AgentID, agentMeta.ModelConfig.Provider, agentMeta.ModelConfig.Model, err)
		return nil, "", err
	}

	userForIntent := chat.UserMessageContentForTurn(content, ir)
	var meaChecks []mea.AcceptanceCheck
	var meaAcceptance []string
	if clean, checks, ok := chat.ParseMEAChecks(userForIntent); ok {
		userForIntent = clean
		meaChecks = checks
	}
	if clean, acceptance, ok := chat.ParseMEAAcceptance(userForIntent); ok {
		userForIntent = clean
		meaAcceptance = acceptance
	}
	active, surfaceRes := chat.PrepareTurnToolSurface(ctx, userForIntent, tools, mcpServerMetas, agentMeta, m)
	s.log.Infof("turn tool surface: session_id=%s source=%s conf=%s active=%v candidates=%v reason=%s",
		sessionID, surfaceRes.Source, surfaceRes.Confidence, surfaceRes.ActiveFamilies, surfaceRes.Candidates, surfaceRes.Reason)
	m, err = chat.ResolveTurnModel(active, m, *agentMeta)
	if err != nil {
		s.log.Errorf("resolve turn model failed: session_id=%s agent_id=%s err=%v", sessionID, session.AgentID, err)
		return nil, "", err
	}

	reg := tool.NewRegistry()
	var mcpServers []toolskill.McpServerEntry
	ch := make(chan ChatStreamEvent, 32)
	// 每个 turn 使用本轮专属的私有 bus，并注入到本轮 agent（见下方 BuildReActAgent）。
	// portal 只订阅这个私有 bus，完全不碰进程级全局 SetDefaultBus，因此并发的多个流
	// 互不干扰、无跨 turn 事件泄漏或丢失：始终映射 model_call；DebugRun 时额外转发原始调试串。
	done := make(chan struct{})
	relay := make(chan ChatStreamEvent, 32)
	var relayWg sync.WaitGroup
	turnBus := events.NewBus()
	epBuf := memory.NewEpisodeLocalBuffer(sessionID)
	memory.AttachFailureSignalBridge(turnBus, memory.MultiFailureSink{
		chat.DefaultFailureSignalSink(),
		memory.EpisodeLocalFailureSink{Buffer: epBuf},
	})
	debugRun := agentMeta.DebugRun
	modelName := agentMeta.ModelConfig.Model
	// 可靠投递语义：使用同步订阅（Subscribe(false, ...)）。框架在发送 StreamEventDone
	// 之前先 emit(ModelResponded)（见 react_agent.go），而 Bus.Publish 会先把所有同步
	// 监听器执行完再继续。因此 ModelResponded 一定在 StreamEventDone 之前落入 relay。
	// 关闭时（见下方 run goroutine 的 defer）先 close(relay) 并把缓冲事件全部 drain 到 ch，
	// 再 close(done)/close(ch)，从而保证最后一个 model_call(responded) 不会在快速/空响应
	// 路径上被丢弃（否则 UI 的 model 节点会停留在 invoked，被 finalizeTimeline 误判为「已中断」）。
	turnBus.Subscribe(false, func(ctx context.Context, e events.Event) {
		if mc := modelCallEventFromBus(e, modelName); mc != nil {
			ev := ChatStreamEvent{Type: ChatStreamEventModelCall, ModelCall: mc}
			select {
			case relay <- ev:
			case <-done:
				return
			}
		}
		if debugRun {
			msg, _ := json.Marshal(e.Payload)
			ev := ChatStreamEvent{Type: ChatStreamEventDebug, Content: string(e.Kind) + "[" + string(msg) + "]\r\n"}
			select {
			case relay <- ev:
			case <-done:
			}
		}
	})
	relayWg.Add(1)
	go func() {
		defer relayWg.Done()
		// drain：range 直到 relay 被 close（在 run goroutine 的 defer 中，且在最后一次 emit
		// 之后），把所有缓冲的 model_call（含最终 ModelResponded）转发到 ch。
		for ev := range relay {
			select {
			case ch <- ev:
			case <-ctx.Done():
				// 客户端/请求已断开：停止转发，但继续 range 把 relay 排空，
				// 避免同步监听器在 relay 满时永久阻塞框架的 emit goroutine。
			}
		}
	}()
	regResult, err := chat.BuildRegistry(tools, mcpServerMetas, reg, chat.RegistryBuildOptions{ActiveFamilies: active})
	if err != nil {
		s.log.Errorf("SendMessageStream build tool registry failed: session_id=%s agent_id=%s err=%v", sessionID, session.AgentID, err)
		return nil, "", err
	}
	mcpServers = regResult.McpServers
	extraSkillDirs, err := s.sharedSkillDirs(ctx, session.AgentID)
	if err != nil {
		return nil, "", err
	}
	skillsIdx, err := chat.BuildSkillsIndex(agentMeta.Workspace, extraSkillDirs)
	if err != nil {
		s.log.Errorf("SendMessageStream build skills index failed: session_id=%s agent_id=%s workspace=%s err=%v", sessionID, session.AgentID, agentMeta.Workspace, err)
		return nil, "", err
	}
	streamSessionProvider := chat.NewChatTranscriptProvider(s.chatUC)
	if err := chat.RegisterAgentRuntimeTools(reg, chat.AgentRuntimeToolsOptions{
		Flags:           chat.HermesP0FlagsPtrForAgent(agentMeta),
		SkillsIdx:       skillsIdx,
		McpServers:      mcpServers,
		AllowScript:     true,
		MemoryStore:     s.memoryStore,
		SessionProvider: streamSessionProvider,
		VisionAnalyzer:  chat.VisionAnalyzerForModel(m),
		RuntimeTools:    agentMeta.RuntimeTools,
		ActiveFamilies:  active,
	}); err != nil {
		s.log.Errorf("SendMessageStream register runtime tools failed: session_id=%s agent_id=%s err=%v", sessionID, session.AgentID, err)
		return nil, "", err
	}
	if err := chat.RegisterLearningTools(reg); err != nil {
		s.log.Errorf("SendMessageStream register append_learning failed: session_id=%s err=%v", sessionID, err)
		return nil, "", err
	}
	if err := chat.RegisterAskUserTools(reg); err != nil {
		s.log.Errorf("SendMessageStream register ask_user failed: session_id=%s err=%v", sessionID, err)
		return nil, "", err
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
		s.log.Errorf("SendMessageStream wire catalog/tool_search failed: session_id=%s agent_id=%s err=%v", sessionID, session.AgentID, err)
		return nil, "", err
	}

	toolFamily := chat.BuildToolFamilyIndex(reg)
	mcpExpand := chat.NewMcpExpandOnMiss(chat.McpExpandOnMissOptions{
		Reg:            reg,
		BoundServers:   mcpServerMetas,
		ActiveFamilies: active,
		ToolFamily:     toolFamily,
		Wiring:         catalogInput,
		Catalog:        catalog,
	})
	maxHistory := 20
	// 注入本轮私有 bus：WithReActEventBus 作为最后一个 extra option 传入，
	// 覆盖 BuildReActAgent 内部默认注入的全局 DefaultBus，使本轮事件只发布到 turnBus。
	a := chat.BuildReActAgent(m, reg, agentMeta.SystemPrompt, maxHistory,
		append(chat.ReActOptionsFromAgent(*agentMeta),
			append(s.growthReActOptions(agentMeta.Workspace),
				chat.EvidenceGateTurnOption(reg, active, userForIntent),
				chat.CodeClaimGateTurnOption(reg, active, m),
				chat.TurnIntentGateOption(active, toolFamily),
				agent.WithReActEventBus(turnBus),
			)...)...)

	history, err := s.chatUC.ListMessages(ctx, sessionID, maxHistory*2)
	if err != nil {
		s.log.Errorf("SendMessageStream list history failed: session_id=%s err=%v", sessionID, err)
		return nil, "", err
	}

	userContent := chat.UserMessageContentForTurn(content, ir)
	if clean, checks, ok := chat.ParseMEAChecks(userContent); ok {
		userContent = clean
		if len(meaChecks) == 0 {
			meaChecks = checks
		}
	}
	if clean, acceptance, ok := chat.ParseMEAAcceptance(userContent); ok {
		userContent = clean
		if len(meaAcceptance) == 0 {
			meaAcceptance = acceptance
		}
	}
	var synthetic []model.Message
	if ir != nil {
		pending, outcome, applyErr := chat.ApplyInputResponse(ctx, sessionID, *ir, chat.AskUserPendingStore(), chat.AskUserFulfillmentStore())
		if applyErr != nil {
			s.log.Errorf("SendMessageStream apply input_response failed: session_id=%s err=%v", sessionID, applyErr)
			return nil, "", applyErr
		}
		synthetic = chat.BuildSyntheticAskUserMessages(pending, outcome)
		userMsg, cerr := s.chatUC.CreateMessage(ctx, sessionID, "user", userContent)
		if cerr != nil {
			s.log.Errorf("SendMessageStream save user message failed: session_id=%s err=%v", sessionID, cerr)
			return nil, "", cerr
		}
		go chat.NotifyMemorySessionDirty(ctx, sessionID, len(userContent), 1, s.chatUC, s.agentUC, streamSessionProvider)
		chat.NotifySessionMessageIndexed(ctx, s.chatUC, sessionID, userMsg)
		history, err = s.chatUC.ListMessages(ctx, sessionID, maxHistory*2)
		if err != nil {
			return nil, "", err
		}
	} else {
		persistContent := strings.TrimSpace(userContent)
		if persistContent == "" && cr != nil {
			persistContent = chat.UserMessagePlaceholderForConfirm(cr.Kind)
			userContent = persistContent
		}
		if persistContent != "" {
			// Normal chat / confirm turns must persist the user row. Non-stream SendMessage
			// already does; stream previously only saved on input_response, so reload showed
			// assistant answer without the question.
			userMsg, cerr := s.chatUC.CreateMessage(ctx, sessionID, "user", persistContent)
			if cerr != nil {
				s.log.Errorf("SendMessageStream save user message failed: session_id=%s err=%v", sessionID, cerr)
				return nil, "", cerr
			}
			go chat.NotifyMemorySessionDirty(ctx, sessionID, len(persistContent), 1, s.chatUC, s.agentUC, streamSessionProvider)
			chat.NotifySessionMessageIndexed(ctx, s.chatUC, sessionID, userMsg)
			history, err = s.chatUC.ListMessages(ctx, sessionID, maxHistory*2)
			if err != nil {
				return nil, "", err
			}
		}
	}

	effectivePrompt := chat.FormatToolCatalogPrompt(catalog)
	if effectivePrompt != "" {
		effectivePrompt += "\n\n---\n\n"
	}
	effectivePrompt += chat.BuildEffectiveSystemPromptForTurnOnSurface(agentMeta.SystemPrompt, skillsIdx, userContent, session.AgentID, sessionID, active)
	effectivePrompt = chat.AppendTurnIntentPrompt(effectivePrompt)
	effectivePrompt = chat.AppendCodeAnalysisPromptIf(active, effectivePrompt)
	if chat.ShouldAppendWebToolsPrompt(chat.RuntimeToolsForAgent(agentMeta)) {
		effectivePrompt = chat.AppendWebToolsPrompt(effectivePrompt)
	}
	effectivePrompt = chat.AppendAskUserToolPrompt(effectivePrompt)
	if chat.FamilyActive(active, chat.FamilyData) {
		effectivePrompt = chat.AppendDatasourcePrompt(effectivePrompt, regResult.DatasourcePrompt)
	}
	effectivePrompt = appendWecomBoundSystemPrompt(ctx, s.channelUC, effectivePrompt, agentMeta)
	lock := buildTurnTaskLockFromHistory(userContent, history)
	effectivePrompt = chat.MaybeApplyTaskLock(effectivePrompt, lock)
	messages := make([]model.Message, 0, len(history)+3)
	if effectivePrompt != "" {
		messages = append(messages, model.Message{Role: "system", Content: effectivePrompt})
	}
	for _, h := range history {
		if h.Role == "system" {
			continue
		}
		if strings.TrimSpace(h.Content) == "" {
			continue
		}
		messages = append(messages, model.Message{Role: h.Role, Content: h.Content})
	}
	if ir != nil {
		messages = chat.InjectSyntheticBeforeLastUser(messages, synthetic)
	}
	// Normal turns: user row is already in history after CreateMessage+reload above.

	runCtx := context.WithValue(ctx, tool.ContextKeyWorkspaceRoot, agentMeta.Workspace)
	runCtx = context.WithValue(runCtx, tool.ContextKeyAgentID, session.AgentID)
	runCtx = context.WithValue(runCtx, tool.ContextKeyAgentName, agentMeta.Name)
	runCtx = context.WithValue(runCtx, tool.ContextKeySessionID, sessionID)
	userID := chat.ResolveMemoryUserID(ctx, session)
	if userID != "" {
		runCtx = context.WithValue(runCtx, tool.ContextKeyUserID, userID)
	}
	runCtx = context.WithValue(runCtx, tool.ContextKeyToolCatalog, catalog)
	runCtx = chat.WithDiscoveryExpand(runCtx, mcpExpand)
	if toolSearchActive {
		runCtx = context.WithValue(runCtx, tool.ContextKeyToolSearchActive, true)
	}
	runCtx = tool.WithSecretProvider(runCtx, chat.AskUserFulfillmentStore())
	go func() {
		defer func() {
			epBuf.Clear()
			// 关闭顺序（保证不丢事件、不 send-on-closed）：
			// 1. 到此处时 `for ev := range evCh` 已经返回——框架的 RunEvents 单 goroutine
			//    在 defer close(out) 之前完成了所有 emit（同步订阅已把事件写入 relay），
			//    因此之后不会再有 Publish/emit 发生，close(relay) 不会造成 send-on-closed。
			// 2. close(relay)：让 drain goroutine range 结束前，把缓冲的 model_call
			//    （含最终 ModelResponded）全部 flush 到 ch。
			// 3. relayWg.Wait()：等 drain 完成，确保所有 relay 事件已进入 ch。
			// 4. close(done)：仅作为同步监听器 guard 的防御信号（此时已无监听器触发）。
			// 5. close(ch)：所有事件已在 ch 中，安全关闭。
			close(relay)
			relayWg.Wait()
			close(done)
			close(ch)
		}()

		req := &agent.Request{
			Messages: messages,
			Metadata: chat.MaybeMergeTaskLockMetadata(prefetchRequestMetadata(sessionID, session.AgentID, agentMeta.Workspace, userID), lock),
		}

		enabled := chat.MEAEnabledForAgent(session.AgentID, agentMeta.RuntimeTools.MEAEnabled)
		g := strings.TrimSpace(lock.Q)
		if g == "" {
			g = userContent
		}
		checks := meaChecks
		if enabled {
			checks = chat.ResolveAcceptanceChecks(meaChecks, len(meaChecks) > 0, g)
		}
		useMEA := chat.ShouldUseMEA(enabled, agentMeta.Workspace, checks, meaAcceptance)
		if useMEA {
			s.streamWithRulesMEA(runCtx, sessionID, session.AgentID, agentMeta.Workspace, g, checks, meaAcceptance, agentMeta.RuntimeTools.MEAEnabled, m, a, messages, req.Metadata, streamSessionProvider, ch)
			return
		}

		if _, err := s.streamAgentEvents(runCtx, sessionID, session.AgentID, agentMeta.Workspace, a, req, streamSessionProvider, ch); err != nil {
			return
		}
	}()
	return ch, sessionID, nil
}

// handleStreamRunError 处理流式运行错误：命中护栏时输出可见横幅（可选持久化+通知），
// 否则记录日志并向 ch 发送 error 事件。供 RunEvents 与非流式回退路径共用。
func (s *ChatService) handleStreamRunError(ctx context.Context, sessionID, agentID string, provider *chat.ChatTranscriptProvider, ch chan<- ChatStreamEvent, err error) {
	isH, vis, persist, raw := chat.DecomposeGuardrailRunError(err)
	if isH && !raw && vis != "" {
		ch <- ChatStreamEvent{Type: ChatStreamEventChunk, Content: vis}
		if persist {
			if gmsg, cerr := s.chatUC.CreateMessage(ctx, sessionID, "assistant", vis); cerr != nil {
				s.log.Errorf("SendMessageStream persist guardrail banner failed: session_id=%s err=%v", sessionID, cerr)
			} else {
				s.notifyGrowthAssistantTurn(sessionID)
				go chat.NotifyMemorySessionDirty(ctx, sessionID, len(vis), 1, s.chatUC, s.agentUC, provider)
				chat.NotifySessionMessageIndexed(ctx, s.chatUC, sessionID, gmsg)
			}
		}
		return
	}
	s.log.Errorf("SendMessageStream run agent failed: session_id=%s agent_id=%s err=%v", sessionID, agentID, err)
	ch <- ChatStreamEvent{Type: ChatStreamEventError, Error: err.Error()}
}

// streamSkillManageConfirm applies a UI confirm_response for skill_manage without involving the LLM.
func (s *ChatService) streamSkillManageConfirm(ctx context.Context, sessionID, workspace string, cr chat.ConfirmResponse) (<-chan ChatStreamEvent, string, error) {
	userContent := chat.UserMessagePlaceholderForConfirm(cr.Kind)
	if _, err := s.chatUC.CreateMessage(ctx, sessionID, "user", userContent); err != nil {
		return nil, "", err
	}
	result, err := chat.ApplySkillManageConfirm(ctx, workspace, sessionID, cr.Token)
	ch := make(chan ChatStreamEvent, 4)
	go func() {
		defer close(ch)
		if err != nil {
			ch <- ChatStreamEvent{Type: ChatStreamEventError, Error: err.Error()}
			ch <- ChatStreamEvent{
				Type: ChatStreamEventConfirmResult,
				ConfirmResult: &ConfirmResultPayload{
					OK:    false,
					Kind:  "skill_manage",
					Token: cr.Token,
					Error: err.Error(),
				},
			}
			return
		}
		var text string
		if ev, has := result["error"]; has && ev != nil && ev != "" {
			text = "技能确认失败: " + toStringAny(ev)
		} else {
			b, _ := json.MarshalIndent(result, "", "  ")
			text = "技能操作已确认并执行:\n" + string(b)
		}
		ch <- ChatStreamEvent{Type: ChatStreamEventChunk, Content: text}
		ch <- ChatStreamEvent{
			Type:          ChatStreamEventConfirmResult,
			ConfirmResult: confirmResultFromSkillManageMap(cr.Token, result),
		}
	}()
	return ch, sessionID, nil
}

func toStringAny(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	b, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	return string(b)
}

// SaveAssistantMessage 保存流式完成后的 assistant 消息（供 SendMessageStream 调用方使用）。
// metadata 可为 nil；非空时写入 chat_messages.metadata（如 timeline）。
func (s *ChatService) SaveAssistantMessage(ctx context.Context, sessionID, content string, metadata map[string]any) (*chatv1.MessageReply, error) {
	msg, err := s.chatUC.CreateMessageWithMetadata(ctx, sessionID, "assistant", content, metadata)
	if err != nil {
		s.log.Errorf("SaveAssistantMessage failed: session_id=%s err=%v", sessionID, err)
		return nil, err
	}
	s.notifyGrowthAssistantTurn(sessionID)
	provider := chat.NewChatTranscriptProvider(s.chatUC)
	go chat.NotifyMemorySessionDirty(ctx, sessionID, len(content), 1, s.chatUC, s.agentUC, provider)
	s.notifyMemoryExtractAfterAssistant(ctx, sessionID, content)
	chat.NotifySessionMessageIndexed(ctx, s.chatUC, sessionID, msg)
	return messageToReply(msg), nil
}

func (s *ChatService) notifyMemoryExtractAfterAssistant(ctx context.Context, sessionID, assistantContent string) {
	session, err := s.chatUC.GetSession(ctx, sessionID)
	if err != nil || session == nil {
		return
	}
	agentMeta, err := s.agentUC.GetForSession(ctx, session.AgentID)
	if err != nil {
		agentMeta = nil
	}
	history, _ := s.chatUC.ListMessages(ctx, sessionID, 50)
	userMsg := chat.LastUserMessageContent(history)
	chat.NotifyMemoryExtractFromTurn(ctx, s.memoryStore, session, userMsg, assistantContent, agentMeta)
	chat.NotifyMemoryGraphFromTurn(ctx, s.memoryStore, session, userMsg, assistantContent, agentMeta)
}

func messageToReply(m *biz.ChatMessage) *chatv1.MessageReply {
	reply := &chatv1.MessageReply{
		Ret:       &common.BaseResponse{Code: 0, Message: "ok"},
		Id:        m.ID,
		SessionId: m.SessionID,
		Role:      m.Role,
		Content:   m.Content,
		CreatedAt: m.CreatedAt.Format(time.RFC3339),
	}
	if len(m.Metadata) > 0 {
		if st, err := structpb.NewStruct(m.Metadata); err == nil {
			reply.Metadata = st
		}
	}
	return reply
}

func buildTurnTaskLockFromHistory(userContent string, history []*biz.ChatMessage) chat.TurnTaskLock {
	msgs := make([]model.Message, 0, len(history))
	for _, h := range history {
		if h == nil {
			continue
		}
		role := strings.ToLower(strings.TrimSpace(h.Role))
		if role != "user" && role != "assistant" {
			continue
		}
		if strings.TrimSpace(h.Content) == "" {
			continue
		}
		msgs = append(msgs, model.Message{Role: h.Role, Content: h.Content})
	}
	return chat.BuildTurnTaskLock(userContent, msgs)
}

func prefetchRequestMetadata(sessionID, agentID, workspace, userID string) map[string]any {
	m := map[string]any{
		"session_id": sessionID,
		"agent_id":   agentID,
		// 默认用 session 作为 identity；上层若有 tenant/user 主体可在后续覆盖该字段。
		"identity": sessionID,
	}
	if workspace != "" {
		m["workspace_root"] = workspace
	}
	if userID != "" {
		m["user_id"] = userID
	}
	return m
}

// ListMessages 获取会话内消息历史（按时间升序）
func (s *ChatService) ListMessages(ctx context.Context, req *chatv1.ListMessagesRequest) (*chatv1.ListMessagesReply, error) {
	items, err := s.chatUC.ListMessages(ctx, req.GetSessionId(), 100)
	if err != nil {
		s.log.Errorf("ListMessages failed: session_id=%s err=%v", req.GetSessionId(), err)
		return nil, err
	}
	replies := make([]*chatv1.MessageReply, len(items))
	for i, m := range items {
		replies[i] = messageToReply(m)
	}
	return &chatv1.ListMessagesReply{
		Ret:   &common.BaseResponse{Code: 0, Message: "ok"},
		Items: replies,
	}, nil
}

func sessionToReply(s *biz.ChatSession) *chatv1.SessionReply {
	reply := &chatv1.SessionReply{
		Ret:       &common.BaseResponse{Code: 0, Message: "ok"},
		Id:        s.ID,
		AgentId:   s.AgentID,
		Title:     s.Title,
		CreatedAt: s.CreatedAt.Format(time.RFC3339),
		UpdatedAt: s.UpdatedAt.Format(time.RFC3339),
	}
	if s.ParentSessionID != "" {
		reply.ParentSessionId = &s.ParentSessionID
	}
	if s.Preview != "" {
		reply.Preview = &s.Preview
	}
	if s.AgentName != "" {
		reply.AgentName = &s.AgentName
	}
	return reply
}
