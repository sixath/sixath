package channel

import (
	"context"

	"github.com/wxpusher/wxpusher-sdk-go"
	"github.com/wxpusher/wxpusher-sdk-go/model"
)

// PushToWxPusher 通过 WxPusher 推送消息到微信
// appToken: WxPusher 应用 Token
// uids: 接收者 UID 列表（至少一个）
// content: 消息内容
// summary: 可选摘要（用于通知栏显示）
func PushToWxPusher(ctx context.Context, appToken string, uids []string, content, summary string) error {
	if appToken == "" || len(uids) == 0 || content == "" {
		return nil
	}
	msg := model.NewMessage(appToken).SetContent(content)
	if summary != "" {
		msg.SetSummary(summary)
	}
	for _, uid := range uids {
		if uid != "" {
			msg.AddUId(uid)
		}
	}
	_, err := wxpusher.SendMessage(msg)
	return err
}
