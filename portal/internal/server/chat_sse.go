package server

import (
	"context"
	"net/http"

	chatv1 "backend/api/chat/v1"
	"backend/internal/chatsse"
	portalchat "backend/internal/chat"
	"backend/internal/service"

	"github.com/go-kratos/kratos/v2/log"
	kratoshttp "github.com/go-kratos/kratos/v2/transport/http"
)

// SendMessageSSE 处理流式请求
// 路径 POST /api/v1/sessions/{session_id}/messages/stream 或主路径 + Accept: text/event-stream
// 按 5.5.4 协议：chunk 事件推送增量，done 结束，error 错误
func SendMessageSSE(chat *service.ChatService, logger log.Logger) func(ctx kratoshttp.Context) error {
	l := log.NewHelper(logger)
	return func(ctx kratoshttp.Context) error {
		sessionID := ctx.Vars().Get("session_id")
		if sessionID == "" {
			l.Errorf("SendMessageSSE invalid request: missing session_id")
			return writeSSEError(ctx, "session_id required")
		}

		var body struct {
			Content         string                      `json:"content"`
			InputResponse   *portalchat.InputResponse   `json:"input_response"`
			ConfirmResponse *portalchat.ConfirmResponse `json:"confirm_response"`
		}
		if err := ctx.Bind(&body); err != nil {
			l.Errorf("SendMessageSSE bind request body failed: session_id=%s err=%v", sessionID, err)
			return writeSSEError(ctx, "invalid body")
		}
		if body.Content == "" && body.InputResponse == nil && body.ConfirmResponse == nil {
			l.Errorf("SendMessageSSE invalid request: empty content session_id=%s", sessionID)
			return writeSSEError(ctx, "content or input_response or confirm_response required")
		}

		// Custom Route handlers skip the server middleware chain unless we invoke it
		// explicitly (see runWithMiddleware). Without this, Auth never sets caller_user_id
		// and GetSession/ListByAgent fail with UNAUTHORIZED "caller identity is required".
		var authCtx context.Context
		if _, err := runWithMiddleware(ctx, func(c context.Context) (any, error) {
			authCtx = c
			return nil, nil
		}); err != nil {
			l.Errorf("SendMessageSSE auth middleware failed: session_id=%s err=%v", sessionID, err)
			return writeSSEError(ctx, err.Error())
		}

		// Preserve prior chat SSE behavior: disconnect must not cancel in-flight model/tool work.
		reqCtx := context.WithoutCancel(authCtx)
		if body.InputResponse != nil {
			reqCtx = portalchat.WithInputResponse(reqCtx, body.InputResponse)
		}
		if body.ConfirmResponse != nil {
			reqCtx = portalchat.WithConfirmResponse(reqCtx, body.ConfirmResponse)
		}

		chatReq := &chatv1.SendMessageRequest{SessionId: sessionID, Content: body.Content}

		w := ctx.Response()
		chatsse.SetHeaders(w)

		ch, sessionID, err := chat.SendMessageStream(reqCtx, chatReq)
		if err != nil {
			l.Errorf("SendMessageSSE start stream failed: session_id=%s err=%v", sessionID, err)
			chatsse.WriteEvent(w, "error", map[string]any{"error": err.Error()})
			return nil
		}

		chatsse.WriteStream(context.WithoutCancel(authCtx), w, ch, sessionID, func(c context.Context, sid, content string, meta map[string]any) error {
			_, err := chat.SaveAssistantMessage(c, sid, content, meta)
			return err
		})
		return nil
	}
}

func writeSSEError(ctx kratoshttp.Context, msg string) error {
	w := ctx.Response()
	w.Header().Set("Content-Type", "text/event-stream")
	w.WriteHeader(http.StatusOK)
	chatsse.WriteEvent(w, "error", map[string]any{"error": msg})
	return nil
}
