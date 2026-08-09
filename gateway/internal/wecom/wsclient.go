package wecom

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"
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
	conn, _, err := websocket.Dial(ctx, c.cfg.URL, nil)
	if err != nil {
		return fmt.Errorf("wecom ws dial: %w", err)
	}
	c.mu.Lock()
	c.conn = conn
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		c.conn = nil
		c.mu.Unlock()
		_ = conn.Close(websocket.StatusNormalClosure, "")
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
	var ackBody SubscribeAckBody
	if len(ack.Body) > 0 {
		if err := json.Unmarshal(ack.Body, &ackBody); err != nil {
			return fmt.Errorf("wecom subscribe ack decode: %w", err)
		}
	}
	if ackBody.ErrCode != 0 {
		return fmt.Errorf("wecom subscribe rejected: errcode=%d errmsg=%s", ackBody.ErrCode, ackBody.ErrMsg)
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

// RespondStream sends aibot_respond_msg with msgtype=stream.
// Content longer than MaxStreamContentBytes is truncated.
func (c *Client) RespondStream(ctx context.Context, reqID, streamID, content string, finish bool) error {
	if len(content) > MaxStreamContentBytes {
		content = content[:MaxStreamContentBytes]
	}
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
	data, err := json.Marshal(fr)
	if err != nil {
		return err
	}
	return c.conn.Write(ctx, websocket.MessageText, data)
}

func (c *Client) readFrame(ctx context.Context) (Frame, error) {
	c.mu.Lock()
	conn := c.conn
	c.mu.Unlock()
	if conn == nil {
		return Frame{}, fmt.Errorf("wecom ws not connected")
	}
	_, data, err := conn.Read(ctx)
	if err != nil {
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
