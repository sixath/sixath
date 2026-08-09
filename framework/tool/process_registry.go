package tool

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

const (
	processStatusRunning = "running"
	processStatusExited  = "exited"
	processStatusKilled  = "killed"
	processStatusTimeout = "timed_out"
	processDefaultMaxOut = 50 * 1024
)

// ProcessNotifyEvent is fired when a background process with notify_on_complete finishes.
type ProcessNotifyEvent struct {
	ChatSessionID string
	ProcessID     string
	Command       string
	Status        string
	ExitCode      int
}

// ProcessNotifyHandler is invoked asynchronously after a notified process finishes.
type ProcessNotifyHandler func(ProcessNotifyEvent)

// ProcessRegistry tracks background terminal processes for a portal/process lifetime.
type ProcessRegistry struct {
	mu      sync.Mutex
	byID    map[string]*managedProcess
	maxOut  int
	idGen   func() (string, error)
	onNotify ProcessNotifyHandler
}

// NewProcessRegistry creates an empty registry.
func NewProcessRegistry() *ProcessRegistry {
	return &ProcessRegistry{
		byID:   make(map[string]*managedProcess),
		maxOut: processDefaultMaxOut,
		idGen:  randomProcessID,
	}
}

// SetNotifyHandler registers a callback for notify_on_complete finishes (nil clears).
func (r *ProcessRegistry) SetNotifyHandler(h ProcessNotifyHandler) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.onNotify = h
}

func (r *ProcessRegistry) fireNotify(ev ProcessNotifyEvent) {
	r.mu.Lock()
	h := r.onNotify
	r.mu.Unlock()
	if h != nil {
		h(ev)
	}
}

func randomProcessID() (string, error) {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return "proc_" + hex.EncodeToString(b), nil
}

type managedProcess struct {
	mu sync.Mutex

	ID               string
	ChatSessionID    string
	Command          string
	Workdir          string
	Status           string
	ExitCode         int
	NotifyOnComplete bool
	NotifyPending    bool // set when finished with notify_on_complete
	StartedAt        time.Time
	FinishedAt       time.Time
	PTY              bool

	stdout bytes.Buffer
	stderr bytes.Buffer
	maxOut int

	cmd         *exec.Cmd
	process     *os.Process // set for both pipe and pty modes
	ptySession  io.Closer
	stdin       io.WriteCloser
	stdinClosed bool
	cancel      context.CancelFunc
	done        chan struct{}
	finish      sync.Once
}

// StartSpawner is used by tests to inject a fake command runner; nil uses os/exec.
type processStartRequest struct {
	ChatSessionID    string
	Command          string
	RawArgs          []string // if set, run argv directly (no shell); overrides Command
	Workdir          string
	TimeoutSec       int
	NotifyOnComplete bool
	MaxOutputBytes   int
	PTY              bool // allocate a real PTY (Unix pty / Windows ConPTY)
}

// Start launches a background shell command. Returns Hermes-style process session_id.
func (r *ProcessRegistry) Start(req processStartRequest) (string, error) {
	if r == nil {
		return "", errors.New("process registry is nil")
	}
	command := strings.TrimSpace(req.Command)
	if command == "" && len(req.RawArgs) == 0 {
		return "", errors.New("command is required")
	}
	if command == "" && len(req.RawArgs) > 0 {
		command = strings.Join(req.RawArgs, " ")
	}
	id, err := r.idGen()
	if err != nil {
		return "", err
	}
	maxOut := req.MaxOutputBytes
	if maxOut <= 0 {
		maxOut = r.maxOut
		if maxOut <= 0 {
			maxOut = processDefaultMaxOut
		}
	}

	ctx := context.Background()
	var cancel context.CancelFunc
	if req.TimeoutSec > 0 {
		ctx, cancel = context.WithTimeout(ctx, time.Duration(req.TimeoutSec)*time.Second)
	} else {
		ctx, cancel = context.WithCancel(ctx)
	}

	if req.PTY {
		return r.startPTY(req, id, ctx, cancel, maxOut, command)
	}

	var cmd *exec.Cmd
	if len(req.RawArgs) > 0 {
		cmd = exec.CommandContext(ctx, req.RawArgs[0], req.RawArgs[1:]...)
	} else {
		shell, args := localShellInvocation(command)
		cmd = exec.CommandContext(ctx, shell, args...)
	}
	cmd.Dir = req.Workdir

	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return "", err
	}
	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		cancel()
		return "", err
	}
	stdinPipe, err := cmd.StdinPipe()
	if err != nil {
		cancel()
		return "", err
	}

	p := &managedProcess{
		ID:               id,
		ChatSessionID:    req.ChatSessionID,
		Command:          command,
		Workdir:          req.Workdir,
		Status:           processStatusRunning,
		NotifyOnComplete: req.NotifyOnComplete,
		StartedAt:        time.Now(),
		maxOut:           maxOut,
		cmd:              cmd,
		stdin:            stdinPipe,
		cancel:           cancel,
		done:             make(chan struct{}),
	}

	if err := cmd.Start(); err != nil {
		cancel()
		return "", err
	}
	p.process = cmd.Process

	go copyCapped(stdoutPipe, &p.stdout, &p.mu, maxOut)
	go copyCapped(stderrPipe, &p.stderr, &p.mu, maxOut)
	go func() {
		err := cmd.Wait()
		var notify *ProcessNotifyEvent
		p.finish.Do(func() {
			p.mu.Lock()
			defer p.mu.Unlock()
			p.FinishedAt = time.Now()
			if ctx.Err() == context.DeadlineExceeded && p.Status == processStatusRunning {
				p.Status = processStatusTimeout
				p.ExitCode = -1
			} else if p.Status == processStatusRunning {
				p.Status = processStatusExited
				p.ExitCode = 0
				if err != nil {
					p.ExitCode = -1
					var exitErr *exec.ExitError
					if errors.As(err, &exitErr) {
						p.ExitCode = exitErr.ExitCode()
					}
				}
			}
			if p.NotifyOnComplete {
				p.NotifyPending = true
				notify = &ProcessNotifyEvent{
					ChatSessionID: p.ChatSessionID,
					ProcessID:     p.ID,
					Command:       p.Command,
					Status:        p.Status,
					ExitCode:      p.ExitCode,
				}
			}
			close(p.done)
		})
		p.mu.Lock()
		if p.stdin != nil && !p.stdinClosed {
			_ = p.stdin.Close()
			p.stdinClosed = true
		}
		p.mu.Unlock()
		if notify != nil {
			r.fireNotify(*notify)
		}
	}()

	r.mu.Lock()
	r.byID[id] = p
	r.mu.Unlock()
	return id, nil
}

