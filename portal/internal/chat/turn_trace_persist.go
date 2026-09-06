package chat

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"backend/internal/biz"

	agent "github.com/sixath/framework/harness"
	"github.com/sixath/framework/config"
	"github.com/sixath/framework/sessionsearch"
	"github.com/sixath/framework/tool"
	"github.com/sixath/framework/turntrace"
)

// TurnTraceIndexOpts controls optional FTS projection of tool calls after a
// successful TurnTrace Upsert. Nil opts skips indexing.
type TurnTraceIndexOpts struct {
	// Manager if non-nil is used for IndexMessage (tests inject a fake).
	// If nil, resolved via GetSessionSearchManager + SessMeta.AgentID.
	Manager sessionsearch.SessionSearchManager
	// SessMeta provides session id / agent_id / title / parent.
	// When ChatUC is set, missing fields are filled from GetSession.
	SessMeta sessionsearch.SessionMeta
	// ChatUC optional: look up session for title/parent/agent_id
	// (same path as NotifySessionMessageIndexed).
	ChatUC *biz.ChatUsecase
}

// PersistTurnTraceIfEnabled builds a sanitized TurnTrace and upserts it.
// After a successful Upsert, when indexOpts is non-nil, each TurnToolCall is
// projected into sessionsearch as role=tool for FTS.
// Failures are logged only — never returned to the caller.
//
// Config: env SATH_TRACE_PERSIST is the portal stand-in for design
// `trace.persist.enabled` (default true). Set to "false" or "0" to disable.
func PersistTurnTraceIfEnabled(ctx context.Context, store turntrace.Store, meta agent.TurnTraceMeta, tr *agent.RunTrace, indexOpts *TurnTraceIndexOpts) {
	if store == nil || tr == nil {
		return
	}
	if !tracePersistEnabled() {
		return
	}
	tt := agent.BuildTurnTrace(meta, tr)
	if tt == nil {
		return
	}
	if err := store.Upsert(ctx, tt); err != nil {
		log.Printf("turn_trace upsert failed: %v", err)
		return
	}
	indexTurnTraceToolProjections(ctx, tt, indexOpts)
}

func indexTurnTraceToolProjections(ctx context.Context, tt *agent.TurnTrace, opts *TurnTraceIndexOpts) {
	if opts == nil || tt == nil || len(tt.Calls) == 0 {
		return
	}
	if !DefaultSessionSearchConfig.Enabled && opts.Manager == nil {
		return
	}

	sessMeta := resolveTurnTraceSessionMeta(ctx, tt, opts)
	mgr := opts.Manager
	if mgr == nil {
		agentID := sessMeta.AgentID
		if agentID == "" {
			agentID = tt.AgentID
		}
		if agentID == "" {
			return
		}
		cfg := config.Config{SessionSearch: DefaultSessionSearchConfig}
		var err error
		mgr, err = sessionsearch.GetSessionSearchManager(cfg, agentID)
		if err != nil || mgr == nil {
			if err != nil {
				log.Printf("turn_trace index skip manager: session_id=%s agent_id=%s err=%v", tt.SessionID, agentID, err)
			}
			return
		}
	}

	createdAt := tt.CreatedAt
	if createdAt.IsZero() {
		createdAt = time.Now().UTC()
	}
	sessionID := tt.SessionID
	if sessionID == "" {
		sessionID = sessMeta.ID
	}
	requestID := tt.RequestID

	for i, call := range tt.Calls {
		docID := toolProjectionDocID(requestID, call.ToolCallID, i)
		doc := sessionsearch.MessageDoc{
			ID:        docID,
			SessionID: sessionID,
			Role:      "tool",
			ToolName:  call.ToolName,
			Content:   formatToolFTSContent(call),
			CreatedAt: createdAt,
		}
		if err := mgr.IndexMessage(ctx, sessMeta, doc); err != nil {
			log.Printf("turn_trace IndexMessage failed: session_id=%s doc_id=%s err=%v", sessionID, docID, err)
		}
	}
}

func resolveTurnTraceSessionMeta(ctx context.Context, tt *agent.TurnTrace, opts *TurnTraceIndexOpts) sessionsearch.SessionMeta {
	meta := opts.SessMeta
	if meta.ID == "" && tt != nil {
		meta.ID = tt.SessionID
	}
	if meta.AgentID == "" && tt != nil {
		meta.AgentID = tt.AgentID
	}
	if opts.ChatUC == nil || meta.ID == "" {
		return meta
	}
	sess, err := opts.ChatUC.GetSession(ctx, meta.ID)
	if err != nil || sess == nil {
		if err != nil {
			log.Printf("turn_trace index skip get session: session_id=%s err=%v", meta.ID, err)
		}
		return meta
	}
	return sessionMetaFromBiz(sess)
}

// toolProjectionDocID builds a stable FTS document id.
// Empty ToolCallID uses a stable index fallback so re-persist stays idempotent.
func toolProjectionDocID(requestID, toolCallID string, index int) string {
	id := toolCallID
	if id == "" {
		id = fmt.Sprintf("%d", index)
	}
	return "trace:" + requestID + ":" + id
}

// formatToolFTSContent builds searchable content from a sanitized TurnToolCall
// (name / err / args / result preview — never raw Result).
func formatToolFTSContent(call agent.TurnToolCall) string {
	var b strings.Builder
	b.WriteString("tool=")
	b.WriteString(call.ToolName)
	if call.Error != "" {
		b.WriteString(" err=")
		b.WriteString(call.Error)
	}
	if len(call.Arguments) > 0 {
		argsJSON, err := json.Marshal(call.Arguments)
		if err == nil {
			b.WriteString(" args=")
			b.Write(argsJSON)
		}
	}
	if call.ResultPreview != "" {
		b.WriteString(" result=")
		b.WriteString(call.ResultPreview)
	}
	return b.String()
}

func tracePersistEnabled() bool {
	v := strings.TrimSpace(strings.ToLower(os.Getenv("SATH_TRACE_PERSIST")))
	return v != "false" && v != "0"
}

// RunTraceFromMetadata extracts *agent.RunTrace from response Metadata["trace"].
func RunTraceFromMetadata(md map[string]any) *agent.RunTrace {
	if md == nil {
		return nil
	}
	tr, _ := md["trace"].(*agent.RunTrace)
	return tr
}

// TurnTraceRequestID prefers RunTrace.RequestID, then context tool.ContextKeyRequestID.
func TurnTraceRequestID(ctx context.Context, tr *agent.RunTrace) string {
	if tr != nil && tr.RequestID != "" {
		return tr.RequestID
	}
	if ctx == nil {
		return ""
	}
	rid, _ := ctx.Value(tool.ContextKeyRequestID).(string)
	return rid
}
