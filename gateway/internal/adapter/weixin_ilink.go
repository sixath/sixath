package adapter

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/sixath/gateway/internal/channel"
	"github.com/sixath/gateway/internal/command"
	"github.com/sixath/gateway/internal/idempotency"
	"github.com/sixath/gateway/internal/ilink"
	"github.com/sixath/gateway/internal/pendingswitch"
	"github.com/sixath/gateway/internal/runtimeclient"
	"github.com/sixath/gateway/internal/session"
)

// WeixinIlinkDeps wires weixin_ilink long-poll runners.
type WeixinIlinkDeps struct {
	Registry      *channel.Registry
	Runtime       *runtimeclient.Client
	Sessions      *session.Router
	Idempotency   idempotency.Store
	PendingSwitch *pendingswitch.Store
	TurnTimeout   time.Duration
	// Reporter posts runtime status; when nil, Runtime is used if non-nil.
	Reporter StatusReporter
	// SplitRunes caps each outgoing message; zero means ilink.DefaultSplitRunes.
	SplitRunes int

	// Zero values below mean production defaults; tests shrink them.
	PollTimeout           time.Duration // getupdates client timeout (40s)
	HeartbeatInterval     time.Duration // status heartbeat, independent of polling (30s)
	ProcessingNoticeAfter time.Duration // send 「处理中…」 once when no final reply yet (15s)
	DeniedReplyInterval   time.Duration // 「无权限」 at most once per peer per interval (10m)
	PeerQueueSize         int           // pending messages per peer beyond the one in progress (5)
	PeerIdleTimeout       time.Duration // idle peer worker exits (5m)
	BackoffMin            time.Duration // first retry delay for iLink / Portal failures (1s), doubling to 60s
}

const (
	ilinkDeniedTemplate    = "无权限，请联系管理员开通（ID 尾号 %s）"
	ilinkNonTextReply      = "暂只支持文字消息"
	ilinkQueueFullReply    = "消息太多，请稍后再试"
	ilinkHITLReply         = "该操作需要在 Web 端确认"
	ilinkFailurePrefix     = "处理失败："
	ilinkProcessingReply   = "处理中…"
	ilinkBackoffMax        = 60 * time.Second
	ilinkCursorPutAttempts = 4
	ilinkCursorRetryDelay  = 200 * time.Millisecond
)

func normalizeWeixinIlinkDeps(deps WeixinIlinkDeps) WeixinIlinkDeps {
	if deps.TurnTimeout <= 0 {
		deps.TurnTimeout = 120 * time.Second
	}
	if deps.Idempotency == nil {
		deps.Idempotency = idempotency.NewStore(0)
	}
	if deps.SplitRunes <= 0 {
		deps.SplitRunes = ilink.DefaultSplitRunes
	}
	if deps.PollTimeout <= 0 {
		deps.PollTimeout = ilink.DefaultPollTimeout
	}
	if deps.HeartbeatInterval <= 0 {
		deps.HeartbeatInterval = 30 * time.Second
	}
	if deps.ProcessingNoticeAfter <= 0 {
		deps.ProcessingNoticeAfter = 15 * time.Second
	}
	if deps.DeniedReplyInterval <= 0 {
		deps.DeniedReplyInterval = 10 * time.Minute
	}
	if deps.PeerQueueSize <= 0 {
		deps.PeerQueueSize = 5
	}
	if deps.PeerIdleTimeout <= 0 {
		deps.PeerIdleTimeout = 5 * time.Minute
	}
	if deps.BackoffMin <= 0 {
		deps.BackoffMin = time.Second
	}
	return deps
}

func reportIlinkStatus(deps WeixinIlinkDeps, channelID string, body runtimeclient.StatusBody) {
	var rep StatusReporter
	if deps.Reporter != nil {
		rep = deps.Reporter
	} else if deps.Runtime != nil {
		rep = deps.Runtime
	}
	if rep == nil || channelID == "" || body.State == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := rep.ReportChannelStatus(ctx, channelID, body); err != nil {
		log.Printf("weixin_ilink %s: report status %s: %v", channelID, body.State, err)
	}
}

type ilinkQueuedMsg struct {
	in   ilink.Inbound
	key  string
	corr string
}

type ilinkRunner struct {
	ch     channel.Channel
	deps   WeixinIlinkDeps
	client *ilink.Client

	stateMu sync.Mutex
	state   string

	mu       sync.Mutex
	queues   map[string]chan ilinkQueuedMsg
	deniedAt map[string]time.Time
	// workers tracks the heartbeat and peer workers; Add is only called from the poll goroutine.
	workers sync.WaitGroup
}

