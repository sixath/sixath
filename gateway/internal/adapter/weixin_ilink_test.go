package adapter_test

import (
	"context"
	"io"
	"net/http"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/sixath/gateway/internal/channel"
	"github.com/sixath/gateway/internal/idempotency"
)

const (
	ilinkAlice = "wxid_alice1234@im.wechat"
	ilinkBob   = "wxid_bob5678@im.wechat"
)

func TestWeixinIlink_PeerReportThenCursorThenTurn(t *testing.T) {
	f := newIlinkFixture(t, ilinkAlice)
	f.portal.set(func(p *fakeIlinkPortal) { p.cursorBuf = "buf-0" })
	f.ilink.pushMsgs("buf-1", ilinkTextMsg(101, ilinkAlice, "你好"))
	f.run(t)

	sent := f.ilink.waitSent(t, 1)
	if sent[0].text != "ok" || sent[0].to != ilinkAlice || sent[0].contextToken != "ctx-101" {
		t.Fatalf("sent=%+v", sent[0])
	}
	if got := f.ilink.firstPoll(); got != "buf-0" {
		t.Fatalf("first poll buf=%q want buf-0", got)
	}
	peer, cursor, turn, send := f.log.index("peer:"), f.log.index("cursor:buf-1"), f.log.index("turn:"), f.log.index("send:ok")
	if peer < 0 || cursor < 0 || turn < 0 || send < 0 || !(peer < cursor && cursor < turn && turn < send) {
		t.Fatalf("order peer=%d cursor=%d turn=%d send=%d", peer, cursor, turn, send)
	}
	f.portal.mu.Lock()
	defer f.portal.mu.Unlock()
	p := f.portal.peers[0]
	if p["generation"] != float64(2) || p["peer_id"] != ilinkAlice || p["context_token"] != "ctx-101" || p["preview"] != "你好" || p["at"] != "2025-10-09T08:53:20Z" {
		t.Fatalf("peer report=%v", p)
	}
	c := f.portal.cursorPuts[0]
	if c["generation"] != float64(2) || c["get_updates_buf"] != "buf-1" {
		t.Fatalf("cursor put=%v", c)
	}
}

func TestWeixinIlink_StaleCursorGenerationStartsEmpty(t *testing.T) {
	f := newIlinkFixture(t, ilinkAlice)
	f.portal.set(func(p *fakeIlinkPortal) { p.cursorGen, p.cursorBuf = 1, "old-bot-buf" })
	f.run(t)

	waitFor(t, 2*time.Second, func() bool { return f.ilink.pollCount() > 0 }, "first poll")
	if got := f.ilink.firstPoll(); got != "" {
		t.Fatalf("first poll buf=%q want empty", got)
	}
}

func TestWeixinIlink_DeniedRateLimitedThenAllowedViaRegistry(t *testing.T) {
	f := newIlinkFixture(t)
	f.ilink.pushMsgs("b1", ilinkTextMsg(1, ilinkBob, "hi"), ilinkTextMsg(2, ilinkBob, "hi again"))
	f.run(t)

	f.ilink.waitSent(t, 1)
	waitFor(t, 2*time.Second, func() bool { peers, _, _ := f.portal.counts(); return peers == 2 }, "2 peer reports")
	time.Sleep(50 * time.Millisecond)
	sent := f.ilink.sentSnapshot()
	if len(sent) != 1 || sent[0].text != "无权限，请联系管理员开通（ID 尾号 5678）" {
		t.Fatalf("denied replies=%q", sentTexts(sent))
	}
	if f.portal.turnCount() != 0 {
		t.Fatalf("turns=%d want 0", f.portal.turnCount())
	}

	allowed := f.ch
	allowed.AllowedPeers = []string{ilinkBob}
	f.reg.ReplaceAll([]channel.Channel{allowed})
	f.ilink.pushMsgs("b2", ilinkTextMsg(3, ilinkBob, "现在呢"))

	sent = f.ilink.waitSent(t, 2)
	if sent[1].text != "ok" || f.portal.turnCount() != 1 {
		t.Fatalf("after allow: sent=%q turns=%d", sentTexts(sent), f.portal.turnCount())
	}
}

