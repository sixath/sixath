package adapter

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"log"
	"time"

	"github.com/sixath/gateway/internal/channel"
	"github.com/sixath/gateway/internal/idempotency"
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
	Idempotency *idempotency.Store
	TurnTimeout time.Duration
}

const (
	wecomProcessingContent = "处理中…"
	wecomReconnectMin      = time.Second
	wecomReconnectMax      = 60 * time.Second
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
	backoff := wecomReconnectMin
	for {
		if ctx.Err() != nil {
			return
		}
		started := time.Now()
		err := runWecomBotOnce(ctx, ch, deps)
		if ctx.Err() != nil {
			return
		}
		if time.Since(started) > 30*time.Second {
			backoff = wecomReconnectMin
		}
		log.Printf("wecom_bot %s disconnected: %v; reconnect in %s", ch.ID, err, backoff)
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
	return client.Run(ctx)
}

func handleWecomRawMessage(parent context.Context, conn WecomConn, reqID string, ch channel.Channel, body json.RawMessage, deps WecomBotDeps, dir *wecom.Directory) {
	n, err := wecom.NormalizeMsgBody(body, wecom.NormalizeOpts{
		BotNames: ch.BotNames,
		BotID:    ch.BotID,
	})
	if err != nil {
		log.Printf("wecom_bot %s normalize: %v", ch.ID, err)
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
	if _, ok := deps.Idempotency.Begin(n.MsgID, corr); !ok {
		// Duplicate msgid: do not respond again.
		return
	}

	streamID := streamIDFromMsgID(n.MsgID)
	if err := conn.RespondStream(ctx, reqID, streamID, wecomProcessingContent, false); err != nil {
		log.Printf("wecom_bot %s respond processing: %v", ch.ID, err)
	}

	if cmdReply, isCmd := runSlashCommand(ctx, deps.Runtime, deps.Sessions, ch.ID, n.PeerID, n.QuestionText); isCmd {
		card := wecom.FormatReplyCard(n.AskerName, n.QuestionText, cmdReply)
		_ = conn.RespondStream(ctx, reqID, streamID, card, true)
		deps.Idempotency.Complete(n.MsgID, card)
		return
	}

	// Portal owns default/allowlist; do not send yaml default_agent.
	resolved, err := deps.Sessions.Resolve(ctx, "", runtimeclient.ResolveRequest{
		ChannelID: ch.ID,
		PeerID:    n.PeerID,
	})
	if err != nil {
		failMsg := mapRuntimeUserError(err)
		_ = conn.RespondStream(ctx, reqID, streamID, wecom.FormatFailureCard(n.AskerName, n.QuestionText, failMsg), true)
		deps.Idempotency.Complete(n.MsgID, failMsg)
		return
	}

	out, err := deps.Runtime.TurnsFinal(ctx, resolved.UserID, runtimeclient.TurnRequest{
		SessionID:      resolved.SessionID,
		Content:        n.RuntimeContent,
		ChannelID:      ch.ID,
		PeerID:         n.PeerID,
		CorrelationID:  corr,
		IdempotencyKey: n.MsgID,
	})
	if err != nil {
		failMsg := mapRuntimeUserError(err)
		_ = conn.RespondStream(ctx, reqID, streamID, wecom.FormatFailureCard(n.AskerName, n.QuestionText, failMsg), true)
		deps.Idempotency.Complete(n.MsgID, failMsg)
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
		_ = conn.RespondStream(ctx, reqID, streamID, wecom.FormatFailureCard(n.AskerName, n.QuestionText, failMsg), true)
		deps.Idempotency.Complete(n.MsgID, failMsg)
		return
	}

	card := wecom.FormatReplyCard(n.AskerName, n.QuestionText, out.Content)
	if err := conn.RespondStream(ctx, reqID, streamID, card, true); err != nil {
		log.Printf("wecom_bot %s respond final: %v", ch.ID, err)
	}
	deps.Idempotency.Complete(n.MsgID, card)
}

func streamIDFromMsgID(msgID string) string {
	sum := sha256.Sum256([]byte(msgID))
	return hex.EncodeToString(sum[:16])
}