// RunWeixinIlink runs one channel's long-poll loop until ctx is canceled (exported for tests).
// On -14 or a 409 generation conflict it suspends (blocks without calling iLink) until ctx is canceled.
func RunWeixinIlink(ctx context.Context, ch channel.Channel, deps WeixinIlinkDeps) {
	deps = normalizeWeixinIlinkDeps(deps)
	client := ilink.NewClient(ch.BaseURL, ch.BotToken)
	client.PollTimeout = deps.PollTimeout
	r := &ilinkRunner{
		ch:       ch,
		deps:     deps,
		client:   client,
		queues:   make(map[string]chan ilinkQueuedMsg),
		deniedAt: make(map[string]time.Time),
	}
	r.run(ctx)
}

func (r *ilinkRunner) run(ctx context.Context) {
	defer r.workers.Wait()
	buf, ok := r.loadCursor(ctx)
	if !ok {
		return
	}
	hbCtx, stopHeartbeat := context.WithCancel(ctx)
	defer stopHeartbeat()
	r.setState("connected", "")
	r.workers.Add(1)
	go r.heartbeatLoop(hbCtx)

	backoff := r.deps.BackoffMin
	for ctx.Err() == nil {
		upd, err := r.client.GetUpdates(ctx, buf)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			if ilink.IsSessionExpired(err) {
				stopHeartbeat()
				log.Printf("weixin_ilink %s: session expired (-14); suspended until re-login", r.ch.ID)
				r.reportSessionExpired(ctx)
				<-ctx.Done()
				return
			}
			r.setState("reconnecting", err.Error())
			log.Printf("weixin_ilink %s: getupdates: %v; retry in %s", r.ch.ID, err, backoff)
			if !sleepIlink(ctx, backoff) {
				return
			}
			backoff = nextIlinkBackoff(backoff)
			continue
		}
		backoff = r.deps.BackoffMin
		r.setState("connected", "")
		if !r.handleBatch(ctx, buf, upd) {
			stopHeartbeat()
			log.Printf("weixin_ilink %s: generation %d is stale; suspended until restart", r.ch.ID, r.ch.Generation)
			<-ctx.Done()
			return
		}
		buf = upd.GetUpdatesBuf
	}
}

func (r *ilinkRunner) loadCursor(ctx context.Context) (string, bool) {
	backoff := r.deps.BackoffMin
	for {
		cur, err := r.deps.Runtime.GetIlinkCursor(ctx, r.ch.ID)
		if err == nil {
			if cur.Generation != r.ch.Generation {
				return "", true
			}
			return cur.GetUpdatesBuf, true
		}
		if ctx.Err() != nil {
			return "", false
		}
		r.setState("reconnecting", err.Error())
		log.Printf("weixin_ilink %s: get cursor: %v; retry in %s", r.ch.ID, err, backoff)
		if !sleepIlink(ctx, backoff) {
			return "", false
		}
		backoff = nextIlinkBackoff(backoff)
	}
}

func (r *ilinkRunner) setState(state, lastErr string) {
	r.stateMu.Lock()
	changed := r.state != state
	r.state = state
	r.stateMu.Unlock()
	if changed {
		reportIlinkStatus(r.deps, r.ch.ID, runtimeclient.StatusBody{State: state, LastError: &lastErr})
	}
}

func (r *ilinkRunner) heartbeatLoop(ctx context.Context) {
	defer r.workers.Done()
	ticker := time.NewTicker(r.deps.HeartbeatInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			r.stateMu.Lock()
			state := r.state
			r.stateMu.Unlock()
			reportIlinkStatus(r.deps, r.ch.ID, runtimeclient.StatusBody{State: state})
		}
	}
}

func (r *ilinkRunner) reportSessionExpired(ctx context.Context) {
	backoff := r.deps.BackoffMin
	for {
		err := r.deps.Runtime.ReportIlinkSessionExpired(ctx, r.ch.ID, r.ch.Generation, "-14")
		if err == nil || ctx.Err() != nil {
			return
		}
		log.Printf("weixin_ilink %s: report session expired: %v; retry in %s", r.ch.ID, err, backoff)
		if !sleepIlink(ctx, backoff) {
			return
		}
		backoff = nextIlinkBackoff(backoff)
	}
}

