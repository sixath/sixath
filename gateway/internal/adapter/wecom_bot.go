package adapter

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"log"
	"strings"
	"time"

	"github.com/sixath/gateway/internal/channel"
	"github.com/sixath/gateway/internal/idempotency"
	"github.com/sixath/gateway/internal/pendingswitch"
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
	Registry      *channel.Registry
	Runtime       *runtimeclient.Client
	Sessions      *session.Router
	Idempotency   *idempotency.Store
	PendingSwitch *pendingswitch.Store
	TurnTimeout   time.Duration
	// Reporter posts runtime status; when nil, Runtime is used if non-nil.
	Reporter StatusReporter
	// RunOnce overrides a single connect attempt (tests); nil uses real WS dial.
	RunOnce func(ctx context.Context, ch channel.Channel, deps WecomBotDeps) error
}

const (
	wecomProcessingContent = "处理中…"
	wecomReconnectMin      = time.Second
	wecomReconnectMax      = 60 * time.Second
	wecomStatusHeartbeat   = 30 * time.Second
)

func (d WecomBotDeps) statusReporter() StatusReporter {
	if d.Reporter != nil {
		return d.Reporter
	}
	if d.Runtime != nil {
		return d.Runtime
	}
	return nil
}

func reportChannelStatus(deps WecomBotDeps, channelID string, body runtimeclient.StatusBody) {
	rep := deps.statusReporter()
	if rep == nil || channelID == "" || body.State == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := rep.ReportChannelStatus(ctx, channelID, body); err != nil {
		log.Printf("wecom_bot %s: report status %s: %v", channelID, body.State, err)
	}
}

func runWecomBotLoop(ctx context.Context, ch channel.Channel, deps WecomBotDeps) {
	deps = normalizeWecomBotDeps(deps)
	once := deps.RunOnce
	if once == nil {
		once = runWecomBotOnce
	}
	backoff := wecomReconnectMin
	attempt := 0
	for {
		if ctx.Err() != nil {
			return
		}
		started := time.Now()
		err := once(ctx, ch, deps)
		if ctx.Err() != nil {
			return
		}
		attempt++
		errMsg := ""
		if err != nil {
			errMsg = err.Error()
		}
		reportChannelStatus(deps, ch.ID, runtimeclient.StatusBody{
			State:     "disconnected",
			LastError: &errMsg,
		})
		reconnectMs := int(backoff / time.Millisecond)
		reportChannelStatus(deps, ch.ID, runtimeclient.StatusBody{
			State:            "reconnecting",
			LastError:        &errMsg,
			ReconnectAttempt: &attempt,
			ReconnectInMs:    &reconnectMs,
		})
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

func reportWecomConnected(deps WecomBotDeps, channelID string) {
	zero := 0
	empty := ""
	reportChannelStatus(deps, channelID, runtimeclient.StatusBody{
		State:            "connected",
		LastError:        &empty,
		ReconnectAttempt: &zero,
		ReconnectInMs:    &zero,
	})
}

func runWecomBotOnce(ctx context.Context, ch channel.Channel, deps WecomBotDeps) error {
	dir := wecom.NewDirectory(wecom.DirectoryConfig{
		CorpID: ch.CorpID,
		Secret: ch.CorpSecret,
	})
	connCtx, cancelHeartbeat := context.WithCancel(ctx)
	defer cancelHeartbeat()

	var client *wecom.Client
	client = wecom.NewClient(wecom.ClientConfig{
		URL:    ch.WSURL,
		BotID:  ch.BotID,
		Secret: ch.Secret,
		OnConnected: func() {
			reportWecomConnected(deps, ch.ID)
			go wecomStatusHeartbeatLoop(connCtx, deps, ch.ID)
		},
		OnMessage: func(reqID string, body json.RawMessage) {
			go handleWecomRawMessage(context.Background(), client, reqID, ch, body, deps, dir)
		},
	})
	return client.Run(ctx)
}

func wecomStatusHeartbeatLoop(ctx context.Context, deps WecomBotDeps, channelID string) {
	ticker := time.NewTicker(wecomStatusHeartbeat)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			reportChannelStatus(deps, channelID, runtimeclient.StatusBody{State: "connected"})
		}
	}
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

	if deps.PendingSwitch != nil {
		if ent, ok := deps.PendingSwitch.Get(ch.ID, n.PeerID, time.Now()); ok {
			text := strings.TrimSpace(n.QuestionText)
			if strings.HasPrefix(text, "/") {
				deps.PendingSwitch.Delete(ch.ID, n.PeerID)
			} else if idx, isDigit := parseDigitChoice(text); isDigit {
				if idx < 1 || idx > len(ent.Agents) {
					reply := formatPendingSwitchInvalidPrompt(len(ent.Agents))
					card := wecom.FormatReplyCard(n.AskerName, n.QuestionText, reply)
					_ = conn.RespondStream(ctx, reqID, streamID, card, true)
					deps.Idempotency.Complete(n.MsgID, card)
					return
				}
				agentID := ent.Agents[idx-1].ID
				deps.PendingSwitch.Delete(ch.ID, n.PeerID)
				msg, err := switchChannelAgent(ctx, deps.Runtime, deps.Sessions, ch.ID, n.PeerID, agentID)
				if err != nil {
					failMsg := mapRuntimeUserError(err)
					_ = conn.RespondStream(ctx, reqID, streamID, wecom.FormatFailureCard(n.AskerName, n.QuestionText, failMsg), true)
					deps.Idempotency.Complete(n.MsgID, failMsg)
					return
				}
				card := wecom.FormatReplyCard(n.AskerName, n.QuestionText, msg)
				_ = conn.RespondStream(ctx, reqID, streamID, card, true)
				deps.Idempotency.Complete(n.MsgID, card)
				return
			} else {
				reply := formatPendingSwitchInvalidPrompt(len(ent.Agents))
				card := wecom.FormatReplyCard(n.AskerName, n.QuestionText, reply)
				_ = conn.RespondStream(ctx, reqID, streamID, card, true)
				deps.Idempotency.Complete(n.MsgID, card)
				return
			}
		}
	}

	if cmdReply, isCmd := runSlashCommand(ctx, deps.Runtime, deps.Sessions, deps.PendingSwitch, ch.ID, n.PeerID, n.QuestionText); isCmd {
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
