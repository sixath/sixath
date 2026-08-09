package wecom

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
)

type capturedFrame struct {
	Cmd     string
	ReqID   string
	Body    json.RawMessage
	Raw     []byte
}

func startMockWSServer(t *testing.T, handle func(ctx context.Context, conn *websocket.Conn)) (wsURL string, closeFn func()) {
	t.Helper()
	upgraded := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			t.Errorf("websocket.Accept: %v", err)
			return
		}
		close(upgraded)
		defer conn.Close(websocket.StatusNormalClosure, "")
		handle(r.Context(), conn)
	}))
	wsURL = "ws" + strings.TrimPrefix(srv.URL, "http")
	return wsURL, func() {
		srv.Close()
		select {
		case <-upgraded:
		default:
		}
	}
}

func readFrame(t *testing.T, ctx context.Context, conn *websocket.Conn) capturedFrame {
	t.Helper()
	_, data, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("conn.Read: %v", err)
	}
	var f Frame
	if err := json.Unmarshal(data, &f); err != nil {
		t.Fatalf("unmarshal frame: %v\nraw=%s", err, data)
	}
	return capturedFrame{Cmd: f.Cmd, ReqID: f.Headers.ReqID, Body: f.Body, Raw: data}
}

func writeJSON(t *testing.T, ctx context.Context, conn *websocket.Conn, v any) {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	if err := conn.Write(ctx, websocket.MessageText, data); err != nil {
		t.Fatalf("conn.Write: %v", err)
	}
}

