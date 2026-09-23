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
	// 查看 foo.log 的后 20 行 / last 20 lines of bar.log
	tailLinesRe = regexp.MustCompile(`(?i)(?:查看|看|显示|打开|read|show|view|tail)?\s*[「"'“]?([^\s「」"'“”]+\.(?:log|txt|out|err|gz)?)[」"'”]?\s*(?:的)?\s*(?:后|末尾|最后|last|tail)\s*(\d+)\s*(?:行|lines?)`)
	tailLinesRe2 = regexp.MustCompile(`(?i)(?:后|末尾|最后|last|tail)\s*(\d+)\s*(?:行|lines?).{0,24}?[「"'“]?([^\s「」"'“”]+\.(?:log|txt|out|err))[」"'”]?`)
)

const terminalSystemPrompt = `You are a Windows CMD terminal translator. Translate user requests into Windows CMD commands.
Rules:
- Output ONLY the command to run, nothing else. One line only.
- No markdown, no code fences, no explanations, no apologies.
- Do NOT include "cmd /c" prefix. PowerShell one-liners via powershell -NoProfile -Command "..." are OK when needed.
- Paths may be relative to the current working directory given in the user message.
- To change drive, output only the drive form like D: or D:\ (prefer D:\).
- To enter a subdirectory, output: cd "dirname"
- To view the last N lines of a text/log file, use:
  powershell -NoProfile -Command "Get-Content -Path 'FILE' -Tail N -Encoding UTF8"

Examples:
list files in current directory
→ dir

show network config
→ ipconfig /all

go to C:\Windows and list files
→ cd /d C:\Windows && dir

进入D盘 / switch to D drive
→ D:\

进入 Program Files
→ cd "Program Files"

查看 cgvmagent.log 的后20行 / last 20 lines of cgvmagent.log
→ powershell -NoProfile -Command "Get-Content -Path 'cgvmagent.log' -Tail 20 -Encoding UTF8"

查看当前目录
→ dir

check disk space
→ wmic logicaldisk get size,freespace,caption

create directory C:\temp\logs
→ mkdir C:\temp\logs`

type terminalChatRequest struct {
	AgentID   string `json:"agent_id"`
	VMID      int64  `json:"vmid"`
	Content   string `json:"content"`
	SessionID string `json:"session_id"`
	Cmd       string `json:"cmd,omitempty"` // when set, skip LLM and run this command directly
}

type terminalChatResponse struct {
	SessionID string `json:"session_id"`
	Cmd       string `json:"cmd"`
	Stdout    string `json:"stdout"`
	Stderr    string `json:"stderr,omitempty"`
	WorkDir   string `json:"workdir,omitempty"`
}