// Write sends raw data to process stdin without appending a newline.
func (r *ProcessRegistry) Write(id, data string) (map[string]any, error) {
	return r.writeStdin(id, data, false)
}

// Submit sends data followed by a newline (Enter) to process stdin.
func (r *ProcessRegistry) Submit(id, data string) (map[string]any, error) {
	return r.writeStdin(id, data+"\n", false)
}

// CloseStdin closes process stdin (EOF). Idempotent.
func (r *ProcessRegistry) CloseStdin(id string) (map[string]any, error) {
	p, err := r.get(id)
	if err != nil {
		return nil, err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.Status != processStatusRunning {
		m := p.summaryLocked()
		m["stdin_closed"] = true
		return m, nil
	}
	if p.stdin != nil && !p.stdinClosed {
		_ = p.stdin.Close()
		p.stdinClosed = true
	}
	m := p.summaryLocked()
	m["status"] = "ok"
	m["stdin_closed"] = true
	return m, nil
}

func (r *ProcessRegistry) writeStdin(id, data string, _ bool) (map[string]any, error) {
	p, err := r.get(id)
	if err != nil {
		return nil, err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.Status != processStatusRunning {
		return nil, fmt.Errorf("process not running: %s", p.Status)
	}
	if p.stdin == nil || p.stdinClosed {
		return nil, errors.New("stdin is closed")
	}
	n, err := io.WriteString(p.stdin, data)
	if err != nil {
		return nil, fmt.Errorf("stdin write: %w", err)
	}
	m := p.summaryLocked()
	m["status"] = "ok"
	m["bytes_written"] = n
	return m, nil
}

func copyCapped(src io.Reader, dst *bytes.Buffer, mu *sync.Mutex, maxOut int) {
	buf := make([]byte, 4096)
	for {
		n, err := src.Read(buf)
		if n > 0 {
			mu.Lock()
			remain := maxOut - dst.Len()
			if remain > 0 {
				if n > remain {
					n = remain
				}
				_, _ = dst.Write(buf[:n])
			}
			mu.Unlock()
		}
		if err != nil {
			return
		}
	}
}

func (r *ProcessRegistry) get(id string) (*managedProcess, error) {
	if r == nil {
		return nil, errors.New("process registry is nil")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	p, ok := r.byID[id]
	if !ok {
		return nil, fmt.Errorf("process not found: %s", id)
	}
	return p, nil
}

// List returns summaries for a chat session (empty chatSessionID → all).
func (r *ProcessRegistry) List(chatSessionID string) []map[string]any {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]map[string]any, 0, len(r.byID))
	for _, p := range r.byID {
		p.mu.Lock()
		if chatSessionID != "" && p.ChatSessionID != chatSessionID {
			p.mu.Unlock()
			continue
		}
		out = append(out, p.summaryLocked())
		p.mu.Unlock()
	}
	return out
}

func (p *managedProcess) summaryLocked() map[string]any {
	m := map[string]any{
		"session_id": p.ID,
		"command":    p.Command,
		"status":     p.Status,
		"started_at": p.StartedAt.UTC().Format(time.RFC3339),
	}
	if p.PTY {
		m["pty"] = true
	}
	if p.Workdir != "" {
		m["workdir"] = p.Workdir
	}
	if p.Status != processStatusRunning {
		m["exit_code"] = p.ExitCode
		if !p.FinishedAt.IsZero() {
			m["finished_at"] = p.FinishedAt.UTC().Format(time.RFC3339)
		}
	}
	if p.NotifyPending {
		m["notify_on_complete"] = true
	}
	return m
}

// AcknowledgeNotify clears notify_pending without requiring Poll (used by Agent wake).
func (r *ProcessRegistry) AcknowledgeNotify(id string) bool {
	p, err := r.get(id)
	if err != nil {
		return false
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.NotifyPending {
		return false
	}
	p.NotifyPending = false
	return true
}

// Poll returns status and output since byte offsets (stdout_offset / stderr_offset).
func (r *ProcessRegistry) Poll(id string, stdoutOffset, stderrOffset int) (map[string]any, error) {
	p, err := r.get(id)
	if err != nil {
		return nil, err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	stdoutAll := p.stdout.String()
	stderrAll := p.stderr.String()
	if stdoutOffset < 0 {
		stdoutOffset = 0
	}
	if stderrOffset < 0 {
		stderrOffset = 0
	}
	if stdoutOffset > len(stdoutAll) {
		stdoutOffset = len(stdoutAll)
	}
	if stderrOffset > len(stderrAll) {
		stderrOffset = len(stderrAll)
	}
	newOut := stdoutAll[stdoutOffset:]
	newErr := stderrAll[stderrOffset:]
	m := p.summaryLocked()
	m["stdout"] = stripANSI(newOut)
	m["stderr"] = stripANSI(newErr)
	m["stdout_offset"] = len(stdoutAll)
	m["stderr_offset"] = len(stderrAll)
	if p.NotifyPending {
		m["notify_pending"] = true
		p.NotifyPending = false // consume once
	}
	return m, nil
}

// Log returns paginated combined/full stdout (and stderr) by character offset.
func (r *ProcessRegistry) Log(id string, offset, limit int) (map[string]any, error) {
	p, err := r.get(id)
	if err != nil {
		return nil, err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	stdoutAll := stripANSI(p.stdout.String())
	stderrAll := stripANSI(p.stderr.String())
	if offset < 0 {
		offset = 0
	}
	if limit <= 0 {
		limit = 4000
	}
	slice := func(s string) (string, int) {
		if offset >= len(s) {
			return "", len(s)
		}
		end := offset + limit
		if end > len(s) {
			end = len(s)
		}
		return s[offset:end], len(s)
	}
	outChunk, outTotal := slice(stdoutAll)
	errChunk, errTotal := slice(stderrAll)
	m := p.summaryLocked()
	m["stdout"] = outChunk
	m["stderr"] = errChunk
	m["stdout_total"] = outTotal
	m["stderr_total"] = errTotal
	m["offset"] = offset
	m["limit"] = limit
	return m, nil
}

// Wait blocks until the process finishes or timeoutSec elapses (0 = wait forever).
func (r *ProcessRegistry) Wait(id string, timeoutSec int) (map[string]any, error) {
	p, err := r.get(id)
	if err != nil {
		return nil, err
	}
	if timeoutSec <= 0 {
		<-p.done
		return r.Poll(id, 0, 0)
	}
	select {
	case <-p.done:
		return r.Poll(id, 0, 0)
	case <-time.After(time.Duration(timeoutSec) * time.Second):
		p.mu.Lock()
		m := p.summaryLocked()
		p.mu.Unlock()
		m["wait_timed_out"] = true
		return m, nil
	}
}

// Kill terminates a running process.
func (r *ProcessRegistry) Kill(id string) (map[string]any, error) {
	p, err := r.get(id)
	if err != nil {
		return nil, err
	}
	p.mu.Lock()
	if p.Status != processStatusRunning {
		m := p.summaryLocked()
		p.mu.Unlock()
		return m, nil
	}
	if p.cancel != nil {
		p.cancel()
	}
	if proc := p.ensureProcess(); proc != nil {
		_ = proc.Kill()
	}
	if p.ptySession != nil {
		_ = p.ptySession.Close()
		p.ptySession = nil
	}
	p.Status = processStatusKilled
	p.ExitCode = -1
	p.FinishedAt = time.Now()
	p.finish.Do(func() { close(p.done) })
	m := p.summaryLocked()
	p.mu.Unlock()
	return m, nil
}

// KillChatSession kills all processes for a chat session (best-effort cleanup).
func (r *ProcessRegistry) KillChatSession(chatSessionID string) {
	if r == nil || chatSessionID == "" {
		return
	}
	r.mu.Lock()
	ids := make([]string, 0)
	for id, p := range r.byID {
		p.mu.Lock()
		if p.ChatSessionID == chatSessionID && p.Status == processStatusRunning {
			ids = append(ids, id)
		}
		p.mu.Unlock()
	}
	r.mu.Unlock()
	for _, id := range ids {
		_, _ = r.Kill(id)
	}
}