func TestWSClient_SubscribeAndPing(t *testing.T) {
	const botID = "BOTID"
	const secret = "SECRET"

	subscribed := make(chan capturedFrame, 1)
	gotPing := make(chan capturedFrame, 1)

	wsURL, closeSrv := startMockWSServer(t, func(ctx context.Context, conn *websocket.Conn) {
		first := readFrame(t, ctx, conn)
		subscribed <- first
		writeJSON(t, ctx, conn, Frame{
			Cmd:     CmdSubscribe,
			Headers: FrameHeaders{ReqID: first.ReqID},
			Body:    json.RawMessage(`{"errcode":0,"errmsg":"ok"}`),
		})
		for {
			fr, err := func() (capturedFrame, error) {
				_, data, err := conn.Read(ctx)
				if err != nil {
					return capturedFrame{}, err
				}
				var f Frame
				if err := json.Unmarshal(data, &f); err != nil {
					return capturedFrame{}, err
				}
				return capturedFrame{Cmd: f.Cmd, ReqID: f.Headers.ReqID, Body: f.Body, Raw: data}, nil
			}()
			if err != nil {
				return
			}
			if fr.Cmd == CmdPing {
				select {
				case gotPing <- fr:
				default:
				}
			}
		}
	})
	defer closeSrv()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	client := NewClient(ClientConfig{
		URL:          wsURL,
		BotID:        botID,
		Secret:       secret,
		PingInterval: 40 * time.Millisecond,
	})

	errCh := make(chan error, 1)
	go func() { errCh <- client.Run(ctx) }()

	select {
	case fr := <-subscribed:
		if fr.Cmd != CmdSubscribe {
			t.Fatalf("first cmd=%q want %q", fr.Cmd, CmdSubscribe)
		}
		var body SubscribeBody
		if err := json.Unmarshal(fr.Body, &body); err != nil {
			t.Fatal(err)
		}
		if body.BotID != botID || body.Secret != secret {
			t.Fatalf("subscribe body=%+v", body)
		}
		if fr.ReqID == "" {
			t.Fatal("subscribe req_id empty")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for subscribe")
	}

	select {
	case fr := <-gotPing:
		if fr.ReqID == "" {
			t.Fatal("ping req_id empty")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for ping")
	}

	cancel()
	select {
	case err := <-errCh:
		if err != nil && err != context.Canceled {
			// Run may wrap cancel; either nil or canceled-ish is ok
			if ctx.Err() == nil {
				t.Fatalf("Run error: %v", err)
			}
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for Run exit")
	}
}

func TestWSClient_RespondStream(t *testing.T) {
	ready := make(chan struct{})
	gotRespond := make(chan capturedFrame, 1)
	var once sync.Once

	wsURL, closeSrv := startMockWSServer(t, func(ctx context.Context, conn *websocket.Conn) {
		first := readFrame(t, ctx, conn)
		if first.Cmd != CmdSubscribe {
			t.Errorf("first cmd=%q", first.Cmd)
			return
		}
		writeJSON(t, ctx, conn, Frame{
			Cmd:     CmdSubscribe,
			Headers: FrameHeaders{ReqID: first.ReqID},
			Body:    json.RawMessage(`{"errcode":0,"errmsg":"ok"}`),
		})
		once.Do(func() { close(ready) })

		for {
			_, data, err := conn.Read(ctx)
			if err != nil {
				return
			}
			var f Frame
			if err := json.Unmarshal(data, &f); err != nil {
				t.Errorf("bad frame: %v", err)
				return
			}
			if f.Cmd == CmdRespondMsg {
				gotRespond <- capturedFrame{Cmd: f.Cmd, ReqID: f.Headers.ReqID, Body: f.Body, Raw: data}
				return
			}
		}
	})
	defer closeSrv()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	client := NewClient(ClientConfig{
		URL:          wsURL,
		BotID:        "BOT",
		Secret:       "SEC",
		PingInterval: time.Hour,
	})
	errCh := make(chan error, 1)
	go func() { errCh <- client.Run(ctx) }()

	select {
	case <-ready:
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting subscribe ack")
	}

	const reqID = "callback-req-1"
	const streamID = "stream-1"
	const content = "hello reply"
	if err := client.RespondStream(ctx, reqID, streamID, content, true); err != nil {
		t.Fatalf("RespondStream: %v", err)
	}

	select {
	case fr := <-gotRespond:
		if fr.Cmd != CmdRespondMsg {
			t.Fatalf("cmd=%q", fr.Cmd)
		}
		if fr.ReqID != reqID {
			t.Fatalf("req_id=%q want %q", fr.ReqID, reqID)
		}
		var body RespondMsgBody
		if err := json.Unmarshal(fr.Body, &body); err != nil {
			t.Fatal(err)
		}
		if body.MsgType != "stream" {
			t.Fatalf("msgtype=%q", body.MsgType)
		}
		if body.Stream.ID != streamID || !body.Stream.Finish || body.Stream.Content != content {
			t.Fatalf("stream=%+v", body.Stream)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting respond")
	}

	// truncation
	long := strings.Repeat("a", MaxStreamContentBytes+100)
	if err := client.RespondStream(ctx, reqID, "s2", long, false); err != nil {
		// connection may already be closed by mock after first respond; use fresh server below if needed
		_ = err
	}

	cancel()
	<-errCh
}

func TestWSClient_RespondStream_Truncates(t *testing.T) {
	ready := make(chan struct{})
	gotRespond := make(chan capturedFrame, 1)

	wsURL, closeSrv := startMockWSServer(t, func(ctx context.Context, conn *websocket.Conn) {
		first := readFrame(t, ctx, conn)
		writeJSON(t, ctx, conn, Frame{
			Cmd:     CmdSubscribe,
			Headers: FrameHeaders{ReqID: first.ReqID},
			Body:    json.RawMessage(`{"errcode":0}`),
		})
		close(ready)
		for {
			_, data, err := conn.Read(ctx)
			if err != nil {
				return
			}
			var f Frame
			if err := json.Unmarshal(data, &f); err != nil {
				return
			}
			if f.Cmd == CmdRespondMsg {
				gotRespond <- capturedFrame{Cmd: f.Cmd, ReqID: f.Headers.ReqID, Body: f.Body}
				return
			}
		}
	})
	defer closeSrv()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	client := NewClient(ClientConfig{
		URL:          wsURL,
		BotID:        "B",
		Secret:       "S",
		PingInterval: time.Hour,
	})
	go func() { _ = client.Run(ctx) }()

	select {
	case <-ready:
	case <-time.After(2 * time.Second):
		t.Fatal("timeout subscribe")
	}

	long := strings.Repeat("x", MaxStreamContentBytes+50)
	if err := client.RespondStream(ctx, "r1", "sid", long, true); err != nil {
		t.Fatal(err)
	}

	select {
	case fr := <-gotRespond:
		var body RespondMsgBody
		if err := json.Unmarshal(fr.Body, &body); err != nil {
			t.Fatal(err)
		}
		if len(body.Stream.Content) != MaxStreamContentBytes {
			t.Fatalf("content len=%d want %d", len(body.Stream.Content), MaxStreamContentBytes)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout respond")
	}
	cancel()
}

func TestWSClient_SubscribeRejected(t *testing.T) {
	wsURL, closeSrv := startMockWSServer(t, func(ctx context.Context, conn *websocket.Conn) {
		first := readFrame(t, ctx, conn)
		writeJSON(t, ctx, conn, Frame{
			Cmd:     CmdSubscribe,
			Headers: FrameHeaders{ReqID: first.ReqID},
			Body:    json.RawMessage(`{"errcode":40013,"errmsg":"invalid secret"}`),
		})
		// keep conn briefly
		time.Sleep(50 * time.Millisecond)
	})
	defer closeSrv()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	client := NewClient(ClientConfig{
		URL:          wsURL,
		BotID:        "BOT",
		Secret:       "bad",
		PingInterval: time.Hour,
	})
	err := client.Run(ctx)
	if err == nil {
		t.Fatal("expected subscribe rejection error")
	}
}

func TestWSClient_OnMessageCallback(t *testing.T) {
	msgSeen := make(chan string, 1)
	ready := make(chan struct{})

	wsURL, closeSrv := startMockWSServer(t, func(ctx context.Context, conn *websocket.Conn) {
		first := readFrame(t, ctx, conn)
		writeJSON(t, ctx, conn, Frame{
			Cmd:     CmdSubscribe,
			Headers: FrameHeaders{ReqID: first.ReqID},
			Body:    json.RawMessage(`{"errcode":0}`),
		})
		close(ready)
		writeJSON(t, ctx, conn, Frame{
			Cmd:     CmdMsgCallback,
			Headers: FrameHeaders{ReqID: "cb-req-9"},
			Body:    json.RawMessage(`{"msgid":"M1","msgtype":"text"}`),
		})
		// wait until client closes
		_, _, _ = conn.Read(ctx)
	})
	defer closeSrv()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	client := NewClient(ClientConfig{
		URL:          wsURL,
		BotID:        "B",
		Secret:       "S",
		PingInterval: time.Hour,
		OnMessage: func(reqID string, body json.RawMessage) {
			msgSeen <- reqID
		},
	})
	go func() { _ = client.Run(ctx) }()

	select {
	case <-ready:
	case <-time.After(2 * time.Second):
		t.Fatal("timeout subscribe")
	}

	select {
	case id := <-msgSeen:
		if id != "cb-req-9" {
			t.Fatalf("reqID=%q", id)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout OnMessage")
	}
	cancel()
}