func TestWeixinIlink_NonTextRepliesAndGroupIgnored(t *testing.T) {
	f := newIlinkFixture(t, ilinkAlice)
	f.ilink.pushMsgs("b1", ilinkGroupMsg(1, ilinkAlice), ilinkImageMsg(2, ilinkAlice))
	f.run(t)

	sent := f.ilink.waitSent(t, 1)
	time.Sleep(50 * time.Millisecond)
	sent = f.ilink.sentSnapshot()
	if len(sent) != 1 || sent[0].text != "暂只支持文字消息" || sent[0].contextToken != "ctx-2" {
		t.Fatalf("sent=%+v", sent)
	}
	if peers, _, _ := f.portal.counts(); peers != 1 {
		t.Fatalf("peer reports=%d want 1 (group message skipped)", peers)
	}
	if f.portal.turnCount() != 0 {
		t.Fatalf("turns=%d want 0", f.portal.turnCount())
	}
}

func TestWeixinIlink_DuplicateMessageIDProcessedOnce(t *testing.T) {
	f := newIlinkFixture(t, ilinkAlice)
	f.ilink.pushMsgs("b1", ilinkTextMsg(7, ilinkAlice, "hi"))
	f.ilink.pushMsgs("b2", ilinkTextMsg(7, ilinkAlice, "hi"))
	f.run(t)

	waitFor(t, 2*time.Second, func() bool { _, puts, _ := f.portal.counts(); return puts == 2 }, "both batches")
	f.ilink.waitSent(t, 1)
	time.Sleep(50 * time.Millisecond)
	if n := len(f.ilink.sentSnapshot()); n != 1 || f.portal.turnCount() != 1 {
		t.Fatalf("sent=%d turns=%d want 1/1", n, f.portal.turnCount())
	}
}

func TestWeixinIlink_SlashCommandNoTurn(t *testing.T) {
	f := newIlinkFixture(t, ilinkAlice)
	f.ilink.pushMsgs("b1", ilinkTextMsg(1, ilinkAlice, "/agents"))
	f.run(t)

	sent := f.ilink.waitSent(t, 1)
	if !strings.Contains(sent[0].text, "可用 Agent") || !strings.Contains(sent[0].text, "Ops") {
		t.Fatalf("reply=%q", sent[0].text)
	}
	if f.portal.turnCount() != 0 {
		t.Fatalf("turns=%d want 0", f.portal.turnCount())
	}
}

func TestWeixinIlink_HITLRepliesWebOnly(t *testing.T) {
	f := newIlinkFixture(t, ilinkAlice)
	f.portal.set(func(p *fakeIlinkPortal) {
		p.turnHandler = func(w http.ResponseWriter, _ *http.Request, _ map[string]any) { writeRuntimeSSEHITL(w) }
	})
	f.ilink.pushMsgs("b1", ilinkTextMsg(1, ilinkAlice, "删库"))
	f.run(t)

	sent := f.ilink.waitSent(t, 1)
	if sent[0].text != "该操作需要在 Web 端确认" {
		t.Fatalf("reply=%q", sent[0].text)
	}
}

func TestWeixinIlink_TurnErrorRepliesFailure(t *testing.T) {
	f := newIlinkFixture(t, ilinkAlice)
	f.portal.set(func(p *fakeIlinkPortal) {
		p.turnHandler = func(w http.ResponseWriter, _ *http.Request, _ map[string]any) {
			http.Error(w, "boom", http.StatusInternalServerError)
		}
	})
	f.ilink.pushMsgs("b1", ilinkTextMsg(1, ilinkAlice, "hi"))
	f.run(t)

	sent := f.ilink.waitSent(t, 1)
	if sent[0].text != "处理失败：操作失败，请稍后重试" {
		t.Fatalf("reply=%q", sent[0].text)
	}
}

func TestWeixinIlink_ProcessingNotice(t *testing.T) {
	t.Run("slow turn sends one notice", func(t *testing.T) {
		f := newIlinkFixture(t, ilinkAlice)
		f.deps.ProcessingNoticeAfter = 50 * time.Millisecond
		f.portal.set(func(p *fakeIlinkPortal) {
			p.turnHandler = func(w http.ResponseWriter, r *http.Request, _ map[string]any) {
				w.Header().Set("Content-Type", "text/event-stream")
				w.WriteHeader(http.StatusOK)
				w.(http.Flusher).Flush()
				select {
				case <-time.After(250 * time.Millisecond):
				case <-r.Context().Done():
					return
				}
				_, _ = io.WriteString(w, "event: chunk\ndata: {\"content\":\"ok\"}\n\nevent: done\ndata: {}\n\n")
			}
		})
		f.ilink.pushMsgs("b1", ilinkTextMsg(1, ilinkAlice, "慢问题"))
		f.run(t)

		sent := f.ilink.waitSent(t, 2)
		time.Sleep(50 * time.Millisecond)
		if got := sentTexts(f.ilink.sentSnapshot()); !slices.Equal(got, []string{"处理中…", "ok"}) {
			t.Fatalf("sent=%q", got)
		}
		if sent[0].contextToken != "ctx-1" {
			t.Fatalf("notice context_token=%q", sent[0].contextToken)
		}
	})
	t.Run("fast turn sends no notice", func(t *testing.T) {
		f := newIlinkFixture(t, ilinkAlice)
		f.deps.ProcessingNoticeAfter = time.Second
		f.ilink.pushMsgs("b1", ilinkTextMsg(1, ilinkAlice, "快问题"))
		f.run(t)

		f.ilink.waitSent(t, 1)
		time.Sleep(50 * time.Millisecond)
		if got := sentTexts(f.ilink.sentSnapshot()); !slices.Equal(got, []string{"ok"}) {
			t.Fatalf("sent=%q", got)
		}
	})
}

