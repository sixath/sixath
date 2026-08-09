package tool

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	gopty "github.com/aymanbagabas/go-pty"
)

// ptyWriteCloser writes to a PTY master; Close sends EOT without closing the master
// so readers can still drain output until the session ends.
type ptyWriteCloser struct {
	w io.Writer
}

func (p *ptyWriteCloser) Write(b []byte) (int, error) {
	if p == nil || p.w == nil {
		return 0, errors.New("pty stdin closed")
	}
	return p.w.Write(b)
}

func (p *ptyWriteCloser) Close() error {
	if p == nil || p.w == nil {
		return nil
	}
	_, err := p.w.Write([]byte{4}) // EOT
	return err
}

func (r *ProcessRegistry) startPTY(req processStartRequest, id string, ctx context.Context, cancel context.CancelFunc, maxOut int, command string) (string, error) {
	ptmx, err := gopty.New()
	if err != nil {
		cancel()
		return "", fmt.Errorf("pty: %w", err)
	}

	var ptyCmd *gopty.Cmd
	if len(req.RawArgs) > 0 {
		name := resolveExecutable(req.RawArgs[0])
		ptyCmd = ptmx.CommandContext(ctx, name, req.RawArgs[1:]...)
	} else {
		shell, args := localShellInvocation(command)
		ptyCmd = ptmx.CommandContext(ctx, resolveExecutable(shell), args...)
	}
	ptyCmd.Dir = req.Workdir

	p := &managedProcess{
		ID:               id,
		ChatSessionID:    req.ChatSessionID,
		Command:          command,
		Workdir:          req.Workdir,
		Status:           processStatusRunning,
		NotifyOnComplete: req.NotifyOnComplete,
		StartedAt:        time.Now(),
		maxOut:           maxOut,
		PTY:              true,
		stdin:            &ptyWriteCloser{w: ptmx},
		ptySession:       ptmx,
		cancel:           cancel,
		done:             make(chan struct{}),
	}

	if err := ptyCmd.Start(); err != nil {
		_ = ptmx.Close()
		cancel()
		return "", err
	}
	p.process = ptyCmd.Process

	go copyCapped(ptmx, &p.stdout, &p.mu, maxOut)
	go func() {
		waitErr := ptyCmd.Wait()
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
				if waitErr != nil {
					p.ExitCode = -1
					if ptyCmd.ProcessState != nil {
						p.ExitCode = ptyCmd.ProcessState.ExitCode()
					} else {
						var exitErr *exec.ExitError
						if errors.As(waitErr, &exitErr) {
							p.ExitCode = exitErr.ExitCode()
						}
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
		p.stdinClosed = true
		if p.ptySession != nil {
			_ = p.ptySession.Close()
			p.ptySession = nil
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

// runCommandWithPTY runs a foreground shell command attached to a real PTY.
func runCommandWithPTY(ctx context.Context, command, workDir string, maxOut int) TerminalRunResult {
	start := time.Now()
	res := TerminalRunResult{ExitCode: 0}
	ptmx, err := gopty.New()
	if err != nil {
		res.Err = err
		res.ExitCode = -1
		res.Duration = time.Since(start)
		return res
	}

	shell, args := localShellInvocation(command)
	cmd := ptmx.CommandContext(ctx, resolveExecutable(shell), args...)
	cmd.Dir = workDir

	var outBuf bytesBufferCapped
	outBuf.max = maxOut
	doneCopy := make(chan struct{})
	go func() {
		defer close(doneCopy)
		_, _ = io.Copy(&outBuf, ptmx)
	}()

	if err := cmd.Start(); err != nil {
		_ = ptmx.Close()
		res.Err = err
		res.ExitCode = -1
		res.Duration = time.Since(start)
		return res
	}
	waitErr := cmd.Wait()
	_ = ptmx.Close()
	<-doneCopy

	res.Stdout = outBuf.String()
	res.TimedOut = errors.Is(ctx.Err(), context.DeadlineExceeded)
	res.Duration = time.Since(start)
	res.Err = waitErr
	if waitErr != nil {
		res.ExitCode = -1
		if cmd.ProcessState != nil {
			res.ExitCode = cmd.ProcessState.ExitCode()
		} else {
			var exitErr *exec.ExitError
			if errors.As(waitErr, &exitErr) {
				res.ExitCode = exitErr.ExitCode()
			}
		}
	}
	return res
}

// bytesBufferCapped is a small capped buffer for foreground PTY capture.
type bytesBufferCapped struct {
	buf []byte
	max int
}

func (b *bytesBufferCapped) Write(p []byte) (int, error) {
	if b.max <= 0 {
		b.max = processDefaultMaxOut
	}
	remain := b.max - len(b.buf)
	if remain <= 0 {
		return len(p), nil
	}
	if len(p) > remain {
		p = p[:remain]
	}
	b.buf = append(b.buf, p...)
	return len(p), nil
}

func (b *bytesBufferCapped) String() string {
	return string(b.buf)
}

// ensureProcessAlive returns the OS process for kill (pipe or pty mode).
func (p *managedProcess) ensureProcess() *os.Process {
	if p == nil {
		return nil
	}
	if p.process != nil {
		return p.process
	}
	if p.cmd != nil {
		return p.cmd.Process
	}
	return nil
}

// resolveExecutable returns an absolute path when LookPath succeeds.
// Required for Windows ConPTY: go-pty joins Cmd.Dir with Path when Dir is set.
func resolveExecutable(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return name
	}
	if filepath.IsAbs(name) {
		return name
	}
	if abs, err := exec.LookPath(name); err == nil {
		return abs
	}
	return name
}