// TerminalChatHandler serves POST /api/v1/terminal/chat.
func TerminalChatHandler(mgr *terminal.Manager, agentRepo biz.AgentRepo, toolUC *biz.ToolUsecase) func(kratoshttp.Context) error {
	return func(ctx kratoshttp.Context) error {
		var body terminalChatRequest
		if err := ctx.Bind(&body); err != nil {
			return err
		}
		if body.AgentID == "" || body.VMID <= 0 || (body.Content == "" && body.Cmd == "") {
			return kratosErrors.BadRequest("INVALID_ARGUMENT", "agent_id, vmid, and content or cmd are required")
		}

		out, err := runWithMiddleware(ctx, func(c context.Context) (any, error) {
			cmd := strings.TrimSpace(body.Cmd)
			if cmd == "" {
				// Prefer a cheap local guess for common phrases before calling the model.
				cmd = guessCmdFromNL(body.Content)
			}
			if cmd == "" {
				workdir := "C:\\"
				if body.SessionID != "" {
					if s := mgr.Get(body.SessionID); s != nil && s.WorkDir != "" {
						workdir = s.WorkDir
					}
				}

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

				userPrompt := fmt.Sprintf("Current working directory: %s\n\nUser request:\n%s", workdir, body.Content)
				msgs := []model.Message{
					{Role: "system", Content: terminalSystemPrompt},
					{Role: "user", Content: userPrompt},
				}

				resp, err := ai.Chat(c, msgs)
				if err != nil {
					return nil, fmt.Errorf("model call: %w", err)
				}

				cmd = extractCmd(resp.Text)
				if cmd == "" {
					cmd = guessCmdFromNL(body.Content)
				}
				if cmd == "" {
					return nil, kratosErrors.BadRequest("NO_CMD", "model did not produce a command")
				}
			}

			sessID := body.SessionID
			if sessID == "" || mgr.Get(sessID) == nil {
				host, err := resolveVMHost(c, toolUC, body.AgentID, body.VMID)
				if err != nil {
					return nil, fmt.Errorf("resolve VM host: %w", err)
				}
				s, err := mgr.CreateWithHost(body.VMID, host, 53000, "C:\\")
				if err != nil {
					return nil, err
				}
				sessID = s.ID
			}

			stdout, err := mgr.Execute(sessID, cmd)
			workdir := ""
			if s := mgr.Get(sessID); s != nil {
				workdir = s.WorkDir
			}
			if err != nil {
				return terminalChatResponse{SessionID: sessID, Cmd: cmd, Stderr: err.Error(), WorkDir: workdir}, nil
			}

			return terminalChatResponse{SessionID: sessID, Cmd: cmd, Stdout: stdout, WorkDir: workdir}, nil
		})
		if err != nil {
			return err
		}
		return ctx.JSON(200, out)
	}
}

// resolveVMHost uses the agent's datasource tools to look up a VM's IP address.
func resolveVMHost(ctx context.Context, toolUC *biz.ToolUsecase, agentID string, vmid int64) (string, error) {
	tools, err := toolUC.ListByAgentForSession(ctx, agentID)
	if err != nil {
		return "", fmt.Errorf("list agent tools: %w", err)
	}

	lookup, dsID, err := chat.BuildVMIPLookup(tools)
	if err != nil {
		return "", err
	}

	host, ambiguous, err := lookup(ctx, dsID, vmid)
	if err != nil {
		return "", fmt.Errorf("VM IP lookup via agent datasource: %w", err)
	}
	if ambiguous {
		return "", fmt.Errorf("VM IP lookup returned multiple addresses for vmid %d; specify a datasource", vmid)
	}
	return host, nil
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
	s = strings.TrimSpace(s)
	// Models sometimes add a leading blank line or a short preface; keep the first non-empty line.
	if i := strings.IndexAny(s, "\r\n"); i >= 0 {
		first := strings.TrimSpace(s[:i])
		if first != "" {
			s = first
		}
	}
	return strings.TrimSpace(s)
}

func quotePSPath(name string) string {
	return strings.ReplaceAll(name, "'", "''")
}

// guessCmdFromNL maps a few high-frequency Chinese/English phrases to CMD without calling the model.
func guessCmdFromNL(content string) string {
	s := strings.TrimSpace(content)
	if s == "" {
		return ""
	}
	if m := tailLinesRe.FindStringSubmatch(s); m != nil {
		file, n := m[1], m[2]
		return fmt.Sprintf(`powershell -NoProfile -Command "Get-Content -Path '%s' -Tail %s -Encoding UTF8"`, quotePSPath(file), n)
	}
	if m := tailLinesRe2.FindStringSubmatch(s); m != nil {
		n, file := m[1], m[2]
		return fmt.Sprintf(`powershell -NoProfile -Command "Get-Content -Path '%s' -Tail %s -Encoding UTF8"`, quotePSPath(file), n)
	}
	lower := strings.ToLower(s)
	switch {
	case lower == "dir" || s == "列出当前目录" || s == "查看当前目录" || s == "ls" || s == "列出文件":
		return "dir"
	case s == "pwd" || s == "当前目录" || s == "查看工作目录":
		return "cd"
	}
	return ""
}