func TestWeixinIlink_LongReplyShapedAndSplit(t *testing.T) {
	f := newIlinkFixture(t, ilinkAlice)
	f.deps.SplitRunes = 10
	f.portal.set(func(p *fakeIlinkPortal) {
		p.turnHandler = func(w http.ResponseWriter, _ *http.Request, _ map[string]any) {
			writeRuntimeSSEOK(w, "# 标题\n**第一段内容**\n第二段内容很长很长很长很长")
		}
	})
	f.ilink.pushMsgs("b1", ilinkTextMsg(1, ilinkAlice, "长回答"))
	f.run(t)

	f.ilink.waitSent(t, 3)
	time.Sleep(50 * time.Millisecond)
	sent := f.ilink.sentSnapshot()
	var joined strings.Builder
	for _, s := range sent {
		if utf8.RuneCountInString(s.text) > 10 || s.contextToken != "ctx-1" {
			t.Fatalf("bad part %+v", s)
		}
		joined.WriteString(strings.ReplaceAll(s.text, "\n", ""))
	}
	if joined.String() != "标题第一段内容第二段内容很长很长很长很长" {
		t.Fatalf("parts=%q", sentTexts(sent))
	}
}

func TestWeixinIlink_SamePeerSerialCrossPeerConcurrent(t *testing.T) {
	f := newIlinkFixture(t, ilinkAlice, ilinkBob)
	release := make(chan struct{})
	concurrent := make(chan struct{}, 1)
	var mu sync.Mutex
	active, maxActive, total := map[string]int{}, map[string]int{}, 0
	f.portal.set(func(p *fakeIlinkPortal) {
		p.turnHandler = func(w http.ResponseWriter, r *http.Request, body map[string]any) {
			peer, _ := body["peer_id"].(string)
			mu.Lock()
			active[peer]++
			maxActive[peer] = max(maxActive[peer], active[peer])
			total++
			if total >= 2 {
				select {
				case concurrent <- struct{}{}:
				default:
				}
			}
			mu.Unlock()
			select {
			case <-release:
			case <-r.Context().Done():
			}
			mu.Lock()
			active[peer]--
			total--
			mu.Unlock()
			writeRuntimeSSEOK(w, "ok")
		}
	})
	f.ilink.pushMsgs("b1", ilinkTextMsg(1, ilinkAlice, "a1"), ilinkTextMsg(2, ilinkAlice, "a2"), ilinkTextMsg(3, ilinkBob, "b1"))
	f.run(t)

	select {
	case <-concurrent:
	case <-time.After(3 * time.Second):
		t.Fatal("alice and bob turns never ran concurrently")
	}
	close(release)
	f.ilink.waitSent(t, 3)
	mu.Lock()
	defer mu.Unlock()
	if maxActive["user:"+ilinkAlice] != 1 {
		t.Fatalf("alice max concurrent turns=%d want 1", maxActive["user:"+ilinkAlice])
	}
}

