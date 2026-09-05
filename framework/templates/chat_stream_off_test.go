package templates

import (
	"os"
	"testing"
)

func TestChatStreamGoRemoved(t *testing.T) {
	if _, err := os.Stat("chat_stream.go"); err == nil {
		t.Fatal("unused NewChatStreamHandler wrappers must be removed")
	}
}
