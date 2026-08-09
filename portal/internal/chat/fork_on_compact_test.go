package chat

import (
	"os"
	"testing"
)

func TestCompactForkSessionEnabled(t *testing.T) {
	t.Setenv("SATH_COMPACT_FORK_SESSION", "")
	if CompactForkSessionEnabled() {
		t.Fatal("default off")
	}
	t.Setenv("SATH_COMPACT_FORK_SESSION", "true")
	if !CompactForkSessionEnabled() {
		t.Fatal("true should enable")
	}
	t.Setenv("SATH_COMPACT_FORK_SESSION", "0")
	if CompactForkSessionEnabled() {
		t.Fatal("0 should disable")
	}
	_ = os.Unsetenv("SATH_COMPACT_FORK_SESSION")
}
