package service

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"backend/internal/chat"
	"backend/internal/growthwake"

	agent "github.com/sixath/framework/harness"
	"github.com/sixath/framework/growth"
	"github.com/sixath/framework/model"
	"github.com/sixath/framework/skills"
	"github.com/sixath/framework/tool"
)

const defaultMaxSnapshotMessages = 80

// BackgroundReviewParams drives an in-process C3 fork after FinalizeTurn.
type BackgroundReviewParams struct {
	SessionID   string
	AgentID     string
	RequestID   string
	Workspace   string
	Messages    []model.Message
	SpawnSkill  bool
	SpawnMemory bool
}

// BackgroundReviewer runs the C3 in-process fork (GrowthWorker implements this).
type BackgroundReviewer interface {
	SpawnBackgroundReview(ctx context.Context, p BackgroundReviewParams) error
}

// SetBackgroundReviewer wires the C3 spawner (typically GrowthWorker) onto ChatService.
func (s *ChatService) SetBackgroundReviewer(r BackgroundReviewer) {
	if s == nil {
		return
	}
	s.bgReviewer = r
}

// afterTurnBackgroundReview runs after PersistTurnTrace: FinalizeTurn + optional async spawn.
// Failures are logged only — never returned to the chat caller.
func (s *ChatService) afterTurnBackgroundReview(ctx context.Context, sessionID, agentID, workspace string, messages []model.Message, tr *agent.RunTrace) {
	if s == nil || s.growthUC == nil || !s.growthUC.BackgroundReviewEnabled() || sessionID == "" {
		return
	}
	requestID := chat.TurnTraceRequestID(ctx, tr)
	toolOK := countSuccessfulTools(tr)
	spawnSkill, spawnMemory, err := s.growthUC.FinalizeTurnForBackgroundReview(ctx, sessionID, requestID, toolOK, true)
	if err != nil {
		s.log.Warnf("growth FinalizeTurn session_id=%s err=%v", sessionID, err)
		return
	}
	if !spawnSkill && !spawnMemory {
		return
	}

	snapshot := messages
	if len(snapshot) == 0 {
		// Fallback: recent chat user/assistant + synthetic role=tool from this RunTrace.
		// Not bit-identical to model context, but unblocks review when Task 9 snapshot is missing.
		snapshot = s.rebuildReviewMessagesFallback(ctx, sessionID, tr)
	}
	snapshot = truncateSnapshotMessages(snapshot, maxSnapshotMessages(), preferFailedTools())

	if err := s.growthUC.SetBgReviewInFlight(ctx, sessionID, true); err != nil {
		s.log.Warnf("growth SetBgReviewInFlight session_id=%s err=%v", sessionID, err)
		growthwake.Wake()
		return
	}

	params := BackgroundReviewParams{
		SessionID:   sessionID,
		AgentID:     agentID,
		RequestID:   requestID,
		Workspace:   workspace,
		Messages:    snapshot,
		SpawnSkill:  spawnSkill,
		SpawnMemory: spawnMemory,
	}

	spawnFn := s.spawnBackgroundReviewOnce
	if s.bgReviewSpawnHook != nil {
		spawnFn = s.bgReviewSpawnHook
	}
	go spawnFn(params)
}

// spawnBackgroundReviewOnce invokes the wired BackgroundReviewer and clears in_flight.
// On success: ClearGrowthPending + MarkBackgroundReviewSuccess.
// On failure: Wake() so the async worker can retry with TraceDigest.
func (s *ChatService) spawnBackgroundReviewOnce(p BackgroundReviewParams) {
	bg := context.Background()
	defer func() {
		// MarkBackgroundReviewSuccess already clears in_flight; this covers failure / early return.
		if s.growthUC == nil {
			return
		}
		st, err := s.growthUC.GetState(bg, p.SessionID)
		if err != nil || st == nil || !st.BgReviewInFlight {
			return
		}
		if err := s.growthUC.SetBgReviewInFlight(bg, p.SessionID, false); err != nil && s.log != nil {
			s.log.Warnf("growth clear BgReviewInFlight session_id=%s err=%v", p.SessionID, err)
		}
	}()

	reviewer := s.bgReviewer
	if reviewer == nil {
		if s.log != nil {
			s.log.Warnf("growth background review: no reviewer wired session_id=%s; Wake for worker fallback", p.SessionID)
		}
		growthwake.Wake()
		return
	}

	err := reviewer.SpawnBackgroundReview(bg, p)
	if err != nil {
		if s.log != nil {
			s.log.Warnf("growth SpawnBackgroundReview session_id=%s err=%v", p.SessionID, err)
		}
		growthwake.Wake()
		return
	}
	if clearErr := s.growthUC.ClearGrowthPending(bg, p.SessionID, p.SpawnSkill, p.SpawnMemory); clearErr != nil && s.log != nil {
		s.log.Warnf("growth ClearGrowthPending after bg review session_id=%s err=%v", p.SessionID, clearErr)
	}
	if markErr := s.growthUC.MarkBackgroundReviewSuccess(bg, p.SessionID, p.RequestID); markErr != nil && s.log != nil {
		s.log.Warnf("growth MarkBackgroundReviewSuccess session_id=%s err=%v", p.SessionID, markErr)
	}
}

