package harness

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAgentAliasPackageRemoved(t *testing.T) {
	dir := filepath.Join("..", "agent")
	if _, err := os.Stat(dir); err == nil {
		t.Fatal("one-season alias framework/agent must be removed; import github.com/sixath/framework/harness")
	}
}
