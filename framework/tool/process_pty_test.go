package tool

import (
	"fmt"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestProcessRegistry_PTYStartAndWait(t *testing.T) {
	reg := NewProcessRegistry()
	cmd := "echo pty_bg_ok"
	id, err := reg.Start(processStartRequest{
		ChatSessionID:  "chat-pty",
		Command:        cmd,
		TimeoutSec:     30,
		PTY:            true,
		MaxOutputBytes: 8192,
	})
	if err != nil {
		t.Fatal(err)
	}
	m, err := reg.Wait(id, 15)
	if err != nil {
		t.Fatal(err)
	}
	if m["pty"] != true {
		t.Fatalf("expected pty flag: %#v", m)
	}
	out := strings.ToLower(fmt.Sprint(m["stdout"]))
	if !strings.Contains(out, "pty_bg_ok") {
		t.Fatalf("stdout missing marker: %#v", m)
	}
}

func TestProcessRegistry_PTYInteractiveWrite(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("interactive pty write smoke is unix-focused")
	}
	reg := NewProcessRegistry()
	id, err := reg.Start(processStartRequest{
		Command:    "cat",
		TimeoutSec: 10,
		PTY:        true,
		RawArgs:    []string{"cat"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reg.Submit(id, "hello-pty"); err != nil {
		t.Fatal(err)
	}
	if _, err := reg.CloseStdin(id); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		m, err := reg.Poll(id, 0, 0)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(fmt.Sprint(m["stdout"]), "hello-pty") {
			_, _ = reg.Kill(id)
			return
		}
		if m["status"] != processStatusRunning {
			if strings.Contains(fmt.Sprint(m["stdout"]), "hello-pty") {
				return
			}
			t.Fatalf("exited without expected output: %#v", m)
		}
		time.Sleep(50 * time.Millisecond)
	}
	_, _ = reg.Kill(id)
	t.Fatal("timeout waiting for pty echo")
}