func countSuccessfulTools(tr *agent.RunTrace) int {
	if tr == nil {
		return 0
	}
	n := 0
	for _, c := range tr.ToolCalls {
		if c.Error == "" {
			n++
		}
	}
	return n
}

func maxSnapshotMessages() int {
	v := strings.TrimSpace(os.Getenv("SATH_BACKGROUND_REVIEW_MAX_SNAPSHOT"))
	if v == "" {
		return defaultMaxSnapshotMessages
	}
	n, err := strconv.Atoi(v)
	if err != nil || n <= 0 {
		return defaultMaxSnapshotMessages
	}
	return n
}

func preferFailedTools() bool {
	v := strings.TrimSpace(os.Getenv("SATH_BACKGROUND_REVIEW_PREFER_FAILED"))
	if v == "" {
		return true
	}
	switch strings.ToLower(v) {
	case "0", "false", "no", "off":
		return false
	default:
		return true
	}
}

// truncateSnapshotMessages caps the snapshot. When preferFailed, failed tool messages are
// kept preferentially; remaining budget is filled with the most recent other messages
// while preserving chronological order.
func truncateSnapshotMessages(msgs []model.Message, max int, preferFailed bool) []model.Message {
	if max <= 0 {
		max = defaultMaxSnapshotMessages
	}
	if len(msgs) <= max {
		return msgs
	}
	if !preferFailed {
		return msgs[len(msgs)-max:]
	}

	failedIdx := make(map[int]struct{})
	for i, m := range msgs {
		if isFailedToolMessage(m) {
			failedIdx[i] = struct{}{}
		}
	}
	keep := make([]bool, len(msgs))
	kept := 0
	for i := range msgs {
		if _, ok := failedIdx[i]; ok {
			keep[i] = true
			kept++
		}
	}
	for i := len(msgs) - 1; i >= 0 && kept < max; i-- {
		if keep[i] {
			continue
		}
		keep[i] = true
		kept++
	}
	if kept > max {
		kept = 0
		for i := range keep {
			keep[i] = false
		}
		for i := len(msgs) - 1; i >= 0 && kept < max; i-- {
			if _, ok := failedIdx[i]; ok {
				keep[i] = true
				kept++
			}
		}
		for i := len(msgs) - 1; i >= 0 && kept < max; i-- {
			if keep[i] {
				continue
			}
			keep[i] = true
			kept++
		}
	}
	out := make([]model.Message, 0, max)
	for i, m := range msgs {
		if keep[i] {
			out = append(out, m)
		}
	}
	return out
}

func isFailedToolMessage(m model.Message) bool {
	if m.Role != "tool" {
		return false
	}
	if m.Metadata != nil {
		if errStr, ok := m.Metadata["error"].(string); ok && errStr != "" {
			return true
		}
		if blocked, ok := m.Metadata["blocked"].(bool); ok && blocked {
			return true
		}
	}
	return strings.Contains(m.Content, `"error"`)
}

// rebuildReviewMessagesFallback builds a review-only message list from recent chat
// user/assistant rows plus synthetic role=tool messages from the just-finished RunTrace.
// Documented limitation: not bit-identical to the in-memory model context (no assistant
// tool_calls metadata, args/results are previews), but unblocks C3 when Messages is empty.
func (s *ChatService) rebuildReviewMessagesFallback(ctx context.Context, sessionID string, tr *agent.RunTrace) []model.Message {
	var out []model.Message
	if s.chatUC != nil && sessionID != "" {
		history, err := s.chatUC.ListMessages(ctx, sessionID, 40)
		if err == nil {
			for _, h := range history {
				if h == nil {
					continue
				}
				if h.Role != "user" && h.Role != "assistant" {
					continue
				}
				out = append(out, model.Message{Role: h.Role, Content: h.Content})
			}
		}
	}
	out = append(out, syntheticToolMessagesFromTrace(tr)...)
	return out
}

func syntheticToolMessagesFromTrace(tr *agent.RunTrace) []model.Message {
	if tr == nil || len(tr.ToolCalls) == 0 {
		return nil
	}
	tt := agent.BuildTurnTrace(agent.TurnTraceMeta{RequestID: tr.RequestID}, tr)
	if tt == nil {
		return nil
	}
	out := make([]model.Message, 0, len(tt.Calls))
	for _, c := range tt.Calls {
		payload := map[string]any{
			"tool":   c.ToolName,
			"result": c.ResultPreview,
		}
		if len(c.Arguments) > 0 {
			payload["arguments"] = c.Arguments
		}
		if c.Error != "" {
			payload["error"] = c.Error
		}
		if c.Blocked {
			payload["blocked"] = true
		}
		b, err := json.Marshal(payload)
		content := c.ResultPreview
		if err == nil {
			content = string(b)
		}
		meta := map[string]any{
			"tool_name":    c.ToolName,
			"tool_call_id": c.ToolCallID,
		}
		if c.Error != "" {
			meta["error"] = c.Error
		}
		if c.Blocked {
			meta["blocked"] = true
		}
		out = append(out, model.Message{
			Role:     "tool",
			Content:  content,
			Metadata: meta,
		})
	}
	return out
}

