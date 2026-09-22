package server

import (
	"context"
	"net/http"

	"backend/internal/terminal"

	kratosErrors "github.com/go-kratos/kratos/v2/errors"
	kratoshttp "github.com/go-kratos/kratos/v2/transport/http"
	"github.com/gorilla/websocket"
)

var wsUpgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

type createTerminalSessionRequest struct {
	VMID    int64  `json:"vmid"`
	Port    int    `json:"port"`
	WorkDir string `json:"workdir"`
}

type terminalSessionResponse struct {
	SessionID string `json:"session_id"`
	Host      string `json:"host"`
	Port      int    `json:"port"`
	WorkDir   string `json:"workdir"`
}

// TerminalWSHandler upgrades GET /api/v1/terminal/sessions/{id}/ws to WebSocket.
func TerminalWSHandler(mgr *terminal.Manager) func(kratoshttp.Context) error {
	return func(ctx kratoshttp.Context) error {
		id := ctx.Vars().Get("id")
		if id == "" {
			return kratosErrors.BadRequest("INVALID_ARGUMENT", "session_id is required")
		}
		sess := mgr.Get(id)
		if sess == nil {
			return kratosErrors.NotFound("SESSION_NOT_FOUND", "session not found or expired")
		}

		r := ctx.Request()
		w := ctx.Response()
		if r == nil || w == nil {
			return kratosErrors.InternalServer("UPGRADE_FAILED", "cannot get underlying request/response")
		}

		conn, err := wsUpgrader.Upgrade(w, r, nil)
		if err != nil {
			return err
		}
		defer conn.Close()

		for {
			msgType, msg, err := conn.ReadMessage()
			if err != nil {
				_ = conn.WriteJSON(map[string]string{"type": "closed", "reason": "connection closed"})
				break
			}
			if msgType != websocket.TextMessage {
				continue
			}

			input := string(msg)
			output, execErr := mgr.Execute(id, input)
			if execErr != nil {
				_ = conn.WriteJSON(map[string]string{"type": "error", "message": execErr.Error()})
			} else {
				_ = conn.WriteJSON(map[string]string{"type": "stdout", "data": output})
			}
		}
		return nil
	}
}

// CreateTerminalSessionHandler serves POST /api/v1/terminal/sessions.
func CreateTerminalSessionHandler(mgr *terminal.Manager) func(kratoshttp.Context) error {
	return func(ctx kratoshttp.Context) error {
		var body createTerminalSessionRequest
		if err := ctx.Bind(&body); err != nil {
			return err
		}
		out, err := runWithMiddleware(ctx, func(c context.Context) (any, error) {
			s, err := mgr.Create(body.VMID, body.Port, body.WorkDir)
			if err != nil {
				return nil, err
			}
			return terminalSessionResponse{
				SessionID: s.ID,
				Host:      s.Host,
				Port:      s.Port,
				WorkDir:   s.WorkDir,
			}, nil
		})
		if err != nil {
			return err
		}
		return ctx.JSON(200, out)
	}
}

// GetTerminalSessionHandler serves GET /api/v1/terminal/sessions/{id}.
func GetTerminalSessionHandler(mgr *terminal.Manager) func(kratoshttp.Context) error {
	return func(ctx kratoshttp.Context) error {
		id := ctx.Vars().Get("id")
		out, err := runWithMiddleware(ctx, func(c context.Context) (any, error) {
			s := mgr.Get(id)
			if s == nil {
				return nil, kratosErrors.NotFound("SESSION_NOT_FOUND", "session not found or expired")
			}
			return terminalSessionResponse{
				SessionID: s.ID,
				Host:      s.Host,
				Port:      s.Port,
				WorkDir:   s.WorkDir,
			}, nil
		})
		if err != nil {
			return err
		}
		return ctx.JSON(200, out)
	}
}

// DeleteTerminalSessionHandler serves DELETE /api/v1/terminal/sessions/{id}.
func DeleteTerminalSessionHandler(mgr *terminal.Manager) func(kratoshttp.Context) error {
	return func(ctx kratoshttp.Context) error {
		id := ctx.Vars().Get("id")
		_, err := runWithMiddleware(ctx, func(c context.Context) (any, error) {
			if !mgr.Delete(id) {
				return nil, kratosErrors.NotFound("SESSION_NOT_FOUND", "session not found")
			}
			return map[string]any{"ok": true}, nil
		})
		if err != nil {
			return err
		}
		return ctx.JSON(200, map[string]any{"ok": true})
	}
}