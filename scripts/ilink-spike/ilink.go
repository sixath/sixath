package main

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
	defaultBaseURL     = "https://ilinkai.weixin.qq.com"
	channelVersion     = "1.0.0"
	defaultPollTimeout = 40 * time.Second
	sendTimeout        = 15 * time.Second

	itemTypeText       = 1
	messageTypeUser    = 1
	messageTypeBot     = 2
	messageStateFinish = 2
)

// APIError 是 iLink 以 HTTP 200 返回的业务错误（ret / errcode 非 0）。
type APIError struct {
	Ret     int
	ErrCode int
	ErrMsg  string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("ilink ret=%d errcode=%d errmsg=%s", e.Ret, e.ErrCode, e.ErrMsg)
}

func (e *APIError) SessionExpired() bool { return e.Ret == -14 || e.ErrCode == -14 }

type Client struct {
	BaseURL     string
	Token       string
	HTTP        *http.Client
	PollTimeout time.Duration
	// RawLog 非空时写入每个响应的原始 JSON（bot_token / context_token 已打码）。
	RawLog io.Writer
}

func NewClient(baseURL, token string) *Client {
	if baseURL == "" {
		baseURL = defaultBaseURL
	}
	return &Client{
		BaseURL:     strings.TrimRight(baseURL, "/"),
		Token:       token,
		HTTP:        &http.Client{},
		PollTimeout: defaultPollTimeout,
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

type TextItem struct {
	Text string `json:"text"`
}

type MessageItem struct {
	Type     int       `json:"type"`
	TextItem *TextItem `json:"text_item,omitempty"`
}

type Message struct {
	MessageID    int64         `json:"message_id,omitempty"`
	FromUserID   string        `json:"from_user_id"`
	ToUserID     string        `json:"to_user_id"`
	ClientID     string        `json:"client_id,omitempty"`
	CreateTimeMs int64         `json:"create_time_ms,omitempty"`
	GroupID      string        `json:"group_id,omitempty"`
	MessageType  int           `json:"message_type"`
	MessageState int           `json:"message_state,omitempty"`
	ContextToken string        `json:"context_token,omitempty"`
	ItemList     []MessageItem `json:"item_list"`
}

func (m Message) Text() string {
	var parts []string
	for _, it := range m.ItemList {
		if it.Type == itemTypeText && it.TextItem != nil {
			parts = append(parts, it.TextItem.Text)
		}
	}
	return strings.Join(parts, "\n")
}

func (m Message) ItemTypes() []int {
	out := make([]int, 0, len(m.ItemList))
	for _, it := range m.ItemList {
		out = append(out, it.Type)
	}
	return out
}

type Updates struct {
	Msgs          []Message `json:"msgs"`
	GetUpdatesBuf string    `json:"get_updates_buf"`
}

func (c *Client) GetBotQRCode(ctx context.Context) (*QRCode, error) {
	qctx, cancel := context.WithTimeout(ctx, sendTimeout)
	defer cancel()
	var out QRCode
	if err := c.get(qctx, "/ilink/bot/get_bot_qrcode?bot_type=3", &out); err != nil {
		return nil, err
	}
	if out.QRCode == "" {
		return nil, errors.New("get_bot_qrcode: empty qrcode")
	}
	return &out, nil
}

// GetQRCodeStatus 客户端超时视为 wait（服务端可能挂起长轮询），并用 ClientTimeout 标记。
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

// GetUpdates 客户端超时视为空批次，游标保持不变。
func (c *Client) GetUpdates(ctx context.Context, buf string) (*Updates, error) {
	pctx, cancel := context.WithTimeout(ctx, c.PollTimeout)
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

func (c *Client) SendText(ctx context.Context, to, contextToken, text string) error {
	msg := Message{
		FromUserID:   "",
		ToUserID:     to,
		ClientID:     newClientID(),
		MessageType:  messageTypeBot,
		MessageState: messageStateFinish,
		ContextToken: contextToken,
		ItemList:     []MessageItem{{Type: itemTypeText, TextItem: &TextItem{Text: text}}},
	}
	sctx, cancel := context.WithTimeout(ctx, sendTimeout)
	defer cancel()
	return c.post(sctx, "/ilink/bot/sendmessage", map[string]any{"msg": msg}, nil)
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
	if c.RawLog != nil {
		fmt.Fprintf(c.RawLog, "<- %s %s http=%d %s\n", req.Method, req.URL.Path, resp.StatusCode, maskSecrets(data))
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s %s: http %d: %s", req.Method, req.URL.Path, resp.StatusCode, truncate(maskSecrets(data), 200))
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
		return fmt.Errorf("%s: decode: %w", req.URL.Path, err)
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
	return fmt.Sprintf("sixath-spike:%d-%s", time.Now().UnixMilli(), hex.EncodeToString(b[:]))
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}
