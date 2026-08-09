package channel

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
	"unicode/utf8"
)

const wecomMaxContentBytes = 4096

// PushToWeCom 通过企微群机器人 Webhook 推送消息。
// webhookURL: 机器人 Webhook 地址
// content: 消息正文（超过 4096 字节时按 UTF-8 安全截断）
// msgType: "text" 或 "markdown"，空或无效值时按 text 处理
func PushToWeCom(ctx context.Context, webhookURL, content, msgType string) error {
	if webhookURL == "" || content == "" {
		return nil
	}
	content = truncateUTF8(content, wecomMaxContentBytes)
	msgType = normalizeWeComMsgType(msgType)

	body, err := marshalWeComPayload(content, msgType)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, webhookURL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("wecom webhook: HTTP %d: %s", resp.StatusCode, string(respBody))
	}

	var result wecomResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return fmt.Errorf("wecom webhook: parse response: %w", err)
	}
	if result.ErrCode != 0 {
		return fmt.Errorf("wecom webhook: errcode=%d errmsg=%s", result.ErrCode, result.ErrMsg)
	}
	return nil
}

func normalizeWeComMsgType(msgType string) string {
	if msgType == "markdown" {
		return "markdown"
	}
	return "text"
}

func marshalWeComPayload(content, msgType string) ([]byte, error) {
	switch msgType {
	case "markdown":
		return json.Marshal(map[string]any{
			"msgtype": "markdown",
			"markdown": map[string]string{
				"content": content,
			},
		})
	default:
		return json.Marshal(map[string]any{
			"msgtype": "text",
			"text": map[string]string{
				"content": content,
			},
		})
	}
}

func truncateUTF8(s string, maxBytes int) string {
	if len(s) <= maxBytes {
		return s
	}
	b := s[:maxBytes]
	for len(b) > 0 && !utf8.ValidString(b) {
		_, size := utf8.DecodeLastRuneInString(b)
		if size <= 0 || size > len(b) {
			return ""
		}
		b = b[:len(b)-size]
	}
	return b
}

type wecomResponse struct {
	ErrCode int    `json:"errcode"`
	ErrMsg  string `json:"errmsg"`
}
