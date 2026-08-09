package chat

import (
	"os"
	"testing"

	"backend/internal/biz"

	"github.com/sixath/framework/config"
)

func TestMemoryExtractionEnabled_DefaultOff(t *testing.T) {
	prev := os.Getenv("SATH_MEMORY_EXTRACTION_ENABLED")
	_ = os.Unsetenv("SATH_MEMORY_EXTRACTION_ENABLED")
	defer func() {
		if prev == "" {
			_ = os.Unsetenv("SATH_MEMORY_EXTRACTION_ENABLED")
		} else {
			_ = os.Setenv("SATH_MEMORY_EXTRACTION_ENABLED", prev)
		}
	}()
	SetMemoryExtractionConfig(nil)
	if memoryExtractionEnabled() {
		t.Fatal("expected disabled by default")
	}
	SetMemoryExtractionConfig(&config.MemoryExtraction{Enabled: true})
	if !memoryExtractionEnabled() {
		t.Fatal("expected enabled from YAML")
	}
	_ = os.Setenv("SATH_MEMORY_EXTRACTION_ENABLED", "false")
	if memoryExtractionEnabled() {
		t.Fatal("env false should override YAML")
	}
}

func TestLastUserMessageContent(t *testing.T) {
	msgs := []*biz.ChatMessage{
		{Role: "user", Content: "first"},
		{Role: "assistant", Content: "ok"},
		{Role: "user", Content: "second"},
	}
	if got := LastUserMessageContent(msgs); got != "second" {
		t.Fatalf("got %q", got)
	}
}