// SpawnBackgroundReview implements BackgroundReviewer: fork a lean review agent with
// an optional messages snapshot (no growth success hooks / nudge on the child).
// ClearGrowthPending is owned by ChatService.spawnBackgroundReviewOnce on success.
func (w *GrowthWorker) SpawnBackgroundReview(ctx context.Context, p BackgroundReviewParams) error {
	if w == nil {
		return fmt.Errorf("growth: background review worker nil")
	}
	workspace := strings.TrimSpace(p.Workspace)
	if workspace == "" {
		return fmt.Errorf("growth: background review requires workspace")
	}

	if p.SpawnMemory && w.memoryNotify != nil && !p.SpawnSkill {
		w.memoryNotify(ctx, p.SessionID)
		return nil
	}

	if !p.SpawnSkill {
		return nil
	}

	job := growth.ReviewJob{
		SessionID:     p.SessionID,
		WorkspaceKey:  workspace,
		WorkspaceRoot: workspace,
		PendingSkill:  p.SpawnSkill,
		PendingMemory: p.SpawnMemory,
	}
	if err := w.runForkReviewAgent(ctx, job, p.Messages, "", ""); err != nil {
		return err
	}
	if p.SpawnMemory && w.memoryNotify != nil {
		w.memoryNotify(ctx, p.SessionID)
	}
	return nil
}

// runForkReviewAgent is the shared fork used by worker spawnReviewAgent and C3 SpawnBackgroundReview.
// When messages is non-empty it is used as conversation history (skills summary prepended as user section).
// When messages is empty, falls back to transcript+summary single user message (legacy worker path).
// Child agent: MetaGrowthReview set — no growth success hooks.
func (w *GrowthWorker) runForkReviewAgent(ctx context.Context, job growth.ReviewJob, messages []model.Message, transcript, summary string) error {
	ctx = w.internalContext(ctx)
	workspace := job.WorkspaceRoot
	if workspace == "" {
		workspace = job.WorkspaceKey
	}

	m := w.reviewModel
	if m == nil {
		return errNoReviewModel
	}

	beforeNames, err := listWorkspaceSkillNames(workspace)
	if err != nil {
		return fmt.Errorf("growth: list skills before fork-agent review: %w", err)
	}

	reg, err := w.buildReviewRegistry(workspace)
	if err != nil {
		return err
	}

	maxSteps := 12
	if w.growthCfg != nil {
		if n := int(w.growthCfg.GetAgentReviewMaxSteps()); n > 0 {
			maxSteps = n
		}
	}
	// Child: no growth tool-success hooks / nudge — only lean ReAct + skillops registry.
	a := agent.NewReActAgent(m, nil, reg, agent.WithReActMaxSteps(maxSteps))

	timeout := 120 * time.Second
	if w.growthCfg != nil {
		if d := w.growthCfg.GetAgentReviewTimeout().AsDuration(); d > 0 {
			timeout = d
		}
	}
	rctx := context.WithValue(ctx, tool.ContextKeyWorkspaceRoot, workspace)
	rctx, cancel := context.WithTimeout(rctx, timeout)
	defer cancel()

	if summary == "" && workspace != "" {
		summary = skillsIndexSummary(workspace)
	}

	var reqMessages []model.Message
	if len(messages) > 0 {
		reqMessages = make([]model.Message, 0, len(messages)+1)
		if summary != "" {
			reqMessages = append(reqMessages, model.Message{
				Role:    "user",
				Content: "# Skills index snapshot\n" + summary,
			})
		}
		reqMessages = append(reqMessages, messages...)
	} else {
		reqMessages = []model.Message{{
			Role:    "user",
			Content: "# Skills index snapshot\n" + summary + "\n\n# Transcript\n" + transcript,
		}}
	}

	req := &agent.Request{
		SystemPrompt: agentReviewSystemPrompt,
		Messages:     reqMessages,
		Metadata:     growth.MergeReviewMetadata(map[string]any{"session_id": job.SessionID}),
	}
	if _, err := a.Run(rctx, req); err != nil {
		return err
	}

	return w.rewriteCronAfterForkReview(ctx, workspace, beforeNames)
}

func skillsIndexSummary(workspace string) string {
	skillsDir := workspace + "/skills"
	idx, err := skills.NewIndex([]string{skillsDir}, nil, nil)
	if err != nil || idx == nil {
		return ""
	}
	return growth.FormatSkillsIndexSnapshot(idx, 64, 200)
}
