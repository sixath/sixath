package chat

import (
	"context"
	"strings"
	"sync"
	"testing"

	"backend/internal/conf"

	"github.com/sixath/framework/memory"
	"github.com/sixath/framework/tool"
	"github.com/sixath/framework/vault"
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

// fakeUnitEmbedder 返回空向量，配合空 vault root（无笔记可 embed）走通 watcher 创建路径。
type fakeUnitEmbedder struct{}

func (fakeUnitEmbedder) Embed(ctx context.Context, agentID string, texts []string) ([][]float32, error) {
	out := make([][]float32, 0, len(texts))
	for range texts {
		out = append(out, []float32{0})
	}
	return out, nil
}

// TestWireVaultCleanupAlwaysNilAndIdempotent 锁定两个语义：
//  1. WireVault 恒返回 nil cleanup（watcher 是进程级单例，调用方不应/不能随请求关闭它，
//     否则首个请求返回就会终止 fsnotify 监听、使热重载永久失效）；
//  2. 成功路径下 watcher 只构造一次（幂等，重复调用不重复建 watcher）。
func TestWireVaultCleanupAlwaysNilAndIdempotent(t *testing.T) {
	resetVaultWireForTest()
	t.Cleanup(resetVaultWireForTest)

	root := t.TempDir()
	var mu sync.Mutex
	builds := 0
	vaultStoreOpts = func() MemoryStoreOptions {
		return MemoryStoreOptions{
			UnitEmbedder: fakeUnitEmbedder{},
			UnitVectors:  memory.NewInMemoryUnitVectorIndex(),
		}
	}
	vaultBuildWatcher = func(root string, embed memory.UnitEmbedder, idx memory.UnitVectorIndex) (*vault.Watcher, error) {
		mu.Lock()
		builds++
		mu.Unlock()
		return vault.NewWatcher(root, embed, idx)
	}

	cfg := conf.VaultConfig{Enabled: true, Root: root}

	// 第一次：真实走通 watcher 创建路径。
	cleanup1, err := WireVault(context.Background(), tool.NewRegistry(), cfg)
	if err != nil {
		t.Fatalf("first WireVault err = %v, want nil", err)
	}
	if cleanup1 != nil {
		t.Fatalf("first WireVault cleanup is non-nil, want nil (process-level singleton must not be closed per-request)")
	}

	// 第二次：幂等，不重复建 watcher，cleanup 仍为 nil。
	cleanup2, err := WireVault(context.Background(), tool.NewRegistry(), cfg)
	if err != nil {
		t.Fatalf("second WireVault err = %v, want nil", err)
	}
	if cleanup2 != nil {
		t.Fatalf("second WireVault cleanup is non-nil, want nil")
	}

	mu.Lock()
	n := builds
	mu.Unlock()
	if n != 1 {
		t.Fatalf("watcher built %d times, want exactly 1 (idempotent)", n)
	}
}

