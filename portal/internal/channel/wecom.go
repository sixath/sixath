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

// WeComError 结构化出站错误，便于投递层做重试判定与持久化记录。
type WeComError struct {
	HTTPStatus int // 非 0 表示 HTTP 层错误
	ErrCode    int // 企微业务错误码，非 0 表示业务失败
	ErrMsg     string
	Cause      error // 底层传输错误（连接重置 / 超时 / EOF），可能为 nil
}

func (e *WeComError) Error() string {
	if e == nil {
		return "wecom webhook: <nil>"
	}
	switch {
	case e.Cause != nil:
		return "wecom webhook: " + e.Cause.Error()
	case e.ErrCode != 0:
		return fmt.Sprintf("wecom webhook: errcode=%d errmsg=%s", e.ErrCode, e.ErrMsg)
	case e.HTTPStatus != 0:
		return fmt.Sprintf("wecom webhook: HTTP %d: %s", e.HTTPStatus, e.ErrMsg)
	default:
		if e.ErrMsg != "" {
			return "wecom webhook: " + e.ErrMsg
		}
		return "wecom webhook: unknown error"
	}
}

// Unwrap 暴露底层错误，供 errors.Is/As 与日志归因。
func (e *WeComError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

// Retryable 判断该错误是否值得重试：仅传输层错误与 5xx 可重试；
// 4xx（URL 非法/鉴权失败）与业务 errcode（如 93000 invalid url）不可重试。
func (e *WeComError) Retryable() bool {
	if e == nil {
		return false
	}
	if e.Cause != nil {
		return true
	}
	return e.HTTPStatus >= 500
}

// PushToWeCom 通过企微群机器人 Webhook 推送消息（单次发送，不做重试）。
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
		return fmt.Errorf("wecom webhook: marshal payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, webhookURL, bytes.NewReader(body))
	if err != nil {
		return &WeComError{Cause: err}
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return &WeComError{Cause: err}
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return &WeComError{Cause: err}
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return &WeComError{HTTPStatus: resp.StatusCode, ErrMsg: string(respBody)}
	}

	var result wecomResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return &WeComError{ErrMsg: "parse response: " + err.Error()}
	}
	if result.ErrCode != 0 {
		return &WeComError{ErrCode: result.ErrCode, ErrMsg: result.ErrMsg}
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
