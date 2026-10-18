// Package ilink is a minimal HTTP client for the WeChat iLink bot gateway (long-poll inbound and text replies).
package ilink

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const (
	DefaultBaseURL = "https://ilinkai.weixin.qq.com"
	// DefaultPollTimeout must exceed the ~35s the server holds getupdates open.
	DefaultPollTimeout = 40 * time.Second

	channelVersion     = "1.0.0"
	sendTimeout        = 15 * time.Second
	itemTypeText       = 1
	messageTypeUser    = 1
	messageTypeBot     = 2
	messageStateFinish = 2
)

// APIError is a business error returned with HTTP 200 (ret / errcode non-zero).
type APIError struct {
	Ret     int
	ErrCode int
	ErrMsg  string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("ilink ret=%d errcode=%d errmsg=%s", e.Ret, e.ErrCode, e.ErrMsg)
}

// SessionExpired reports -14; the server may set only one of ret / errcode.
func (e *APIError) SessionExpired() bool { return e.Ret == -14 || e.ErrCode == -14 }

func IsSessionExpired(err error) bool {
	var apiErr *APIError
	return errors.As(err, &apiErr) && apiErr.SessionExpired()
}

type Client struct {
	BaseURL     string
	Token       string
	HTTP        *http.Client
	PollTimeout time.Duration
}

func NewClient(baseURL, token string) *Client {
	if strings.TrimSpace(baseURL) == "" {
		baseURL = DefaultBaseURL
	}
	return &Client{
		BaseURL:     strings.TrimRight(baseURL, "/"),
		Token:       token,
		HTTP:        &http.Client{},
		PollTimeout: DefaultPollTimeout,
	}
}

type TextItem struct {
	Text string `json:"text"`
}

type MessageItem struct {
	Type     int       `json:"type"`
	TextItem *TextItem `json:"text_item,omitempty"`
}

// Message is one getupdates entry; message_id stays raw because it exceeds float64 precision.
type Message struct {
	MessageID    json.RawMessage `json:"message_id,omitempty"`
	FromUserID   string          `json:"from_user_id"`
	ToUserID     string          `json:"to_user_id"`
	CreateTimeMs int64           `json:"create_time_ms,omitempty"`
	GroupID      string          `json:"group_id,omitempty"`
	MessageType  int             `json:"message_type"`
	ContextToken string          `json:"context_token,omitempty"`
	ItemList     []MessageItem   `json:"item_list"`
}

type Updates struct {
	Msgs          []Message `json:"msgs"`
	GetUpdatesBuf string    `json:"get_updates_buf"`
}

// GetUpdates treats its own poll timeout as an empty batch that keeps the cursor; parent cancellation is an error.
func (c *Client) GetUpdates(ctx context.Context, buf string) (*Updates, error) {
	timeout := c.PollTimeout
	if timeout <= 0 {
		timeout = DefaultPollTimeout
	}
	pctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	var out Updates
	err := c.post(pctx, "/ilink/bot/getupdates", map[string]any{"get_updates_buf": buf}, &out)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) && ctx.Err() == nil {
			return &Updates{GetUpdatesBuf: buf}, nil
		}
		return nil, err
	}
	if out.GetUpdatesBuf == "" {
		out.GetUpdatesBuf = buf
	}
	return &out, nil
}

// SendText returns the server-assigned message_id (empty when the server omits it).
// A nil error does not prove delivery: the server also returns success for a stale context_token.
func (c *Client) SendText(ctx context.Context, to, contextToken, text string) (string, error) {
	msg := map[string]any{
		"from_user_id":  "",
		"to_user_id":    to,
		"client_id":     newClientID(),
		"message_type":  messageTypeBot,
		"message_state": messageStateFinish,
		"context_token": contextToken,
		"item_list":     []map[string]any{{"type": itemTypeText, "text_item": map[string]string{"text": text}}},
	}
	sctx, cancel := context.WithTimeout(ctx, sendTimeout)
	defer cancel()
	var out struct {
		MessageID json.RawMessage `json:"message_id"`
	}
	if err := c.post(sctx, "/ilink/bot/sendmessage", map[string]any{"msg": msg}, &out); err != nil {
		return "", err
	}
	return messageIDString(out.MessageID), nil
}

// messageIDString accepts a JSON string or number; any other shape yields "".
func messageIDString(raw json.RawMessage) string {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 {
		return ""
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var n json.Number
	if json.Unmarshal(raw, &n) == nil {
		return n.String()
	}
	return ""
}

func (c *Client) post(ctx context.Context, endpoint string, body map[string]any, out any) error {
	body["base_info"] = map[string]string{"channel_version": channelVersion}
	raw, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+endpoint, bytes.NewReader(raw))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("AuthorizationType", "ilink_bot_token")
	req.Header.Set("Authorization", "Bearer "+c.Token)
	req.Header.Set("X-WECHAT-UIN", randomUIN())
	return c.do(req, out)
}

func (c *Client) do(req *http.Request, out any) error {
	hc := c.HTTP
	if hc == nil {
		hc = http.DefaultClient
	}
	resp, err := hc.Do(req)
	if err != nil {
		// url.Error embeds the full URL; keep only method and path in logs.
		var ue *url.Error
		if errors.As(err, &ue) {
			return fmt.Errorf("ilink %s %s: %w", req.Method, req.URL.Path, ue.Err)
		}
		return err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("ilink %s %s: http %d: %s", req.Method, req.URL.Path, resp.StatusCode, truncate(maskSecrets(data), 200))
	}
	if len(bytes.TrimSpace(data)) == 0 {
		return nil
	}
	var head struct {
		Ret     int    `json:"ret"`
		ErrCode int    `json:"errcode"`
		ErrMsg  string `json:"errmsg"`
	}
	if err := json.Unmarshal(data, &head); err != nil {
		return fmt.Errorf("ilink %s: decode: %w", req.URL.Path, err)
	}
	if head.Ret != 0 || head.ErrCode != 0 {
		return &APIError{Ret: head.Ret, ErrCode: head.ErrCode, ErrMsg: head.ErrMsg}
	}
	if out == nil {
		return nil
	}
	return json.Unmarshal(data, out)
}

var secretFieldRe = regexp.MustCompile(`"(bot_token|context_token)"\s*:\s*"[^"]*"`)

func maskSecrets(data []byte) string {
	return secretFieldRe.ReplaceAllString(string(data), `"$1":"***"`)
}

func randomUIN() string {
	var b [4]byte
	_, _ = rand.Read(b[:])
	n := binary.BigEndian.Uint32(b[:])
	return base64.StdEncoding.EncodeToString([]byte(strconv.FormatUint(uint64(n), 10)))
}

func newClientID() string {
	var b [4]byte
	_, _ = rand.Read(b[:])
	return fmt.Sprintf("sixath:%d-%s", time.Now().UnixMilli(), hex.EncodeToString(b[:]))
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}
