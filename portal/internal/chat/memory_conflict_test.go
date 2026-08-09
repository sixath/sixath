package chat

import (
	"context"
	"os"
	"testing"

	"backend/internal/biz"

	"github.com/sixath/framework/config"
)

func TestMemoryConflictEnabled_DefaultOff(t *testing.T) {
	prev := os.Getenv("SATH_MEMORY_CONFLICT_ENABLED")
	_ = os.Unsetenv("SATH_MEMORY_CONFLICT_ENABLED")
	defer func() {
		if prev == "" {
			_ = os.Unsetenv("SATH_MEMORY_CONFLICT_ENABLED")
		} else {
			_ = os.Setenv("SATH_MEMORY_CONFLICT_ENABLED", prev)
		}
	}()
	SetMemoryConflictConfig(nil)
	if memoryConflictEnabled() {
		t.Fatal("expected disabled by default")
	}
}

func TestMemoryConflictEnabled_EnvTrue(t *testing.T) {
	prev := os.Getenv("SATH_MEMORY_CONFLICT_ENABLED")
	defer func() {
		if prev == "" {
			_ = os.Unsetenv("SATH_MEMORY_CONFLICT_ENABLED")
		} else {
			_ = os.Setenv("SATH_MEMORY_CONFLICT_ENABLED", prev)
		}
	}()
	SetMemoryConflictConfig(nil)
	_ = os.Setenv("SATH_MEMORY_CONFLICT_ENABLED", "true")
	if !memoryConflictEnabled() {
		t.Fatal("expected enabled from env")
	}
	_ = os.Setenv("SATH_MEMORY_CONFLICT_ENABLED", "false")
	SetMemoryConflictConfig(&config.MemoryConflict{Enabled: true})
	if memoryConflictEnabled() {
		t.Fatal("env false should override YAML")
	}
}

func TestMemoryConflictEnabled_YAMLTrue(t *testing.T) {
	prev := os.Getenv("SATH_MEMORY_CONFLICT_ENABLED")
	_ = os.Unsetenv("SATH_MEMORY_CONFLICT_ENABLED")
	defer func() {
		if prev == "" {
			_ = os.Unsetenv("SATH_MEMORY_CONFLICT_ENABLED")
		} else {
			_ = os.Setenv("SATH_MEMORY_CONFLICT_ENABLED", prev)
		}
	}()
	SetMemoryConflictConfig(&config.MemoryConflict{Enabled: true, K: 12})
	if !memoryConflictEnabled() {
		t.Fatal("expected enabled from YAML")
	}
	if memoryConflictK() != 12 {
		t.Fatalf("K=%d, want 12", memoryConflictK())
	}
}

func TestDefaultMemoryStoreOptions_NilSemanticWhenNoFactory(t *testing.T) {
	prevGetter := globalMemoryAgentGetter
	prevExtract := storedExtractionYAML
	t.Cleanup(func() {
		globalMemoryAgentGetter = prevGetter
		storedExtractionYAML = prevExtract
	})

	SetMemoryAgentGetter(nil)
	SetMemoryExtractionConfig(nil)
	opts := DefaultMemoryStoreOptions()
	if opts.SemanticConflicts != nil {
		t.Fatal("expected SemanticConflicts=nil without auxiliary or AgentGetter")
	}

	SetMemoryExtractionConfig(&config.MemoryExtraction{
		Auxiliary: &config.MemoryExtractionModel{Provider: "openai", Model: "gpt-4o-mini"},
	})
	opts = DefaultMemoryStoreOptions()
	if opts.SemanticConflicts == nil {
		t.Fatal("expected SemanticConflicts when auxiliary model is configured")
	}

	SetMemoryExtractionConfig(nil)
	SetMemoryAgentGetter(stubAgentGetter{})
	opts = DefaultMemoryStoreOptions()
	if opts.SemanticConflicts == nil {
		t.Fatal("expected SemanticConflicts when AgentGetter is set")
	}
}

type stubAgentGetter struct{}

func (stubAgentGetter) Get(ctx context.Context, id string) (*biz.AgentMeta, error) {
	return nil, nil
}