// handleBatch returns false when Portal rejected this runner's generation (409).
func (r *ilinkRunner) handleBatch(ctx context.Context, prevBuf string, upd *ilink.Updates) bool {
	var msgs []ilink.Inbound
	for _, m := range upd.Msgs {
		if !m.IsUserMessage() {
			continue
		}
		in := ilink.Normalize(m)
		if in.FromUserID == "" {
			continue
		}
		if in.GroupID != "" {
			log.Printf("weixin_ilink %s: ignoring group message %s", r.ch.ID, in.MessageID)
			continue
		}
		msgs = append(msgs, in)
	}
	// Roster first: a denied peer must still appear in Portal so an admin can allow it.
	for _, in := range msgs {
		if !r.reportPeer(ctx, in) {
			return false
		}
	}
	// At-most-once: commit the cursor before any turn runs.
	if upd.GetUpdatesBuf != prevBuf {
		if !r.putCursor(ctx, upd.GetUpdatesBuf) {
			return false
		}
	}
	for _, in := range msgs {
		r.dispatch(ctx, in)
	}
	return true
}

func (r *ilinkRunner) reportPeer(ctx context.Context, in ilink.Inbound) bool {
	at := in.CreatedAt
	if at.IsZero() {
		at = time.Now()
	}
	_, err := r.deps.Runtime.ReportIlinkPeer(ctx, r.ch.ID, runtimeclient.IlinkPeerReport{
		Generation:   r.ch.Generation,
		PeerID:       in.FromUserID,
		ContextToken: in.ContextToken,
		Preview:      in.Preview(),
		At:           at.UTC().Format(time.RFC3339),
	})
	if runtimeclient.IsIlinkGenerationMismatch(err) {
		return false
	}
	if err != nil {
		log.Printf("weixin_ilink %s: report peer …%s: %v", r.ch.ID, ilinkPeerTail(in.FromUserID), err)
	}
	return true
}

func (r *ilinkRunner) putCursor(ctx context.Context, buf string) bool {
	cur := runtimeclient.IlinkCursor{Generation: r.ch.Generation, GetUpdatesBuf: buf}
	for attempt := 1; attempt <= ilinkCursorPutAttempts; attempt++ {
		err := r.deps.Runtime.PutIlinkCursor(ctx, r.ch.ID, cur)
		if err == nil {
			return true
		}
		if runtimeclient.IsIlinkGenerationMismatch(err) {
			return false
		}
		log.Printf("weixin_ilink %s: put cursor (attempt %d/%d): %v", r.ch.ID, attempt, ilinkCursorPutAttempts, err)
		if attempt < ilinkCursorPutAttempts && !sleepIlink(ctx, ilinkCursorRetryDelay) {
			return true
		}
	}
	log.Printf("weixin_ilink %s: put cursor failed; continuing (a process restart may replay this batch)", r.ch.ID)
	return true
}

func (r *ilinkRunner) dispatch(ctx context.Context, in ilink.Inbound) {
	key := ""
	if in.MessageID != "" {
		key = "ilink:" + r.ch.ID + ":" + in.MessageID
	}
	corr := newCorrelationID()
	if _, reused, _ := r.deps.Idempotency.Begin(ctx, key, corr); reused {
		return
	}
	if !r.peerAllowed(in.FromUserID) {
		r.replyDenied(ctx, in)
		_ = r.deps.Idempotency.Complete(ctx, key, "denied")
		return
	}
	if in.NonText {
		r.reply(ctx, in, ilinkNonTextReply)
		_ = r.deps.Idempotency.Complete(ctx, key, "non_text")
		return
	}
	r.enqueue(ctx, ilinkQueuedMsg{in: in, key: key, corr: corr})
}

// peerAllowed reads the Registry on every message so a Portal allowlist change applies without a restart.
func (r *ilinkRunner) peerAllowed(peer string) bool {
	if r.deps.Registry == nil {
		return false
	}
	cur, err := r.deps.Registry.Get(r.ch.ID)
	if err != nil {
		return false
	}
	return slices.Contains(cur.AllowedPeers, peer)
}

func (r *ilinkRunner) replyDenied(ctx context.Context, in ilink.Inbound) {
	now := time.Now()
	r.mu.Lock()
	last, seen := r.deniedAt[in.FromUserID]
	if seen && now.Sub(last) < r.deps.DeniedReplyInterval {
		r.mu.Unlock()
		return
	}
	r.deniedAt[in.FromUserID] = now
	r.mu.Unlock()
	r.reply(ctx, in, fmt.Sprintf(ilinkDeniedTemplate, ilinkPeerTail(in.FromUserID)))
}

