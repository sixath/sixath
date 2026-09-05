package chat

import (
	"os"
	"strings"
	"testing"
)

func TestGrowthMetadataFileRemoved(t *testing.T) {
	_, err := os.Stat("growth_metadata.go")
	if err == nil {
		t.Fatal("growth_metadata.go must not exist")
	}
	if !os.IsNotExist(err) {
		t.Fatal(err)
	}
}

func TestHarnessGo_doesNotWireFailureCapture(t *testing.T) {
	b, err := os.ReadFile("agent_builder.go")
	if err != nil {
		t.Fatal(err)
	}
	src := string(b)
	for _, needle := range []string{"NewFailureCaptureHook", "FailureCaptureEnabled"} {
		if strings.Contains(src, needle) {
			t.Errorf("agent_builder.go must not contain %q", needle)
		}
	}
}

func TestMainGo_doesNotEnrichFailureCapture(t *testing.T) {
	b, err := os.ReadFile("../../cmd/backend/main.go")
	if err != nil {
		t.Fatal(err)
	}
	src := string(b)
	for _, needle := range []string{"EnrichFailureCaptureFromEnv", "SATH_GROWTH_FAILURE_CAPTURE"} {
		if strings.Contains(src, needle) {
			t.Errorf("main.go must not contain %q", needle)
		}
	}
}
