package agent

import (
	"context"
	"errors"
	"sync"
)

// 主动取消（docs/superpowers/plans/2026-09-12-maturity-hardening.md Task 12）
//
// 目标：把「取消」从"只能靠父 ctx 超时/断连、且会伪装成 error 事件"变成一等语义：
//   - 调用方可用 request_id 主动中断在途 Run（Cancel）；
//   - 取消以 StreamEventCancelled 上报，而不是 StreamEventError；
//   - 已产生的增量文本与工具结果保留（不因取消而丢弃）；
//   - Run 结束自动注销句柄，不留泄漏。

// ErrRunNotFound 表示没有与给定 request_id 匹配的在途 Run（可能尚未开始或已结束）。
var ErrRunNotFound = errors.New("agent run not found")

// CancelableAgent 支持按 request_id 主动取消在途 Run。
type CancelableAgent interface {
	Agent
	// Cancel 中断指定 request_id 的 Run；未找到返回 ErrRunNotFound。
	Cancel(runID string) error
}

// runCancelState 为单次 Run 的取消状态：由 Cancel 置位后触发 cancel。
type runCancelState struct {
	cancel context.CancelFunc

	mu        sync.Mutex
	byRequest bool
}

func (s *runCancelState) markRequested() {
	s.mu.Lock()
	s.byRequest = true
	s.mu.Unlock()
}

// requestedByUser 报告取消是否来自 Cancel API（否则为父 ctx 取消/断连）。
func (s *runCancelState) requestedByUser() bool {
	if s == nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.byRequest
}

// runCanceler 维护在途 Run 的取消句柄。
type runCanceler struct {
	mu   sync.Mutex
	runs map[string]*runCancelState
}

func newRunCanceler() *runCanceler {
	return &runCanceler{runs: make(map[string]*runCancelState)}
}

// register 为 runID 注册取消句柄；runID 为空时返回 nil（该 Run 不可主动取消）。
func (c *runCanceler) register(runID string, cancel context.CancelFunc) *runCancelState {
	if c == nil || runID == "" || cancel == nil {
		return nil
	}
	st := &runCancelState{cancel: cancel}
	c.mu.Lock()
	if c.runs == nil {
		c.runs = make(map[string]*runCancelState)
	}
	c.runs[runID] = st
	c.mu.Unlock()
	return st
}

// unregister 在 Run 结束时注销；仅当句柄未被新的同名 Run 覆盖时删除。
func (c *runCanceler) unregister(runID string, st *runCancelState) {
	if c == nil || runID == "" || st == nil {
		return
	}
	c.mu.Lock()
	if cur, ok := c.runs[runID]; ok && cur == st {
		delete(c.runs, runID)
	}
	c.mu.Unlock()
}

// cancel 触发取消；false 表示该 runID 不在途。
func (c *runCanceler) cancel(runID string) bool {
	if c == nil || runID == "" {
		return false
	}
	c.mu.Lock()
	st, ok := c.runs[runID]
	c.mu.Unlock()
	if !ok || st == nil {
		return false
	}
	st.markRequested()
	st.cancel()
	return true
}

// inflight 返回当前在途 Run 数（测试用于确认无句柄泄漏）。
func (c *runCanceler) inflight() int {
	if c == nil {
		return 0
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.runs)
}