func (r *ilinkRunner) reply(ctx context.Context, in ilink.Inbound, text string) {
	for _, part := range ilink.ShapeReply(text, r.deps.SplitRunes) {
		if _, err := r.client.SendText(ctx, in.FromUserID, in.ContextToken, part); err != nil {
			log.Printf("weixin_ilink %s: send to …%s: %v", r.ch.ID, ilinkPeerTail(in.FromUserID), err)
			return
		}
	}
}

func (r *ilinkRunner) enqueue(ctx context.Context, msg ilinkQueuedMsg) {
	peer := msg.in.FromUserID
	r.mu.Lock()
	q, ok := r.queues[peer]
	if !ok {
		q = make(chan ilinkQueuedMsg, r.deps.PeerQueueSize)
		r.queues[peer] = q
		r.workers.Add(1)
		go r.peerWorker(ctx, peer, q)
	}
	select {
	case q <- msg:
		r.mu.Unlock()
	default:
		r.mu.Unlock()
		r.reply(ctx, msg.in, ilinkQueueFullReply)
		_ = r.deps.Idempotency.Complete(ctx, msg.key, "queue_full")
	}
}

// peerWorker serializes one peer's messages; it exits after PeerIdleTimeout without work.
func (r *ilinkRunner) peerWorker(ctx context.Context, peer string, q chan ilinkQueuedMsg) {
	defer r.workers.Done()
	idle := time.NewTimer(r.deps.PeerIdleTimeout)
	defer idle.Stop()
	for {
		select {
		case <-ctx.Done():
			r.mu.Lock()
			if r.queues[peer] == q {
				delete(r.queues, peer)
			}
			r.mu.Unlock()
			return
		case msg := <-q:
			r.process(ctx, msg)
			idle.Reset(r.deps.PeerIdleTimeout)
		case <-idle.C:
			// enqueue sends while holding mu, so an empty queue checked under mu cannot lose a message.
			r.mu.Lock()
			if len(q) > 0 {
				r.mu.Unlock()
				idle.Reset(r.deps.PeerIdleTimeout)
				continue
			}
			if r.queues[peer] == q {
				delete(r.queues, peer)
			}
			r.mu.Unlock()
			return
		}
	}
}

// process mirrors HandleWecomMsgCallback: pending /switch → slash → auto-route → resolve → TurnsStream.
func (r *ilinkRunner) process(ctx context.Context, msg ilinkQueuedMsg) {
	deps := r.deps
	in := msg.in
	chID := r.ch.ID
	peerID := "user:" + in.FromUserID
	noticeAt := time.Now().Add(deps.ProcessingNoticeAfter)
	defer func() { _ = deps.Idempotency.Complete(context.Background(), msg.key, "done") }()
	replyText := func(text string) { r.reply(ctx, in, text) }

	turnCtx, cancel := context.WithTimeout(ctx, deps.TurnTimeout)
	defer cancel()

	if replyPendingSwitchText(turnCtx, deps.Runtime, deps.Sessions, deps.PendingSwitch, chID, peerID, in.Text, replyText) {
		return
	}
	if cmdReply, isCmd := runSlashCommand(turnCtx, deps.Runtime, deps.Sessions, deps.PendingSwitch, chID, peerID, in.Text); isCmd {
		replyText(cmdReply)
		return
	}

	plan := prepareAutoRoute(turnCtx, deps.Runtime, chID, peerID, in.Text)
	content := in.Text
	if plan.TurnText != "" {
		content = plan.TurnText
	}

	var resolved *runtimeclient.ResolveReply
	var err error
	if plan.AgentID != "" {
		resolved, err = resolveMaybeRebind(turnCtx, deps.Sessions, chID, peerID, plan.AgentID, plan.Reason)
	} else {
		resolved, err = deps.Sessions.Resolve(turnCtx, "", runtimeclient.ResolveRequest{ChannelID: chID, PeerID: peerID})
	}
	if err != nil {
		replyText(ilinkFailurePrefix + mapRuntimeUserError(err))
		return
	}

	rc, _, err := deps.Runtime.TurnsStream(turnCtx, resolved.UserID, runtimeclient.TurnRequest{
		SessionID:      resolved.SessionID,
		Content:        content,
		ChannelID:      chID,
		PeerID:         peerID,
		CorrelationID:  msg.corr,
		IdempotencyKey: msg.key,
	})
	if err != nil {
		replyText(ilinkFailurePrefix + mapRuntimeUserError(err))
		return
	}
	defer rc.Close()

	res := consumeIlinkTurnStream(turnCtx, rc, noticeAt, func() { replyText(ilinkProcessingReply) })
	if ctx.Err() != nil {
		// Runner stopped (disable / re-login): this bot's credentials are no longer ours to use.
		return
	}
	switch {
	case res.HITL:
		replyText(ilinkHITLReply)
	case res.Failed:
		if ctxErr := turnCtx.Err(); ctxErr != nil {
			replyText(ilinkFailurePrefix + mapRuntimeUserError(ctxErr))
		} else {
			replyText(ilinkFailurePrefix + mapTurnFailureMessage(res.ErrMsg, ""))
		}
	default:
		replyText(res.Content)
	}
}

