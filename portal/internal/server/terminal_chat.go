package server

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"backend/internal/biz"
	"backend/internal/chat"
	"backend/internal/terminal"

	kratosErrors "github.com/go-kratos/kratos/v2/errors"
	kratoshttp "github.com/go-kratos/kratos/v2/transport/http"
	"github.com/sixath/framework/model"
)

var (
	codeFenceRe = regexp.MustCompile(`(?s)^\x60\x60\x60(?:cmd|powershell|bat|dos|shell|sh|bash)?\s*\n?(.*?)\n?\x60\x60\x60$`)
	cmdIntroRe  = regexp.MustCompile(`(?i)^(here is the command|the command is|运行以下命令|执行以下命令|command:|cmd:)[：:\s]*`)
)

const terminalSystemPrompt = `You are a Windows CMD terminal translator. Translate user requests into Windows CMD commands.
Rules:
- Output ONLY the command to run, nothing else.
- No markdown, no code fences, no explanations.
- Do NOT include "cmd" or "powershell" prefixes.
- Use full paths when the user mentions specific locations.

Examples:
list files in current directory
→ dir

show network config
→ ipconfig /all

go to C:\Windows and list files
→ cd C:\Windows && dir

check disk space
→ wmic logicaldisk get size,freespace,caption

create directory C:\temp\logs
→ mkdir C:\temp\logs`

type terminalChatRequest struct {
	AgentID   string `json:"agent_id"`
	VMID      int64  `json:"vmid"`
	Content   string `json:"content"`
	SessionID string `json:"session_id"`
}

type terminalChatResponse struct {
	SessionID string `json:"session_id"`
	Cmd       string `json:"cmd"`
	Stdout    string `json:"stdout"`
	Stderr    string `json:"stderr,omitempty"`
}

// TerminalChatHandler serves POST /api/v1/terminal/chat.
func TerminalChatHandler(mgr *terminal.Manager, agentRepo biz.AgentRepo) func(kratoshttp.Context) error {
	return func(ctx kratoshttp.Context) error {
		var body terminalChatRequest
		if err := ctx.Bind(&body); err != nil {
			return err
		}
		if body.AgentID == "" || body.VMID <= 0 || body.Content == "" {
			return kratosErrors.BadRequest("INVALID_ARGUMENT", "agent_id, vmid, content are required")
		}

		out, err := runWithMiddleware(ctx, func(c context.Context) (any, error) {
			agentMeta, err := agentRepo.GetByID(c, body.AgentID)
			if err != nil {
				return nil, err
			}

			ai, err := chat.BuildModel(
				agentMeta.ModelConfig.Provider,
				agentMeta.ModelConfig.Model,
				agentMeta.ModelConfig.APIKey,
				agentMeta.ModelConfig.BaseURL,
			)
			if err != nil {
				return nil, err
			}

			msgs := []model.Message{
				{Role: "system", Content: terminalSystemPrompt},
				{Role: "user", Content: body.Content},
			}

			resp, err := ai.Chat(c, msgs)
			if err != nil {
				return nil, fmt.Errorf("model call: %w", err)
			}

			cmd := extractCmd(resp.Text)
			if cmd == "" {
				return nil, kratosErrors.BadRequest("NO_CMD", "model did not produce a command")
			}

			sessID := body.SessionID
			if sessID == "" || mgr.Get(sessID) == nil {
				s, err := mgr.Create(body.VMID, 53000, "C:\\")
				if err != nil {
					return nil, err
				}
				sessID = s.ID
			}

			stdout, err := mgr.Execute(sessID, cmd)
			if err != nil {
				return terminalChatResponse{SessionID: sessID, Cmd: cmd, Stderr: err.Error()}, nil
			}

			return terminalChatResponse{SessionID: sessID, Cmd: cmd, Stdout: stdout}, nil
		})
		if err != nil {
			return err
		}
		return ctx.JSON(200, out)
	}
}

func extractCmd(raw string) string {
	s := strings.TrimSpace(raw)
	if s == "" {
		return ""
	}
	if m := codeFenceRe.FindStringSubmatch(s); m != nil {
		s = strings.TrimSpace(m[1])
	}
	s = cmdIntroRe.ReplaceAllString(s, "")
	return strings.TrimSpace(s)
}