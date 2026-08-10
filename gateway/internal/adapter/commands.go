package adapter

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/sixath/gateway/internal/command"
	"github.com/sixath/gateway/internal/runtimeclient"
	"github.com/sixath/gateway/internal/session"
)

// runSlashCommand handles inbound slash commands. ok is false when text is not a command.
// On success, reply is a short user-facing message; turns must not be invoked.
func runSlashCommand(ctx context.Context, rt *runtimeclient.Client, sessions *session.Router, channelID, peerID, text string) (reply string, ok bool) {
	cmd, isCmd := command.Parse(text)
	if !isCmd {
		return "", false
	}
	if rt == nil {
		return "运行时未配置", true
	}
	switch cmd.Kind {
	case command.KindAgentList:
		msg, err := formatChannelAgentList(ctx, rt, channelID)
		if err != nil {
			return mapRuntimeUserError(err), true
		}
		return msg, true
	case command.KindAgentSwitch:
		msg, err := switchChannelAgent(ctx, rt, sessions, channelID, peerID, cmd.Target)
		if err != nil {
			return mapRuntimeUserError(err), true
		}
		return msg, true
	case command.KindNew:
		msg, err := newChannelSession(ctx, sessions, channelID, peerID)
		if err != nil {
			return mapRuntimeUserError(err), true
		}
		return msg, true
	case command.KindUnbind:
		if err := rt.DeleteBinding(ctx, channelID, peerID); err != nil {
			return mapRuntimeUserError(err), true
		}
		if sessions != nil {
			sessions.Invalidate(channelID, peerID)
		}
		return "已解除绑定，下一条消息将按默认 Agent 新建会话", true
	default:
		return "未知指令。支持：/agent、/agents、/new、/unbind", true
	}
}

func formatChannelAgentList(ctx context.Context, rt *runtimeclient.Client, channelID string) (string, error) {
	out, err := rt.ListChannelAgents(ctx, channelID)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	def := strings.TrimSpace(out.DefaultAgent)
	if def == "" {
		b.WriteString("可用 Agent：\n")
	} else {
		fmt.Fprintf(&b, "可用 Agent（default: %s）：\n", def)
	}
	if len(out.Agents) == 0 {
		b.WriteString("（空）")
		return b.String(), nil
	}
	for _, a := range out.Agents {
		name := strings.TrimSpace(a.Name)
		id := strings.TrimSpace(a.ID)
		if name == "" || name == id {
			fmt.Fprintf(&b, "- %s\n", id)
		} else {
			fmt.Fprintf(&b, "- %s (%s)\n", name, id)
		}
	}
	return strings.TrimRight(b.String(), "\n"), nil
}

func switchChannelAgent(ctx context.Context, rt *runtimeclient.Client, sessions *session.Router, channelID, peerID, target string) (string, error) {
	list, err := rt.ListChannelAgents(ctx, channelID)
	if err != nil {
		return "", err
	}
	agentID, label, err := matchChannelAgent(list, target)
	if err != nil {
		return "", err
	}
	if sessions == nil {
		return "", fmt.Errorf("session router not configured")
	}
	sessions.Invalidate(channelID, peerID)
	resolved, err := sessions.Resolve(ctx, "", runtimeclient.ResolveRequest{
		ChannelID: channelID,
		PeerID:    peerID,
		AgentID:   agentID,
		ForceNew:  true,
		Reason:    "slash_agent_switch",
	})
	if err != nil {
		return "", err
	}
	sessions.Invalidate(channelID, peerID)
	suffix := shortID(resolved.SessionID)
	if suffix != "" {
		return fmt.Sprintf("已切换到 %s（session …%s）", label, suffix), nil
	}
	return fmt.Sprintf("已切换到 %s", label), nil
}

func newChannelSession(ctx context.Context, sessions *session.Router, channelID, peerID string) (string, error) {
	if sessions == nil {
		return "", fmt.Errorf("session router not configured")
	}
	sessions.Invalidate(channelID, peerID)
	resolved, err := sessions.Resolve(ctx, "", runtimeclient.ResolveRequest{
		ChannelID: channelID,
		PeerID:    peerID,
		ForceNew:  true,
		Reason:    "slash_new",
	})
	if err != nil {
		return "", err
	}
	sessions.Invalidate(channelID, peerID)
	suffix := shortID(resolved.SessionID)
	if suffix != "" {
		return fmt.Sprintf("已开启新会话（…%s）", suffix), nil
	}
	return "已开启新会话", nil
}

func matchChannelAgent(list *runtimeclient.ChannelAgentsReply, target string) (id, label string, err error) {
	target = strings.TrimSpace(target)
	if target == "" {
		return "", "", errAgentNotFound("empty")
	}
	if list == nil {
		return "", "", errAgentNotFound(target)
	}
	for _, a := range list.Agents {
		if a.ID == target {
			return a.ID, agentLabel(a), nil
		}
	}
	for _, a := range list.Agents {
		if strings.EqualFold(strings.TrimSpace(a.Name), target) {
			return a.ID, agentLabel(a), nil
		}
	}
	// Allow matching default_agent id even if list omitted it.
	if def := strings.TrimSpace(list.DefaultAgent); def != "" && (def == target || strings.EqualFold(def, target)) {
		return def, def, nil
	}
	return "", "", errAgentNotFound(target)
}

func agentLabel(a runtimeclient.ChannelAgentItem) string {
	name := strings.TrimSpace(a.Name)
	id := strings.TrimSpace(a.ID)
	if name == "" || name == id {
		return id
	}
	return fmt.Sprintf("%s (%s)", name, id)
}

type agentNotFoundError string

func (e agentNotFoundError) Error() string { return string(e) }

func errAgentNotFound(target string) error {
	return agentNotFoundError(fmt.Sprintf("agent not found: %s", target))
}

func mapRuntimeUserError(err error) string {
	if err == nil {
		return "操作失败，请稍后重试"
	}
	var notFound agentNotFoundError
	if errors.As(err, &notFound) {
		return "Agent 不存在或名称无法解析（仅本渠道白名单）"
	}
	var he *runtimeclient.HTTPError
	if errors.As(err, &he) && he != nil {
		body := string(he.Body)
		switch he.StatusCode {
		case 403:
			return "该 Agent 不在本渠道白名单内"
		case 404:
			if strings.Contains(body, "CHANNEL_NOT_FOUND") {
				return "渠道未在 Portal 配置"
			}
			if strings.Contains(body, "AGENT_NOT_FOUND") {
				return "Agent 不存在或名称无法解析"
			}
			return "资源不存在"
		case 409:
			return "已绑定其它 Agent，请使用 /agent <name> 或 /new"
		}
	}
	return "操作失败，请稍后重试"
}

func shortID(id string) string {
	id = strings.TrimSpace(id)
	if len(id) <= 8 {
		return id
	}
	return id[len(id)-8:]
}
