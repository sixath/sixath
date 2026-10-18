package adapter_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sixath/gateway/internal/adapter"
	"github.com/sixath/gateway/internal/channel"
	"github.com/sixath/gateway/internal/idempotency"
	"github.com/sixath/gateway/internal/pendingswitch"
	"github.com/sixath/gateway/internal/runtimeclient"
	"github.com/sixath/gateway/internal/session"
)

// ilinkEventLog records iLink and Portal calls in one order so tests can assert sequencing.
type ilinkEventLog struct {
	mu     sync.Mutex
	events []string
}

func (l *ilinkEventLog) add(e string) {
	l.mu.Lock()
	l.events = append(l.events, e)
	l.mu.Unlock()
}

func (l *ilinkEventLog) index(prefix string) int {
	l.mu.Lock()
	defer l.mu.Unlock()
	for i, e := range l.events {
		if strings.HasPrefix(e, prefix) {
			return i
		}
	}
	return -1
}

type ilinkSent struct {
	to, contextToken, text string
}

type fakeIlinkServer struct {
	srv     *httptest.Server
	log     *ilinkEventLog
	mu      sync.Mutex
	batches []string
	polls   []string
	sent    []ilinkSent
	// sendHook runs after a sendmessage is recorded and before it is answered.
	sendHook func(r *http.Request)
}

func newFakeIlink(t *testing.T, log *ilinkEventLog) *fakeIlinkServer {
	t.Helper()
	f := &fakeIlinkServer{log: log}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		switch r.URL.Path {
		case "/ilink/bot/getupdates":
			var body struct {
				Buf string `json:"get_updates_buf"`
			}
			_ = json.Unmarshal(raw, &body)
			f.mu.Lock()
			f.polls = append(f.polls, body.Buf)
			var next string
			if len(f.batches) > 0 {
				next, f.batches = f.batches[0], f.batches[1:]
			}
			f.mu.Unlock()
			switch next {
			case "":
				select {
				case <-time.After(10 * time.Millisecond):
				case <-r.Context().Done():
				}
				_, _ = fmt.Fprintf(w, `{"ret":0,"msgs":[],"get_updates_buf":%q}`, body.Buf)
			case "HTTP500":
				http.Error(w, "boom", http.StatusInternalServerError)
			default:
				_, _ = w.Write([]byte(next))
			}
		case "/ilink/bot/sendmessage":
			var body struct {
				Msg struct {
					To           string `json:"to_user_id"`
					ContextToken string `json:"context_token"`
					ItemList     []struct {
						TextItem struct {
							Text string `json:"text"`
						} `json:"text_item"`
					} `json:"item_list"`
				} `json:"msg"`
			}
			_ = json.Unmarshal(raw, &body)
			text := ""
			if len(body.Msg.ItemList) > 0 {
				text = body.Msg.ItemList[0].TextItem.Text
			}
			f.mu.Lock()
			f.sent = append(f.sent, ilinkSent{to: body.Msg.To, contextToken: body.Msg.ContextToken, text: text})
			n := len(f.sent)
			hook := f.sendHook
			f.mu.Unlock()
			log.add("send:" + text)
			if hook != nil {
				hook(r)
			}
			_, _ = fmt.Fprintf(w, `{"message_id":%d}`, 9000+n)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeIlinkServer) push(raw string) {
	f.mu.Lock()
	f.batches = append(f.batches, raw)
	f.mu.Unlock()
}

func (f *fakeIlinkServer) pushMsgs(buf string, msgs ...string) {
	f.push(fmt.Sprintf(`{"ret":0,"msgs":[%s],"get_updates_buf":%q}`, strings.Join(msgs, ","), buf))
}

func (f *fakeIlinkServer) pollCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.polls)
}

func (f *fakeIlinkServer) firstPoll() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.polls) == 0 {
		return "<none>"
	}
	return f.polls[0]
}

func (f *fakeIlinkServer) sentSnapshot() []ilinkSent {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]ilinkSent(nil), f.sent...)
}

