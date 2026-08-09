package memory

import (
	"encoding/hex"
	"testing"
)

func TestScopeConstants(t *testing.T) {
	if ScopeUser != "user" || ScopeSession != "session" || ScopeAgent != "agent" {
		t.Fatalf("unexpected scope constants")
	}
}

func TestErrScopeNotEnabled(t *testing.T) {
	if ErrScopeNotEnabled == nil || ErrScopeNotEnabled.Error() == "" {
		t.Fatal("ErrScopeNotEnabled must be set")
	}
}

func TestErrNotSupported(t *testing.T) {
	if ErrNotSupported == nil || ErrNotSupported.Error() == "" {
		t.Fatal("ErrNotSupported must be set")
	}
}

func TestContentHash(t *testing.T) {
	const content = "hello"
	got := ContentHash(content)
	if len(got) != 64 {
		t.Fatalf("ContentHash length = %d, want 64", len(got))
	}
	if _, err := hex.DecodeString(got); err != nil {
		t.Fatalf("ContentHash is not valid hex: %v", err)
	}
	// SHA-256("hello") is stable
	const want = "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824"
	if got != want {
		t.Fatalf("ContentHash(%q) = %q, want %q", content, got, want)
	}
}
