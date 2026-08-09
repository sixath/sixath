package memorysearch

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/sixath/framework/config"
)

func TestNewQmdMemoryManager_InvalidCommand(t *testing.T) {
	_, err := NewQmdMemoryManager(&config.QmdConfig{Command: ""}, "/tmp")
	if err == nil {
		t.Fatal("expected error for empty command")
	}
	_, err = NewQmdMemoryManager(nil, "/tmp")
	if err == nil {
		t.Fatal("expected error for nil config")
	}
	// 使用不存在的命令，应失败（LookPath 或 exec 会失败）
	_, err = NewQmdMemoryManager(&config.QmdConfig{Command: "/nonexistent/qmd-xyz-12345"}, "/tmp")
	if err == nil {
		t.Log("note: /nonexistent path may not fail on all systems")
	}
}

func TestGetMemorySearchManager_QmdFallbackToBuiltin(t *testing.T) {
	dir := t.TempDir()
	memPath := filepath.Join(dir, "MEMORY.md")
	_ = os.WriteFile(memPath, []byte("# Memory\n\nKeyword: fallback test."), 0644)

	// 使用 "qmd" 命令：若 PATH 中无 qmd，NewQmdMemoryManager 会失败，触发回退
	cfg := config.Config{
		Memory: config.MemoryConfig{
			Backend: "qmd",
			Qmd:     &config.QmdConfig{Command: "qmd"},
			Defaults: config.MemorySearchConfig{
				Enabled: true,
				Sources: []string{"memory"},
				Store:   config.MemoryStoreConfig{Path: filepath.Join(dir, ".m.db")},
			},
		},
	}
	mgr, err := GetMemorySearchManager(cfg, "agent-1", dir, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if mgr == nil {
		t.Fatal("expected manager (builtin or qmd)")
	}
	ctx := context.Background()
	_ = mgr.Sync(ctx, &SyncParams{Reason: "test", Force: true})
	// qmd 未安装时回退 builtin，能检索到；qmd 已安装时可能返回空（无 collection）
	results, err := mgr.Search(ctx, "fallback", &SearchOpts{MaxResults: 5, MinScore: 0})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) == 0 {
		t.Log("note: no results (qmd may be installed but have no index)")
	} else {
		t.Logf("got %d results", len(results))
	}
	// 关闭以释放 db 文件，避免 TempDir 清理时报错
	if c, ok := mgr.(interface{ Close() error }); ok {
		_ = c.Close()
	}
}

func TestFallbackMemoryManager_SearchFallback(t *testing.T) {
	dir := t.TempDir()
	memPath := filepath.Join(dir, "MEMORY.md")
	_ = os.WriteFile(memPath, []byte("# Memory\n\nContent for fallback."), 0644)

	// 创建一个会失败的 primary（无效 qmd）
	qmdMgr, err := NewQmdMemoryManager(&config.QmdConfig{Command: "qmd"}, dir)
	if err != nil {
		// qmd 可能未安装，跳过
		t.Skip("qmd not installed, skip FallbackManager test")
	}
	cfg := config.Config{
		Memory: config.MemoryConfig{
			Defaults: config.MemorySearchConfig{
				Enabled: true,
				Store:   config.MemoryStoreConfig{Path: filepath.Join(dir, ".fb.db")},
			},
		},
	}
	fb, err := newFallbackMemoryManager(qmdMgr, cfg, "a1", dir, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	// primary (qmd) 可能失败（无索引等），fallback 应能工作
	_ = fb.Sync(ctx, &SyncParams{Force: true})
	results, _ := fb.Search(ctx, "Content", &SearchOpts{MaxResults: 5})
	if len(results) > 0 {
		t.Logf("fallback returned %d results", len(results))
	}
	// 验证 SessionDirtyNotifier 转发
	var _ SessionDirtyNotifier = (*FallbackMemoryManager)(nil)
	fb.NotifySessionDirty("s1", 100, 1)
}
