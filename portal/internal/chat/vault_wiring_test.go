package chat

import (
	"context"
	"strings"
	"testing"

	"backend/internal/conf"

	"github.com/sixath/framework/tool"
)

func TestVaultDepositHintEnabled(t *testing.T) {
	h := VaultDepositHint(true)
	for _, kw := range []string{"_agent_notes", "promoted", "status", "pending", "session"} {
		if !strings.Contains(h, kw) {
			t.Errorf("VaultDepositHint(true) missing keyword %q:\n%s", kw, h)
		}
	}
}

func TestVaultDepositHintDisabled(t *testing.T) {
	if h := VaultDepositHint(false); h != "" {
		t.Errorf("VaultDepositHint(false) = %q, want empty", h)
	}
}

func TestWireVaultDisabled(t *testing.T) {
	cleanup, err := WireVault(context.Background(), tool.NewRegistry(), conf.VaultConfig{Enabled: false})
	if err != nil {
		t.Fatalf("WireVault(disabled) err = %v, want nil", err)
	}
	if cleanup != nil {
		t.Fatalf("WireVault(disabled) cleanup is non-nil, want nil")
	}
}

func TestWireVaultEmptyRoot(t *testing.T) {
	cleanup, err := WireVault(context.Background(), tool.NewRegistry(), conf.VaultConfig{Enabled: true, Root: ""})
	if err != nil {
		t.Fatalf("WireVault(empty root) err = %v, want nil", err)
	}
	if cleanup != nil {
		t.Fatalf("WireVault(empty root) cleanup is non-nil, want nil")
	}
}