func TestWeixinIlink_PeerQueueOverflow(t *testing.T) {
	f := newIlinkFixture(t, ilinkAlice)
	f.deps.PeerQueueSize = 1
	release := make(chan struct{})
	started := make(chan struct{}, 4)
	f.portal.set(func(p *fakeIlinkPortal) {
		p.turnHandler = func(w http.ResponseWriter, r *http.Request, _ map[string]any) {
			started <- struct{}{}
			select {
			case <-release:
			case <-r.Context().Done():
			}
			writeRuntimeSSEOK(w, "ok")
		}
	})
	f.ilink.pushMsgs("b1", ilinkTextMsg(1, ilinkAlice, "first"))
	f.run(t)
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("first turn never started")
	}

	f.ilink.pushMsgs("b2", ilinkTextMsg(2, ilinkAlice, "queued"), ilinkTextMsg(3, ilinkAlice, "overflow"))
	sent := f.ilink.waitSent(t, 1)
	if sent[0].text != "消息太多，请稍后再试" || sent[0].contextToken != "ctx-3" {
		t.Fatalf("overflow reply=%+v", sent[0])
	}
	close(release)
	f.ilink.waitSent(t, 3)
	time.Sleep(50 * time.Millisecond)
	if f.portal.turnCount() != 2 {
		t.Fatalf("turns=%d want 2", f.portal.turnCount())
	}
}

func TestWeixinIlink_CursorConflictSuspends(t *testing.T) {
	f := newIlinkFixture(t, ilinkAlice)
	f.portal.set(func(p *fakeIlinkPortal) { p.cursorPutStatus = http.StatusConflict })
	f.ilink.pushMsgs("b1", ilinkTextMsg(1, ilinkAlice, "hi"))
	f.run(t)

	waitFor(t, 2*time.Second, func() bool { _, puts, _ := f.portal.counts(); return puts == 1 }, "cursor put")
	time.Sleep(150 * time.Millisecond)
	if f.ilink.pollCount() != 1 || f.portal.turnCount() != 0 || len(f.ilink.sentSnapshot()) != 0 {
		t.Fatalf("polls=%d turns=%d sent=%d want 1/0/0", f.ilink.pollCount(), f.portal.turnCount(), len(f.ilink.sentSnapshot()))
	}
}

func TestWeixinIlink_PeerReportConflictSuspends(t *testing.T) {
	f := newIlinkFixture(t, ilinkAlice)
	f.portal.set(func(p *fakeIlinkPortal) { p.peerStatus = http.StatusConflict })
	f.ilink.pushMsgs("b1", ilinkTextMsg(1, ilinkAlice, "hi"))
	f.run(t)

	waitFor(t, 2*time.Second, func() bool { peers, _, _ := f.portal.counts(); return peers == 1 }, "peer report")
	time.Sleep(150 * time.Millisecond)
	_, puts, _ := f.portal.counts()
	if f.ilink.pollCount() != 1 || puts != 0 || f.portal.turnCount() != 0 {
		t.Fatalf("polls=%d cursorPuts=%d turns=%d want 1/0/0", f.ilink.pollCount(), puts, f.portal.turnCount())
	}
}

func TestWeixinIlink_CursorPutFailureRetriedThenContinues(t *testing.T) {
	f := newIlinkFixture(t, ilinkAlice)
	f.portal.set(func(p *fakeIlinkPortal) { p.cursorPutStatus = http.StatusBadGateway })
	f.ilink.pushMsgs("b1", ilinkTextMsg(1, ilinkAlice, "hi"))
	f.run(t)

	sent := f.ilink.waitSent(t, 1)
	if sent[0].text != "ok" {
		t.Fatalf("reply=%q", sent[0].text)
	}
	if _, puts, _ := f.portal.counts(); puts != 4 {
		t.Fatalf("cursor put attempts=%d want 4", puts)
	}
}

func TestWeixinIlink_HungPeerReportBoundedByPortalCallTimeout(t *testing.T) {
	f := newIlinkFixture(t, ilinkAlice)
	f.deps.PortalCallTimeout = 100 * time.Millisecond
	hang := blockUntilCanceled(t)
	f.portal.set(func(p *fakeIlinkPortal) { p.peerHook = hang })
	f.ilink.pushMsgs("b1", ilinkTextMsg(1, ilinkAlice, "hi"))
	start := time.Now()
	f.run(t)

	sent := f.ilink.waitSent(t, 1)
	if sent[0].text != "ok" {
		t.Fatalf("reply=%q", sent[0].text)
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("reply took %s; hung peer report blocked the runner", elapsed)
	}
	waitFor(t, 2*time.Second, func() bool { return f.ilink.pollCount() >= 2 }, "next getupdates after hung peer report")
}

