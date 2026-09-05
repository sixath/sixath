package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	"backend/internal/chat"

	agent "github.com/sixath/framework/harness"
	"github.com/sixath/framework/growth"
)

// metadataSessionID 与 prefetchRequestMetadata 中 "session_id" 键一致。
func metadataSessionID(req *agent.Request) string {
	if req == nil || req.Metadata == nil {
		return ""
	}
	v, ok := req.Metadata["session_id"]
	if !ok || v == nil {
		return ""
	}
	switch s := v.(type) {
	case string:
		return s
	default:
		return fmt.Sprint(s)
	}
}

func (s *ChatService) growthReActOptions(workspace string) []agent.ReActOption {
	var opts []agent.ReActOption
	var hooks []agent.ToolHook
	if ws := strings.TrimSpace(workspace); ws != "" {
		if loaded, err := agent.LoadWorkspaceHarnessHooks(ws); err != nil {
			s.log.Warnf("harness hooks load workspace=%s err=%v", ws, err)
		} else {
			hooks = append(hooks, loaded...)
		}
	}
	if chat.FailureCaptureEnabled {
		hooks = append(hooks, agent.NewFailureCaptureHook(agent.FailureCaptureConfig{}))
	}
	if len(hooks) > 0 {
		opts = append(opts, agent.WithReActToolHooks(hooks...))
	}
	return opts
}

func (s *ChatService) growthToolSuccessHook(_ context.Context, req *agent.Request, rec agent.ToolCallRecord) {
	if s.growthUC == nil || rec.Error != "" {
		return
	}
	// C3: counters move to FinalizeTurnForBackgroundReview (sync after Run); skip async Wake path.
	if s.growthUC.BackgroundReviewEnabled() {
		return
	}
	if growth.IsGrowthReviewMetadata(req.Metadata) || growth.ShouldSkipGrowthReview(req.Metadata) {
		return
	}
	sid := metadataSessionID(req)
	if sid == "" {
		return
	}
	s.runGrowthAsync(func(bg context.Context) error {
		return s.growthUC.OnToolSuccess(bg, sid)
	}, "OnToolSuccess", sid)
}

func (s *ChatService) notifyGrowthAssistantTurn(sessionID string) {
	if s.growthUC == nil || sessionID == "" {
		return
	}
	// C3: assistant turn counted in FinalizeTurnForBackgroundReview; skip async path.
	if s.growthUC.BackgroundReviewEnabled() {
		return
	}
	// Scheme A (G2): assistant 落库只做阈值计数；TrySessionEnd* 挂在 ChatSessionHooks / DeleteSession。
	s.runGrowthAsync(func(bg context.Context) error {
		return s.growthUC.OnAssistantTurn(bg, sessionID)
	}, "OnAssistantTurn", sessionID)
}

// registerGrowthSessionHooks 在 ChatSession 结束（DeleteSession 成功后）触发 C2/C2s 轻量复盘 pending。
func (s *ChatService) registerGrowthSessionHooks() {
	if s == nil || s.growthUC == nil || s.sessionHooks == nil {
		return
	}
	uc := s.growthUC
	s.sessionHooks.Register(agent.ChatSessionHookFunc(func(ctx context.Context, sessionID string) error {
		if err := uc.TrySessionEndMemoryReview(ctx, sessionID); err != nil {
			return err
		}
		return uc.TrySessionEndSkillReview(ctx, sessionID)
	}))
}

func (s *ChatService) runGrowthAsync(fn func(context.Context) error, op, sessionID string) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := fn(ctx); err != nil {
			s.log.Warnf("growth %s session_id=%s err=%v", op, sessionID, err)
		}
	}()
}
