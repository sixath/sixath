// Package ilink is a minimal HTTP client for the WeChat iLink bot gateway (QR login and outbound text).
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
	// DefaultLoginPollTimeout must exceed the ~30s the server holds get_qrcode_status open.
	DefaultLoginPollTimeout = 35 * time.Second

	channelVersion     = "1.0.0"
	requestTimeout     = 15 * time.Second
	itemTypeText       = 1
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
		PollTimeout: DefaultLoginPollTimeout,
	}
}

type QRCode struct {
	QRCode     string `json:"qrcode"`
	ImgContent string `json:"qrcode_img_content"`
}

type QRStatus struct {
	Status        string `json:"status"`
	BotToken      string `json:"bot_token"`
	BotID         string `json:"ilink_bot_id"`
	UserID        string `json:"ilink_user_id"`
	BaseURL       string `json:"baseurl"`
	ClientTimeout bool   `json:"-"`
}

func (c *Client) GetBotQRCode(ctx context.Context) (*QRCode, error) {
	qctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	var out QRCode
	if err := c.get(qctx, "/ilink/bot/get_bot_qrcode?bot_type=3", &out); err != nil {
		return nil, err
	}
	if out.QRCode == "" {
		return nil, errors.New("ilink: get_bot_qrcode returned empty qrcode")
	}
	return &out, nil
}

// GetQRCodeStatus treats a client-side timeout as "wait" because the server holds the request open.
func (c *Client) GetQRCodeStatus(ctx context.Context, qrcode string) (*QRStatus, error) {
	pctx, cancel := context.WithTimeout(ctx, c.PollTimeout)
	defer cancel()
	var out QRStatus
	err := c.get(pctx, "/ilink/bot/get_qrcode_status?qrcode="+url.QueryEscape(qrcode), &out)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) && ctx.Err() == nil {
			return &QRStatus{Status: "wait", ClientTimeout: true}, nil
		}
		return nil, err
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
	sctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	var out struct {
		MessageID json.Number `json:"message_id"`
	}
	if err := c.post(sctx, "/ilink/bot/sendmessage", map[string]any{"msg": msg}, &out); err != nil {
		return "", err
	}
	return out.MessageID.String(), nil
}

func (c *Client) get(ctx context.Context, pathAndQuery string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+pathAndQuery, nil)
	if err != nil {
		return err
	}
	req.Header.Set("iLink-App-ClientVersion", "1")
	return c.do(req, out)
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
	resp, err := c.HTTP.Do(req)
	if err != nil {
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
