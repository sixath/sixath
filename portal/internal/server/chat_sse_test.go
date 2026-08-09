package server

import (
	"context"
	"errors"
	"testing"

	"backend/internal/chatsse"
)

func TestSuppressTerminalStreamErrorAfterContent(t *testing.T) {
	if !chatsse.SuppressTerminalStreamError(context.DeadlineExceeded, true) {
		t.Fatal("expected deadline error after streamed content to be suppressed")
	}
	if chatsse.SuppressTerminalStreamError(context.DeadlineExceeded, false) {
		t.Fatal("expected deadline error before streamed content to remain visible")
	}
	if chatsse.SuppressTerminalStreamError(errors.New("invalid connection"), true) {
		t.Fatal("expected non-context errors to remain visible")
	}
}