type ilinkTurnResult struct {
	Content string
	Failed  bool
	HITL    bool
	ErrMsg  string
}

// consumeIlinkTurnStream collects chunk content; notice fires once if the stream is still open at noticeAt.
func consumeIlinkTurnStream(ctx context.Context, body io.Reader, noticeAt time.Time, notice func()) ilinkTurnResult {
	st := NewProgressState(time.Now())
	events := make(chan sseEvent, 16)
	go scanSSEEvents(ctx, body, events)
	timer := time.NewTimer(time.Until(noticeAt))
	defer timer.Stop()

	var full strings.Builder
	cancelled := false
	for done := false; !done; {
		select {
		case <-ctx.Done():
			cancelled, done = true, true
			if c, ok := body.(io.Closer); ok {
				_ = c.Close()
			}
		case <-timer.C:
			notice()
		case ev, ok := <-events:
			if !ok || ev.event == "done" {
				done = true
				continue
			}
			st.ApplySSEEvent(ev.event, ev.data)
			if ev.event == "chunk" {
				full.WriteString(chunkContent(ev.data))
			}
		}
	}
	// Drain so scanSSEEvents cannot leak on a blocked send after cancel.
	if cancelled {
		for range events {
		}
	}

	res := ilinkTurnResult{Content: strings.TrimSpace(full.String())}
	switch {
	case st.HITL:
		res.Failed, res.HITL = true, true
	case st.Failed:
		res.Failed, res.ErrMsg = true, st.ErrMsg
	case ctx.Err() != nil:
		res.Failed, res.ErrMsg = true, ctx.Err().Error()
	case res.Content == "":
		res.Failed, res.ErrMsg = true, "turn failed"
	}
	return res
}

func chunkContent(data []byte) string {
	var payload struct {
		Content string `json:"content"`
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		return ""
	}
	return payload.Content
}

// replyPendingSwitchText is the reply-func variant of replyPendingSwitch for plain-text channels.
func replyPendingSwitchText(ctx context.Context, rt *runtimeclient.Client, sessions *session.Router, store *pendingswitch.Store, channelID, peerID, text string, reply func(string)) bool {
	if store == nil {
		return false
	}
	ent, ok := store.Get(channelID, peerID, time.Now())
	if !ok {
		return false
	}
	text = strings.TrimSpace(text)
	if strings.HasPrefix(text, "/") {
		if cmd, isCmd := command.Parse(text); isCmd && command.PreservesPending(cmd.Kind) {
			return false
		}
		store.Delete(channelID, peerID)
		return false
	}
	if idx, isDigit := parseDigitChoice(text); isDigit && idx >= 1 && idx <= len(ent.Agents) {
		store.Delete(channelID, peerID)
		msg, err := switchChannelAgent(ctx, rt, sessions, channelID, peerID, ent.Agents[idx-1].ID)
		if err != nil {
			reply(ilinkFailurePrefix + mapRuntimeUserError(err))
			return true
		}
		reply(msg)
		return true
	}
	reply(formatPendingSwitchInvalidPrompt(len(ent.Agents)))
	return true
}

// ilinkPeerTail matches the Web roster label: last 4 chars of the part before "@".
func ilinkPeerTail(peerID string) string {
	local, _, _ := strings.Cut(peerID, "@")
	r := []rune(local)
	if len(r) <= 4 {
		return local
	}
	return string(r[len(r)-4:])
}

func sleepIlink(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}

func nextIlinkBackoff(d time.Duration) time.Duration {
	d *= 2
	if d > ilinkBackoffMax {
		d = ilinkBackoffMax
	}
	return d
}
