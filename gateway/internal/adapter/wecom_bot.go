package adapter

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"time"

	"github.com/sixath/gateway/internal/channel"
	"github.com/sixath/gateway/internal/idempotency"
	"github.com/sixath/gateway/internal/leadership"
	"github.com/sixath/gateway/internal/metrics"
	"github.com/sixath/gateway/internal/observability"
	"github.com/sixath/gateway/internal/runtimeclient"
	"github.com/sixath/gateway/internal/session"
	"github.com/sixath/gateway/internal/wecom"
)

// WecomConn is the outbound surface used by the wecom_bot runner (and tests).
type WecomConn interface {
	RespondStream(ctx context.Context, reqID, streamID, content string, finish bool) error
}

// WecomBotDeps wires long-connection runners.
type WecomBotDeps struct {
	Registry    *channel.Registry
	Runtime     *runtimeclient.Client
	Sessions    *session.Router
	Idempotency idempotency.Store
	// Leader 可选：多副本时判定本实例是否持有订阅领导权；nil 默认 AlwaysLeader（单副本）。
	Leader      leadership.Leader
	TurnTimeout time.Duration
}

const (
	wecomProcessingContent = "处理中…"
	wecomReconnectMin      = time.Second
	wecomReconnectMax      = 60 * time.Second
	leadershipPollInterval = 5 * time.Second
)

// StartWecomBots starts one reconnecting runner per enabled wecom_bot channel.
// It returns immediately; runners exit when ctx is canceled.
func StartWecomBots(ctx context.Context, deps WecomBotDeps) {
	if deps.TurnTimeout <= 0 {
		deps.TurnTimeout = 120 * time.Second
	}
	if deps.Idempotency == nil {
		deps.Idempotency = idempotency.NewStore(0)
	}
	if deps.Registry == nil {
		return
	}
	for _, ch := range deps.Registry.All() {
		if ch.Type != "wecom_bot" || !ch.Enabled {
			continue
		}
		ch := ch
		go runWecomBotLoop(ctx, ch, deps)
	}
}

func runWecomBotLoop(ctx context.Context, ch channel.Channel, deps WecomBotDeps) {
	leader := deps.Leader
	if leader == nil {
		leader = leadership.AlwaysLeader{}
	}
	backoff := wecomReconnectMin
	for {
		if ctx.Err() != nil {
			return
		}
		// 多副本预留：未持领导权时不订阅 WSS，轮询等待（单副本 AlwaysLeader 恒为 true）。
		if !leader.IsLeader(ctx) {
			observability.Logger(ctx).Warn("wecom_bot_not_leader",
				"channel", ch.ID, "retry_in", leadershipPollInterval.String())
			select {
			case <-ctx.Done():
				return
			case <-time.After(leadershipPollInterval):
			}
			continue
		}
		started := time.Now()
		err := runWecomBotOnce(ctx, ch, deps)
		if ctx.Err() != nil {
			return
		}
		if time.Since(started) > 30*time.Second {
			backoff = wecomReconnectMin
		}
		metrics.IncWecomReconnect(ch.ID)
		observability.Logger(ctx).Warn("wecom_bot_disconnected",
			"channel", ch.ID, "err", err, "reconnect_in", backoff.String())
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
		next := backoff * 2
		if next > wecomReconnectMax {
			next = wecomReconnectMax
		}
		backoff = next
	}
}

func runWecomBotOnce(ctx context.Context, ch channel.Channel, deps WecomBotDeps) error {
	dir := wecom.NewDirectory(wecom.DirectoryConfig{
		CorpID: ch.CorpID,
		Secret: ch.CorpSecret,
	})
	var client *wecom.Client
	client = wecom.NewClient(wecom.ClientConfig{
		URL:    ch.WSURL,
		BotID:  ch.BotID,
		Secret: ch.Secret,
		OnMessage: func(reqID string, body json.RawMessage) {
			go handleWecomRawMessage(context.Background(), client, reqID, ch, body, deps, dir)
		},
	})
	metrics.SetWecomConnections(ch.ID, 1)
	defer metrics.SetWecomConnections(ch.ID, 0)
	return client.Run(ctx)
}

