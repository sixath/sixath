package wecom

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/gorilla/websocket"
)

const defaultPingInterval = 30 * time.Second

// MessageHandler is invoked for aibot_msg_callback frames.
type MessageHandler func(reqID string, body json.RawMessage)

// ClientConfig configures a WeCom long-connection client.
type ClientConfig struct {
	URL          string
	BotID        string
	Secret       string
	PingInterval time.Duration
	OnMessage    MessageHandler
}

// Client is a WeCom aibot WebSocket long-connection client.
// Uses gorilla/websocket: coder/websocket handshake gets HTTP 404 from openws.work.weixin.qq.com.
type Client struct {
	cfg ClientConfig

	mu   sync.Mutex
	conn *websocket.Conn
}

// NewClient builds a Client. URL defaults to the official endpoint when empty.
func NewClient(cfg ClientConfig) *Client {
	if cfg.URL == "" {
		cfg.URL = DefaultWSURL
	}
	if cfg.PingInterval <= 0 {
		cfg.PingInterval = defaultPingInterval
	}
	return &Client{cfg: cfg}
}

// Run dials, subscribes, then loops reading frames and sending pings.
// It returns when the connection ends or ctx is canceled so the caller can reconnect.
func (c *Client) Run(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	dialer := websocket.Dialer{HandshakeTimeout: 15 * time.Second}
	conn, resp, err := dialer.DialContext(ctx, c.cfg.URL, http.Header{})
	if err != nil {
		status := 0
		if resp != nil {
			status = resp.StatusCode
		}
		return fmt.Errorf("wecom ws dial: status=%d: %w", status, err)
	}
	c.mu.Lock()
	c.conn = conn
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		c.conn = nil
		c.mu.Unlock()
		_ = conn.Close()
	}()

	reqID := newReqID()
	subBody, err := json.Marshal(SubscribeBody{BotID: c.cfg.BotID, Secret: c.cfg.Secret})
	if err != nil {
		return err
	}
	if err := c.writeFrame(ctx, Frame{
		Cmd:     CmdSubscribe,
		Headers: FrameHeaders{ReqID: reqID},
		Body:    subBody,
	}); err != nil {
		return fmt.Errorf("wecom subscribe send: %w", err)
	}

	ack, err := c.readFrame(ctx)
	if err != nil {
		return fmt.Errorf("wecom subscribe ack: %w", err)
	}
	// Official ack puts errcode/errmsg at top level (not inside body).
	if ack.ErrCode != 0 {
		return fmt.Errorf("wecom subscribe rejected: errcode=%d errmsg=%s", ack.ErrCode, ack.ErrMsg)
	}
	// Tolerate mock/legacy body-shaped ack.
	if len(ack.Body) > 0 {
		var ackBody SubscribeAckBody
		if err := json.Unmarshal(ack.Body, &ackBody); err == nil && ackBody.ErrCode != 0 {
			return fmt.Errorf("wecom subscribe rejected: errcode=%d errmsg=%s", ackBody.ErrCode, ackBody.ErrMsg)
		}
	}

	pingTicker := time.NewTicker(c.cfg.PingInterval)
	defer pingTicker.Stop()

	errCh := make(chan error, 1)
	go func() {
		for {
			fr, err := c.readFrame(ctx)
			if err != nil {
				errCh <- err
				return
			}
			if fr.Cmd == CmdMsgCallback && c.cfg.OnMessage != nil {
				c.cfg.OnMessage(fr.Headers.ReqID, fr.Body)
			}
		}
	}()

	for {
		select {
		case <-ctx.Done():
			_ = conn.Close()
			return ctx.Err()
		case err := <-errCh:
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return fmt.Errorf("wecom ws read: %w", err)
		case <-pingTicker.C:
			if err := c.writeFrame(ctx, Frame{
				Cmd:     CmdPing,
				Headers: FrameHeaders{ReqID: newReqID()},
			}); err != nil {
				if ctx.Err() != nil {
					return ctx.Err()
				}
				return fmt.Errorf("wecom ping: %w", err)
			}
		}
	}
}

// truncateUTF8MaxBytes shortens s to at most maxBytes without splitting a UTF-8 code point.
func truncateUTF8MaxBytes(s string, maxBytes int) string {
	if len(s) <= maxBytes {
		return s
	}
	s = s[:maxBytes]
	for len(s) > 0 && !utf8.ValidString(s) {
		s = s[:len(s)-1]
	}
	return s
}

// RespondStream sends aibot_respond_msg with msgtype=stream.
// Content longer than MaxStreamContentBytes is truncated on a UTF-8 boundary.
func (c *Client) RespondStream(ctx context.Context, reqID, streamID, content string, finish bool) error {
	content = truncateUTF8MaxBytes(content, MaxStreamContentBytes)
	body, err := json.Marshal(RespondMsgBody{
		MsgType: "stream",
		Stream: StreamPayload{
			ID:      streamID,
			Finish:  finish,
			Content: content,
		},
	})
	if err != nil {
		return err
	}
	return c.writeFrame(ctx, Frame{
		Cmd:     CmdRespondMsg,
		Headers: FrameHeaders{ReqID: reqID},
		Body:    body,
	})
}

func (c *Client) writeFrame(ctx context.Context, fr Frame) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.conn == nil {
		return fmt.Errorf("wecom ws not connected")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	data, err := json.Marshal(fr)
	if err != nil {
		return err
	}
	_ = c.conn.SetWriteDeadline(time.Now().Add(30 * time.Second))
	return c.conn.WriteMessage(websocket.TextMessage, data)
}

func (c *Client) readFrame(ctx context.Context) (Frame, error) {
	c.mu.Lock()
	conn := c.conn
	c.mu.Unlock()
	if conn == nil {
		return Frame{}, fmt.Errorf("wecom ws not connected")
	}
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetReadDeadline(deadline)
	} else {
		_ = conn.SetReadDeadline(time.Time{})
	}
	_, data, err := conn.ReadMessage()
	if err != nil {
		if ctx.Err() != nil {
			return Frame{}, ctx.Err()
		}
		return Frame{}, err
	}
	var fr Frame
	if err := json.Unmarshal(data, &fr); err != nil {
		return Frame{}, err
	}
	return fr, nil
}

var reqSeq atomic.Uint64

func newReqID() string {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return fmt.Sprintf("%d-%s", reqSeq.Add(1), hex.EncodeToString(b[:]))
}
