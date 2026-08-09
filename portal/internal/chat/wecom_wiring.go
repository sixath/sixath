package chat

import (
	"context"
	"fmt"
	"sync"
	"time"

	"backend/internal/channel"

	"github.com/sixath/framework/tool"
)

const sendToWeComMinInterval = time.Second

// sendToWeComLastSend 按 session_id 记录上次发送时间，进程内限流。
var sendToWeComLastSend sync.Map

// SendToWeComOptions 构造 send_to_wecom 工具的依赖。
type SendToWeComOptions struct {
	ResolveWebhook func(ctx context.Context) (string, error)
}

// RegisterSendToWeComTool 向注册表注册 send_to_wecom；reg 或 ResolveWebhook 为空时不注册。
func RegisterSendToWeComTool(reg *tool.Registry, opts SendToWeComOptions) error {
	if reg == nil || opts.ResolveWebhook == nil {
		return nil
	}
	resolve := opts.ResolveWebhook
	return reg.Register(tool.Tool{
		Name: "send_to_wecom",
		Description: "Push a message to the bound WeCom (企业微信) group webhook. " +
			"Use when the user asks to notify the group, or when a conclusion should be shared with the team. " +
			"将消息推送到已绑定的企业微信群机器人；用户要求通知群聊或需要把结论同步到团队时使用。",
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
					"description": "WeCom message type. Default: text.",
				},
			},
			"required": []string{"content"},
		},
		Execute: buildSendToWeComExecute(resolve),
	})
}

func buildSendToWeComExecute(resolve func(context.Context) (string, error)) tool.ExecuteFunc {
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
		if err := checkSendToWeComRateLimit(sessionID); err != nil {
			return err.Error(), nil
		}

		url, err := resolve(ctx)
		if err != nil {
			return fmt.Sprintf("send_to_wecom: resolve webhook: %v", err), nil
		}
		if err := channel.PushToWeCom(ctx, url, content, msgType); err != nil {
			return fmt.Sprintf("send_to_wecom: %v", err), nil
		}
		return "已发送到企业微信群", nil
	}
}

func checkSendToWeComRateLimit(sessionID string) error {
	now := time.Now()
	if v, ok := sendToWeComLastSend.Load(sessionID); ok {
		if last, ok := v.(time.Time); ok && now.Sub(last) < sendToWeComMinInterval {
			return fmt.Errorf("发送过于频繁，请至少间隔 1 秒后再试")
		}
	}
	sendToWeComLastSend.Store(sessionID, now)
	return nil
}