func (f *fakeIlinkServer) waitSent(t *testing.T, n int) []ilinkSent {
	t.Helper()
	waitFor(t, 3*time.Second, func() bool { return len(f.sentSnapshot()) >= n }, fmt.Sprintf("%d sendmessage calls", n))
	return f.sentSnapshot()
}

func sentTexts(sent []ilinkSent) []string {
	out := make([]string, len(sent))
	for i, s := range sent {
		out[i] = s.text
	}
	return out
}

func ilinkTextMsg(id int64, from, text string) string {
	return fmt.Sprintf(`{"message_id":%d,"from_user_id":%q,"to_user_id":"bot@im.bot","message_type":1,"context_token":"ctx-%d","create_time_ms":1760000000000,"item_list":[{"type":1,"text_item":{"text":%q}}]}`, id, from, id, text)
}

func ilinkImageMsg(id int64, from string) string {
	return fmt.Sprintf(`{"message_id":%d,"from_user_id":%q,"message_type":1,"context_token":"ctx-%d","item_list":[{"type":2}]}`, id, from, id)
}

func ilinkGroupMsg(id int64, from string) string {
	return fmt.Sprintf(`{"message_id":%d,"from_user_id":%q,"group_id":"g-1","message_type":1,"context_token":"ctx-%d","item_list":[{"type":1,"text_item":{"text":"群消息"}}]}`, id, from, id)
}

type fakeIlinkPortal struct {
	srv *httptest.Server
	log *ilinkEventLog

	mu              sync.Mutex
	cursorGen       int
	cursorBuf       string
	peerStatus      int
	cursorPutStatus int
	peers           []map[string]any
	cursorPuts      []map[string]any
	expired         []map[string]any
	turnHandler     func(w http.ResponseWriter, r *http.Request, body map[string]any)
	// peerHook runs after a peer report is recorded and before it is answered.
	peerHook func(r *http.Request)
	turns    int32
}

func newFakeIlinkPortal(t *testing.T, log *ilinkEventLog) *fakeIlinkPortal {
	t.Helper()
	p := &fakeIlinkPortal{log: log, cursorGen: 2}
	p.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		body := map[string]any{}
		_ = json.Unmarshal(raw, &body)
		const base = "/runtime/v1/gateway/channels/wx-1"
		p.mu.Lock()
		peerStatus, cursorPutStatus, turnHandler, peerHook := p.peerStatus, p.cursorPutStatus, p.turnHandler, p.peerHook
		p.mu.Unlock()
		switch {
		case r.URL.Path == base+"/ilink/peers":
			p.mu.Lock()
			p.peers = append(p.peers, body)
			p.mu.Unlock()
			log.add(fmt.Sprintf("peer:%v", body["peer_id"]))
			if peerHook != nil {
				peerHook(r)
			}
			if peerStatus != 0 {
				w.WriteHeader(peerStatus)
				_, _ = w.Write([]byte(`{"reason":"ILINK_GENERATION_MISMATCH"}`))
				return
			}
			_, _ = w.Write([]byte(`{"allowed":false}`))
		case r.Method == http.MethodGet && r.URL.Path == base+"/ilink/cursor":
			p.mu.Lock()
			out := map[string]any{"generation": p.cursorGen, "get_updates_buf": p.cursorBuf}
			p.mu.Unlock()
			_ = json.NewEncoder(w).Encode(out)
		case r.Method == http.MethodPut && r.URL.Path == base+"/ilink/cursor":
			p.mu.Lock()
			p.cursorPuts = append(p.cursorPuts, body)
			p.mu.Unlock()
			log.add(fmt.Sprintf("cursor:%v", body["get_updates_buf"]))
			if cursorPutStatus != 0 {
				w.WriteHeader(cursorPutStatus)
				_, _ = w.Write([]byte(`{"reason":"ILINK_GENERATION_MISMATCH"}`))
				return
			}
			_, _ = w.Write([]byte(`{"ok":true}`))
		case r.URL.Path == base+"/ilink/session-expired":
			p.mu.Lock()
			p.expired = append(p.expired, body)
			p.mu.Unlock()
			log.add("expired")
			_, _ = w.Write([]byte(`{"ok":true}`))
		case r.URL.Path == base+"/status":
			_, _ = w.Write([]byte(`{"ok":true}`))
		case r.Method == http.MethodGet && r.URL.Path == "/runtime/v1/channels/wx-1/agents":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"default_agent":      "agent-1",
				"auto_route_enabled": false,
				"agents": []map[string]string{
					{"id": "agent-1", "name": "Default"},
					{"id": "agent-2", "name": "Ops"},
				},
			})
		case r.URL.Path == "/runtime/v1/sessions/resolve":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"session_id": "sess-1", "agent_id": "agent-1", "user_id": "u1", "created": true,
			})
		case r.URL.Path == "/runtime/v1/turns":
			atomic.AddInt32(&p.turns, 1)
			log.add(fmt.Sprintf("turn:%v", body["peer_id"]))
			if turnHandler != nil {
				turnHandler(w, r, body)
				return
			}
			writeRuntimeSSEOK(w, "ok")
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(p.srv.Close)
	return p
}

