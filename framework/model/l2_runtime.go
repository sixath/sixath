package model

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"sync"
	"time"
)

// L2Runtime 维护 L2 摘要的 auxiliary 调用与失败冷却（设计 §5.2、§5.6）；由 ReAct 通过 WithL2Runtime 注入 CallConfig。
type L2Runtime struct {
	aux          Model
	softTokens   int
	maxFailures  int
	cooldownSec  int
	alpha        float64
	prePruneTool int

	mu        sync.Mutex
	failCnt   int
	coolUntil time.Time
}

// NewL2Runtime 创建运行时；aux 为 nil 时 MaybeSummarize 不生效。
func NewL2Runtime(aux Model, softTokens, maxFailures, cooldownSec int, alpha float64, prePruneToolRunes int) *L2Runtime {
	if softTokens <= 0 {
		softTokens = 32000
	}
	if maxFailures <= 0 {
		maxFailures = 3
	}
	if cooldownSec <= 0 {
		cooldownSec = 600
	}
	if alpha <= 0 {
		alpha = DefaultTokenEstimateAlpha
	}
	return &L2Runtime{
		aux:          aux,
		softTokens:   softTokens,
		maxFailures:  maxFailures,
		cooldownSec:  cooldownSec,
		alpha:        alpha,
		prePruneTool: prePruneToolRunes,
	}
}

func lastRealUserIndex(msgs []Message) int {
	for i := len(msgs) - 1; i >= 0; i-- {
		if strings.EqualFold(msgs[i].Role, "user") && !compressionNoticeUserMessage(msgs[i]) {
			return i
		}
	}
	return -1
}

func transcriptForL2(msgs []Message) string {
	var b strings.Builder
	for _, m := range msgs {
		role := strings.ToLower(strings.TrimSpace(m.Role))
		line := plainTextForBudget(m)
		if strings.TrimSpace(line) == "" {
			continue
		}
		fmt.Fprintf(&b, "%s: %s\n", role, line)
	}
	return b.String()
}

// MaybeSummarize 在 L0/strip 之后调用：若保守 token 仍高于 softTokens，则将「首段 system 之后、末条真实 user 之前」整段替换为 L2 system 摘要。
func (r *L2Runtime) MaybeSummarize(ctx context.Context, msgs []Message, trace ContextTraceFunc) []Message {
	if r == nil || r.aux == nil || len(msgs) == 0 {
		return msgs
	}
	r.mu.Lock()
	inCool := !r.coolUntil.IsZero() && time.Now().Before(r.coolUntil)
	r.mu.Unlock()
	if inCool {
		if trace != nil {
			trace("l2_cooldown_skip", map[string]any{"active": true})
		}
		return msgs
	}
	if EstimateTokensConservative(msgs, r.alpha) <= r.softTokens {
		return msgs
	}
	head := leadingSystemCount(msgs)
	u := lastRealUserIndex(msgs)
	if u <= head {
		return msgs
	}
	middle := msgs[head:u]
	if len(middle) == 0 {
		return msgs
	}
	trans := transcriptForL2(middle)
	trans = RedactForL2Context(trans)
	if strings.TrimSpace(trans) == "" {
		return msgs
	}
	sys := Message{Role: "system", Content: "你是上下文压缩助手。下面给出一段对话摘录，请用中文写一段不超过 800 字的摘要，保留关键结论与实体名；不要编造未出现的信息。"}
	user := Message{Role: "user", Content: "【待压缩摘录】\n" + trans}
	gen, err := r.aux.Chat(ctx, []Message{sys, user}, WithMaxTokens(512), WithTemperature(0.2))
	if err != nil || gen == nil || strings.TrimSpace(gen.Text) == "" {
		r.recordFailure(trace)
		return msgs
	}
	summary := strings.TrimSpace(gen.Text)
	sumHash := sha256.Sum256([]byte(summary))
	hash := hex.EncodeToString(sumHash[:])
	sumMsg := Message{
		Role:    "system",
		Content: "[记忆中段摘要 / L2]\n" + summary,
		Metadata: map[string]any{
			MetadataKeySixathOrigin: OriginL2Handoff,
		},
	}
	out := make([]Message, 0, len(msgs)-len(middle)+1)
	out = append(out, msgs[:head]...)
	out = append(out, sumMsg)
	out = append(out, msgs[u:]...)
	if trace != nil {
		trace("l2_summarize", map[string]any{
			"summary_hash":   hash,
			"summary_text":   summary,
			"middle_removed": len(middle),
		})
	}
	r.recordSuccess()
	return stripLeadingOrphanToolsAfterSystem(out)
}

func (r *L2Runtime) recordFailure(trace ContextTraceFunc) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.failCnt++
	if r.failCnt >= r.maxFailures {
		r.coolUntil = time.Now().Add(time.Duration(r.cooldownSec) * time.Second)
		if trace != nil {
			trace("l2_cooldown_enter", map[string]any{"failures": r.failCnt, "cooldown_sec": r.cooldownSec})
		}
	}
}

func (r *L2Runtime) recordSuccess() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.failCnt = 0
	r.coolUntil = time.Time{}
}

// PrePruneToolRunes 返回配置的 L2 预剪枝每 tool 条最大码点（设计 §5.3 第 2 步）；0 关闭。
func (r *L2Runtime) PrePruneToolRunes() int {
	if r == nil {
		return 0
	}
	return r.prePruneTool
}