func handleWecomRawMessage(parent context.Context, conn WecomConn, reqID string, ch channel.Channel, body json.RawMessage, deps WecomBotDeps, dir *wecom.Directory) {
	n, err := wecom.NormalizeMsgBody(body, wecom.NormalizeOpts{
		BotNames: ch.BotNames,
		BotID:    ch.BotID,
	})
	if err != nil {
		observability.Logger(parent).Warn("wecom_bot_normalize_failed", "channel", ch.ID, "err", err)
		return
	}
	timeout := deps.TurnTimeout
	if timeout <= 0 {
		timeout = 120 * time.Second
	}
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()
	if dir != nil {
		if name := dir.ResolveDisplayName(ctx, n.AskerID); name != "" {
			n = n.WithAskerName(name)
		}
	}
	HandleWecomMsgCallback(ctx, conn, reqID, ch, n, deps)
}

// HandleWecomMsgCallback processes one normalized text callback (exported for unit tests).
func HandleWecomMsgCallback(ctx context.Context, conn WecomConn, reqID string, ch channel.Channel, n wecom.Normalized, deps WecomBotDeps) {
	if deps.Idempotency == nil {
		deps.Idempotency = idempotency.NewStore(0)
	}
	corr := newCorrelationID()
	if _, reused, berr := deps.Idempotency.Begin(ctx, n.MsgID, corr); berr != nil {
		observability.Logger(ctx).Error("idempotency_begin_failed", "channel", ch.ID, "err", berr)
	} else if reused {
		// Duplicate msgid: do not respond again.
		metrics.ObserveIdempotency(ch.ID, "duplicate")
		return
	}
	metrics.ObserveIdempotency(ch.ID, "new")

	streamID := streamIDFromMsgID(n.MsgID)
	if err := conn.RespondStream(ctx, reqID, streamID, wecomProcessingContent, false); err != nil {
		observability.Logger(ctx).Warn("wecom_respond_processing_failed", "channel", ch.ID, "err", err)
	}

	agentID := ch.DefaultAgent
	resolved, err := deps.Sessions.Resolve(ctx, "", runtimeclient.ResolveRequest{
		ChannelID: ch.ID,
		PeerID:    n.PeerID,
		AgentID:   agentID,
	})
	if err != nil {
		failMsg := err.Error()
		respondErr := conn.RespondStream(ctx, reqID, streamID, wecom.FormatFailureCard(n.AskerName, n.QuestionText, failMsg), true)
		metrics.ObserveTurn(ch.ID, "failed", 0)
		metrics.ObserveReply(ch.ID, "wecom_card", respondErr)
		_ = deps.Idempotency.Complete(ctx, n.MsgID, failMsg)
		return
	}

	start := time.Now()
	out, err := deps.Runtime.TurnsFinal(ctx, resolved.UserID, runtimeclient.TurnRequest{
		SessionID:      resolved.SessionID,
		Content:        n.RuntimeContent,
		ChannelID:      ch.ID,
		PeerID:         n.PeerID,
		CorrelationID:  corr,
		IdempotencyKey: n.MsgID,
	})
	if err != nil {
		failMsg := err.Error()
		respondErr := conn.RespondStream(ctx, reqID, streamID, wecom.FormatFailureCard(n.AskerName, n.QuestionText, failMsg), true)
		metrics.ObserveTurn(ch.ID, "failed", time.Since(start))
		metrics.ObserveReply(ch.ID, "wecom_card", respondErr)
		_ = deps.Idempotency.Complete(ctx, n.MsgID, failMsg)
		return
	}

	status := out.Status
	if status == "" {
		status = "ok"
	}
	if status == "failed" {
		failMsg := out.Error
		if failMsg == "" {
			failMsg = out.Content
		}
		if failMsg == "" {
			failMsg = "turn failed"
		}
		respondErr := conn.RespondStream(ctx, reqID, streamID, wecom.FormatFailureCard(n.AskerName, n.QuestionText, failMsg), true)
		metrics.ObserveTurn(ch.ID, "failed", time.Since(start))
		metrics.ObserveReply(ch.ID, "wecom_card", respondErr)
		_ = deps.Idempotency.Complete(ctx, n.MsgID, failMsg)
		return
	}

	card := wecom.FormatReplyCard(n.AskerName, n.QuestionText, out.Content)
	err = conn.RespondStream(ctx, reqID, streamID, card, true)
	if err != nil {
		observability.Logger(ctx).Warn("wecom_respond_final_failed", "channel", ch.ID, "err", err)
	}
	metrics.ObserveTurn(ch.ID, "ok", time.Since(start))
	metrics.ObserveReply(ch.ID, "wecom_card", err)
	_ = deps.Idempotency.Complete(ctx, n.MsgID, card)
}

func streamIDFromMsgID(msgID string) string {
	sum := sha256.Sum256([]byte(msgID))
	return hex.EncodeToString(sum[:16])
}
