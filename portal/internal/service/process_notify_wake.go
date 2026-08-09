package service

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	chatv1 "backend/api/chat/v1"
	"backend/internal/chat"

	"github.com/sixath/framework/events"
	"github.com/sixath/framework/tool"
)

// processNotifyWakeEnabled: SATH_PROCESS_NOTIFY_WAKE=0/false disables auto SendMessage (events still fire).
func processNotifyWakeEnabled() bool {
	v := strings.ToLower(strings.TrimSpace(os.Getenv("SATH_PROCESS_NOTIFY_WAKE")))
	if v == "0" || v == "false" || v == "no" || v == "off" {
		return false
	}
	return true
}

var processWakeLocks sync.Map // sessionID -> *sync.Mutex

func (s *ChatService) registerProcessNotifyWake() {
	if s == nil {
		return
	}
	reg := chat.ProcessRegistryForHooks()
	if reg == nil {
		return
	}
	reg.SetNotifyHandler(func(ev tool.ProcessNotifyEvent) {
		s.onProcessNotify(ev)
	})
}

func (s *ChatService) onProcessNotify(ev tool.ProcessNotifyEvent) {
	events.DefaultBus().Publish(context.Background(), events.Event{
		Kind: events.ProcessNotify,
		Payload: map[string]any{
			"session_id":         ev.ChatSessionID,
			"process_session_id": ev.ProcessID,
			"command":            ev.Command,
			"status":             ev.Status,
			"exit_code":          ev.ExitCode,
		},
	})
	if !processNotifyWakeEnabled() {
		return
	}
	if strings.TrimSpace(ev.ChatSessionID) == "" {
		return
	}
	go s.wakeAgentFromProcessNotify(ev)
}

func (s *ChatService) wakeAgentFromProcessNotify(ev tool.ProcessNotifyEvent) {
	lockI, _ := processWakeLocks.LoadOrStore(ev.ChatSessionID, &sync.Mutex{})
	lock := lockI.(*sync.Mutex)
	if !lock.TryLock() {
		s.log.Warnf("process notify wake skipped (session busy): session_id=%s process=%s", ev.ChatSessionID, ev.ProcessID)
		return
	}
	defer lock.Unlock()

	chat.ProcessRegistryForHooks().AcknowledgeNotify(ev.ProcessID)

	content := fmt.Sprintf(
		"[process-notify] Background process %s finished (status=%s exit_code=%d).\nCommand: %s\nUse the process tool (poll/log) if you need output, then continue the user's task.",
		ev.ProcessID, ev.Status, ev.ExitCode, ev.Command,
	)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	_, err := s.SendMessage(ctx, &chatv1.SendMessageRequest{
		SessionId: ev.ChatSessionID,
		Content:   content,
	})
	if err != nil {
		s.log.Warnf("process notify wake SendMessage failed: session_id=%s process=%s err=%v", ev.ChatSessionID, ev.ProcessID, err)
	}
}