func (p *fakeIlinkPortal) set(fn func(p *fakeIlinkPortal)) {
	p.mu.Lock()
	fn(p)
	p.mu.Unlock()
}

func (p *fakeIlinkPortal) counts() (peers, cursorPuts, expired int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.peers), len(p.cursorPuts), len(p.expired)
}

func (p *fakeIlinkPortal) turnCount() int { return int(atomic.LoadInt32(&p.turns)) }

type ilinkFixture struct {
	log      *ilinkEventLog
	ilink    *fakeIlinkServer
	portal   *fakeIlinkPortal
	reg      *channel.Registry
	ch       channel.Channel
	deps     adapter.WeixinIlinkDeps
	reporter *fakeStatusReporter
}

func newIlinkFixture(t *testing.T, allowed ...string) *ilinkFixture {
	t.Helper()
	log := &ilinkEventLog{}
	il := newFakeIlink(t, log)
	portal := newFakeIlinkPortal(t, log)
	ch := channel.Channel{
		ID: "wx-1", Type: "weixin_ilink", Enabled: true,
		BotToken: "bt-secret", BaseURL: il.srv.URL, Generation: 2, AllowedPeers: allowed,
	}
	reg := channel.NewRegistry()
	reg.ReplaceAll([]channel.Channel{ch})
	rt := runtimeclient.New(portal.srv.URL, "dev-runtime-token")
	reporter := &fakeStatusReporter{}
	return &ilinkFixture{
		log: log, ilink: il, portal: portal, reg: reg, ch: ch, reporter: reporter,
		deps: adapter.WeixinIlinkDeps{
			Registry:      reg,
			Runtime:       rt,
			Sessions:      session.NewRouter(rt, 30*time.Second),
			Idempotency:   idempotency.NewStore(10 * time.Minute),
			PendingSwitch: pendingswitch.New(),
			TurnTimeout:   5 * time.Second,
			Reporter:      reporter,
			PollTimeout:   2 * time.Second,
			BackoffMin:    20 * time.Millisecond,
		},
	}
}

// run starts RunWeixinIlink; the returned stop (also run at cleanup, before the fake servers
// close) cancels it and waits for it to exit.
func (f *ilinkFixture) run(t *testing.T) (stop func()) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		adapter.RunWeixinIlink(ctx, f.ch, f.deps)
		close(done)
	}()
	var once sync.Once
	stop = func() {
		once.Do(func() {
			cancel()
			select {
			case <-done:
			case <-time.After(5 * time.Second):
				t.Error("runner did not exit after cancel")
			}
		})
	}
	t.Cleanup(stop)
	return stop
}

// blockUntilCanceled returns a hook that holds a request until the client gives up or the test ends.
func blockUntilCanceled(t *testing.T) func(r *http.Request) {
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	return func(r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-release:
		case <-time.After(5 * time.Second):
		}
	}
}

func waitFor(t *testing.T, timeout time.Duration, cond func() bool, what string) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}
