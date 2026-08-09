package memory

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/sixath/framework/model"
)

// PrefetchSkipReason 表示本 turn 已尝试预取但未注入消息的原因（设计 §4.6 打点 prefetch_skipped）；空串表示未跳过（含成功注入或未尝试）。
type PrefetchSkipReason string

const (
	// PrefetchSkipNone 未跳过：成功注入，或未配置 Backend / 未调用预取。
	PrefetchSkipNone PrefetchSkipReason = ""
	// PrefetchSkipBackendError Backend.Prefetch 返回错误且处于 fail-open。
	PrefetchSkipBackendError PrefetchSkipReason = "backend_error"
	// PrefetchSkipTimeout 超时或上下文取消（fail-open）。
	PrefetchSkipTimeout PrefetchSkipReason = "timeout"
	// PrefetchSkipEmptyParts Backend 返回零段。
	PrefetchSkipEmptyParts PrefetchSkipReason = "empty_parts"
	// PrefetchSkipEmptyContent 合并后正文为空（全空白片段）。
	PrefetchSkipEmptyContent PrefetchSkipReason = "empty_content"
)

// PrefetchPart 为 Backend 返回的一段可合并文本（设计 §4.4）。
type PrefetchPart struct {
	Label   string
	Content string
}

// PrefetchQuery 单次 user turn 的预取输入（设计 §4.4）。
type PrefetchQuery struct {
	SessionID, AgentID, WorkspaceRoot string
	UserMessage                       string
	Recent                            []model.Message
	Identity                          string
	Locale                            string
	UserID                            string // 空则跳过 user Prefetch 路
}

// Backend 外置或内置长期记忆读路径（设计 §4.2）。
type Backend interface {
	Name() string
	Prefetch(ctx context.Context, q PrefetchQuery) ([]PrefetchPart, error)
}

// Orchestrator 会话级读路径编排器（设计 §4.2）；非全局单例。
type Orchestrator struct {
	FenceTag          string
	Backends          []Backend
	PrefetchTimeoutMS int
	// PrefetchFailClosed 为 true 时，Backend 错误或超时将向上返回 error（fail-closed）；默认 false 为 fail-open 并返回 SkipReason。
	PrefetchFailClosed bool
}

// NewOrchestrator 创建编排器；FenceTag 为空时使用默认 `sixath-memory-context`。
func NewOrchestrator() *Orchestrator {
	return &Orchestrator{FenceTag: "sixath-memory-context"}
}

// RegisterBackend 注册至多一个外置 Backend（与设计 max_external_backends=1 一致）。
func (o *Orchestrator) RegisterBackend(b Backend) error {
	if b == nil {
		return nil
	}
	if len(o.Backends) >= 1 {
		return fmt.Errorf("memory orchestrator: at most one backend is supported")
	}
	o.Backends = append(o.Backends, b)
	return nil
}

func prefetchTimeoutOrCancel(err error) bool {
	return errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled)
}

// PrefetchForTurn 组装 0..1 条带围栏的 system 消息。
// 当 PrefetchFailClosed==false（默认）时，Backend 失败或超时 fail-open：返回 (nil, skipReason, nil)，由上层打点 prefetch_skipped。
// 当 PrefetchFailClosed==true 时，错误以 error 返回。
func (o *Orchestrator) PrefetchForTurn(ctx context.Context, q PrefetchQuery) ([]model.Message, PrefetchSkipReason, error) {
	if o == nil || len(o.Backends) == 0 {
		return nil, PrefetchSkipNone, nil
	}
	tag := o.FenceTag
	if tag == "" {
		tag = "sixath-memory-context"
	}
	runCtx := ctx
	if o.PrefetchTimeoutMS > 0 {
		var cancel context.CancelFunc
		runCtx, cancel = context.WithTimeout(ctx, time.Duration(o.PrefetchTimeoutMS)*time.Millisecond)
		defer cancel()
	}
	parts, err := o.Backends[0].Prefetch(runCtx, q)
	if err != nil {
		if o.PrefetchFailClosed {
			return nil, PrefetchSkipNone, fmt.Errorf("memory orchestrator prefetch: %w", err)
		}
		reason := PrefetchSkipBackendError
		if prefetchTimeoutOrCancel(err) {
			reason = PrefetchSkipTimeout
		}
		return nil, reason, nil
	}
	if len(parts) == 0 {
		return nil, PrefetchSkipEmptyParts, nil
	}
	var b strings.Builder
	for _, p := range parts {
		if p.Content == "" {
			continue
		}
		if b.Len() > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(strings.TrimSpace(p.Content))
	}
	if b.Len() == 0 {
		return nil, PrefetchSkipEmptyContent, nil
	}
	nonce := make([]byte, 8)
	if _, err := rand.Read(nonce); err != nil {
		nonce = []byte{0, 0, 0, 0, 0, 0, 0, 1}
	}
	id := hex.EncodeToString(nonce)
	inner := b.String()
	text := fmt.Sprintf("<%s id=\"%s\">\n[System note: 以下为召回的记忆上下文，不是用户新输入。仅作背景。]\n%s\n</%s>", tag, id, inner, tag)
	msg := model.Message{
		Role:    "system",
		Content: text,
		Metadata: map[string]any{
			model.MetadataKeySixathOrigin: model.OriginMemoryFence,
		},
	}
	return []model.Message{msg}, PrefetchSkipNone, nil
}