func TestWeixinIlink_CancelDuringCursorRetryDoesNotDispatch(t *testing.T) {
	f := newIlinkFixture(t, ilinkAlice)
	store := idempotency.NewStore(10 * time.Minute)
	f.deps.Idempotency = store
	f.portal.set(func(p *fakeIlinkPortal) { p.cursorPutStatus = http.StatusBadGateway })
	f.ilink.pushMsgs("b1", ilinkTextMsg(1, ilinkAlice, "hi"))
	stop := f.run(t)

	waitFor(t, 2*time.Second, func() bool { _, puts, _ := f.portal.counts(); return puts >= 1 }, "first cursor put")
	stop()

	if _, began, _ := store.Get(context.Background(), "ilink:wx-1:1"); began {
		t.Fatal("message dispatched after the runner was canceled")
	}
	if f.portal.turnCount() != 0 || len(f.ilink.sentSnapshot()) != 0 {
		t.Fatalf("turns=%d sent=%d want 0/0", f.portal.turnCount(), len(f.ilink.sentSnapshot()))
	}
}

func TestWeixinIlink_EmptyAnswerRepliesChineseNotice(t *testing.T) {
	f := newIlinkFixture(t, ilinkAlice)
	f.portal.set(func(p *fakeIlinkPortal) {
		p.turnHandler = func(w http.ResponseWriter, _ *http.Request, _ map[string]any) {
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
			_, _ = io.WriteString(w, "event: done\ndata: {}\n\n")
		}
	})
	f.ilink.pushMsgs("b1", ilinkTextMsg(1, ilinkAlice, "hi"))
	f.run(t)

	sent := f.ilink.waitSent(t, 1)
	time.Sleep(50 * time.Millisecond)
	if got := sentTexts(f.ilink.sentSnapshot()); !slices.Equal(got, []string{"未生成回复，请稍后重试。"}) {
		t.Fatalf("sent=%q", got)
	}
	if sent[0].contextToken != "ctx-1" {
		t.Fatalf("context_token=%q", sent[0].contextToken)
	}
}

func TestWeixinIlink_AnswerShapedToNothingFallsBackToRaw(t *testing.T) {
	f := newIlinkFixture(t, ilinkAlice)
	f.portal.set(func(p *fakeIlinkPortal) {
		p.turnHandler = func(w http.ResponseWriter, _ *http.Request, _ map[string]any) {
			writeRuntimeSSEOK(w, "```go\n```")
		}
	})
	f.ilink.pushMsgs("b1", ilinkTextMsg(1, ilinkAlice, "hi"))
	f.run(t)

	f.ilink.waitSent(t, 1)
	time.Sleep(50 * time.Millisecond)
	if got := sentTexts(f.ilink.sentSnapshot()); !slices.Equal(got, []string{"```go\n```"}) {
		t.Fatalf("sent=%q", got)
	}
}

func TestWeixinIlink_NoticeSendDoesNotBlockPolling(t *testing.T) {
	f := newIlinkFixture(t)
	hang := blockUntilCanceled(t)
	f.ilink.mu.Lock()
	f.ilink.sendHook = hang
	f.ilink.mu.Unlock()
	f.ilink.pushMsgs("b1", ilinkTextMsg(1, ilinkBob, "hi"))
	f.run(t)

	f.ilink.waitSent(t, 1)
	waitFor(t, time.Second, func() bool { return f.ilink.pollCount() >= 3 }, "polling continues while the denied reply is in flight")
}

func TestWeixinIlink_SessionExpiredReportsAndSuspends(t *testing.T) {
	f := newIlinkFixture(t, ilinkAlice)
	f.ilink.push(`{"errcode":-14,"errmsg":"session timeout"}`)
	f.run(t)

	waitFor(t, 2*time.Second, func() bool { _, _, exp := f.portal.counts(); return exp == 1 }, "session-expired report")
	time.Sleep(150 * time.Millisecond)
	if f.ilink.pollCount() != 1 {
		t.Fatalf("polls=%d want 1 (suspended)", f.ilink.pollCount())
	}
	f.portal.mu.Lock()
	defer f.portal.mu.Unlock()
	if e := f.portal.expired[0]; e["generation"] != float64(2) || e["reason"] != "-14" {
		t.Fatalf("session-expired body=%v", e)
	}
}

func TestWeixinIlink_HeartbeatAndReconnecting(t *testing.T) {
	f := newIlinkFixture(t, ilinkAlice)
	f.deps.HeartbeatInterval = 30 * time.Millisecond
	f.ilink.push("HTTP500")
	f.run(t)

	waitFor(t, 2*time.Second, func() bool {
		states := f.reporter.statesFor("wx-1")
		i := slices.Index(states, "reconnecting")
		if i < 0 {
			return false
		}
		connectedAfter := 0
		for _, s := range states[i+1:] {
			if s == "connected" {
				connectedAfter++
			}
		}
		return connectedAfter >= 3
	}, "reconnecting followed by connected heartbeats")
}
