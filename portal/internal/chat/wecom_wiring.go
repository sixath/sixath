package chat

import (
	"context"
	"fmt"
	"sync"
	"time"

	"backend/internal/channel"

	"github.com/sixath/framework/tool"
)

const sendToWeComDefaultInterval = time.Second

// sendToWeComLastSend 按 session_id 记录上次发送时间，进程内限流。
// 多副本场景下无法跨进程生效——如需全局限流，后续在 Recorder 侧接入 Redis 计数。
var sendToWeComLastSend sync.Map

// SendToWeComOptions 构造 send_to_wecom 工具的依赖。
type SendToWeComOptions struct {
	// ResolveOutbound 返回绑定的出站渠道（wecom / wxpusher / ...）。优先于 ResolveWebhook。
	ResolveOutbound func(ctx context.Context) (channel.OutboundChannel, error)
	// ResolveWebhook 兼容旧路径：返回 wecom webhook URL，内部包装为 WeComOutbound。
	ResolveWebhook func(ctx context.Context) (string, error)
	// Recorder 可选：非空时每次投递（成功或失败）都会落一条 channel_deliveries 记录。
	Recorder channel.DeliveryRecorder
	// ChannelID 用于投递记录归属；仅当 Recorder 非空时才有意义。
	ChannelID string
	// RateLimitInterval 同一会话发送的最小间隔，<=0 默认 1s。
	RateLimitInterval time.Duration
}

// RegisterSendToWeComTool 向注册表注册 send_to_wecom（兼容别名）。
// 该工具按 Agent/Skill 绑定选择渠道（wecom / wxpusher / 未来渠道），
// 名称保留 send_to_wecom 以避免破坏既有 Agent 配置。
// reg 为空、且 ResolveOutbound 与 ResolveWebhook 均为 nil 时不注册。
func RegisterSendToWeComTool(reg *tool.Registry, opts SendToWeComOptions) error {
	resolve := opts.ResolveOutbound
	if resolve == nil && opts.ResolveWebhook != nil {
		wh := opts.ResolveWebhook
		resolve = func(ctx context.Context) (channel.OutboundChannel, error) {
			url, err := wh(ctx)
			if err != nil {
				return nil, err
			}
			return channel.NewWeComOutbound(url), nil
		}
	}
	if reg == nil || resolve == nil {
		return nil
	}
	recorder := opts.Recorder
	channelID := opts.ChannelID
	interval := opts.RateLimitInterval
	if interval <= 0 {
		interval = sendToWeComDefaultInterval
	}
	return reg.Register(tool.Tool{
		Name: "send_to_wecom",
		Description: "Push a message to the agent's bound outbound channel (WeCom group webhook or WxPusher). " +
			"Use when the user asks to notify the group, or when a conclusion should be shared with the team. " +
			"将消息推送到 Agent 绑定的出站渠道（企业微信群机器人或 WxPusher）；用户要求通知群聊或需要把结论同步到团队时使用。",
		Toolset: tool.ToolsetCore,
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"content": map[string]any{
					"type":        "string",
					"description": "Message body to send.",
				},
				"msg_type": map[string]any{
					"type":        "string",
					"enum":        []string{"text", "markdown"},
					"description": "WeCom message type (ignored by other channels). Default: text.",
				},
			},
			"required": []string{"content"},
		},
		Execute: buildSendToWeComExecute(resolve, recorder, channelID, interval),
	})
}

func buildSendToWeComExecute(resolve func(context.Context) (channel.OutboundChannel, error), recorder channel.DeliveryRecorder, channelID string, interval time.Duration) tool.ExecuteFunc {
	return func(ctx context.Context, params map[string]any) (any, error) {
		content, _ := params["content"].(string)
		if content == "" {
			return "send_to_wecom: content is required", nil
		}
		msgType, _ := params["msg_type"].(string)
		if msgType == "" {
			msgType = "text"
		}
		if msgType != "text" && msgType != "markdown" {
			msgType = "text"
		}

		sessionID, _ := ctx.Value(tool.ContextKeySessionID).(string)
		if err := checkSendToWeComRateLimit(sessionID, interval); err != nil {
			return err.Error(), nil
		}

		out, err := resolve(ctx)
		if err != nil {
			return fmt.Sprintf("send_to_wecom: resolve channel: %v", err), nil
		}
		d, err := channel.Deliver(ctx, out, channel.OutboundMessage{Content: content, MsgType: msgType}, channelID, sessionID, channel.DeliverOptions{}, recorder)
		if err != nil {
			return fmt.Sprintf("send_to_wecom: 投递失败（delivery_id: %s）: %v", d.ID, err), nil
		}
		return fmt.Sprintf("%s（delivery_id: %s）", channelSuccessLabel(out.Type()), d.ID), nil
	}
}

// channelSuccessLabel 按渠道类型返回成功提示文案。
func channelSuccessLabel(typ string) string {
	switch typ {
	case "wecom":
		return "已投递到企业微信群"
	case "wxpusher":
		return "已投递到 WxPusher"
	default:
		return "已投递"
	}
}

func checkSendToWeComRateLimit(sessionID string, interval time.Duration) error {
	if interval <= 0 {
		interval = sendToWeComDefaultInterval
	}
	now := time.Now()
	if v, ok := sendToWeComLastSend.Load(sessionID); ok {
		if last, ok := v.(time.Time); ok && now.Sub(last) < interval {
			return fmt.Errorf("发送过于频繁，请至少间隔 %s 后再试", interval)
		}
	}
	sendToWeComLastSend.Store(sessionID, now)
	return nil
}
