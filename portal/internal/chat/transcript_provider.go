package chat

import (
	"context"
	"fmt"
	"strings"

	"backend/internal/biz"

	"github.com/sixath/framework/config"
	"github.com/sixath/framework/memorysearch"
)

// ChatTranscriptProvider 实现 memorysearch.SessionTranscriptProvider，基于 ChatUsecase 提供会话转录。
type ChatTranscriptProvider struct {
	chatUC *biz.ChatUsecase
}

// NewChatTranscriptProvider 创建会话转录提供者。
func NewChatTranscriptProvider(chatUC *biz.ChatUsecase) *ChatTranscriptProvider {
	return &ChatTranscriptProvider{chatUC: chatUC}
}

// ListSessionsForAgent 返回该 Agent 下需索引的会话 ID 列表。
func (p *ChatTranscriptProvider) ListSessionsForAgent(ctx context.Context, agentID string) ([]string, error) {
	sessions, _, err := p.chatUC.ListSessions(ctx, agentID, "", 1, 500, false)
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(sessions))
	for _, s := range sessions {
		ids = append(ids, s.ID)
	}
	return ids, nil
}

// GetTranscript 返回会话的 Markdown 转录内容。
func (p *ChatTranscriptProvider) GetTranscript(ctx context.Context, sessionID string) (string, error) {
	msgs, err := p.chatUC.ListMessages(ctx, sessionID, 500)
	if err != nil {
		return "", err
	}
	var buf strings.Builder
	buf.WriteString("# Session Transcript\n\n")
	for _, m := range msgs {
		role := m.Role
		if role == "" {
			role = "unknown"
		}
		buf.WriteString(fmt.Sprintf("## %s\n\n%s\n\n", role, m.Content))
	}
	return buf.String(), nil
}

// Ensure ChatTranscriptProvider implements memorysearch.SessionTranscriptProvider.
var _ memorysearch.SessionTranscriptProvider = (*ChatTranscriptProvider)(nil)

// AgentGetter 用于获取 Agent 信息（避免 chat 包依赖 service）。
type AgentGetter interface {
	Get(ctx context.Context, id string) (*biz.AgentMeta, error)
}

// NotifyMemorySessionDirty 在会话消息更新后调用，触发 session-delta 同步。
// 调用方可同步或异步调用；内部用 DetachCallerContext，避免请求取消/丢 caller 后 GetSession 失败。
func NotifyMemorySessionDirty(ctx context.Context, sessionID string, bytesDelta, messagesDelta int, chatUC *biz.ChatUsecase, agentGetter AgentGetter, provider memorysearch.SessionTranscriptProvider) {
	if provider == nil || chatUC == nil {
		return
	}
	bg := biz.DetachCallerContext(ctx)
	session, err := chatUC.GetSession(bg, sessionID)
	if err != nil {
		return
	}
	agentMeta, err := agentGetter.Get(bg, session.AgentID)
	if err != nil {
		return
	}
	cfg := config.Config{Memory: DefaultMemoryConfig}
	mgr, err := memorysearch.GetMemorySearchManager(cfg, session.AgentID, agentMeta.Workspace, nil, provider)
	if err != nil || mgr == nil {
		return
	}
	if n, ok := mgr.(memorysearch.SessionDirtyNotifier); ok {
		n.NotifySessionDirty(sessionID, bytesDelta, messagesDelta)
	}
}
