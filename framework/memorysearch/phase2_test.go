package memorysearch

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/sixath/framework/config"
)

// mockSessionProviderSimple 简化版：直接返回固定会话列表和转录。
type mockSessionProviderSimple struct {
	SessionIDs  []string
	Transcripts map[string]string
}

func (m *mockSessionProviderSimple) ListSessionsForAgent(ctx context.Context, agentID string) ([]string, error) {
	return m.SessionIDs, nil
}

func (m *mockSessionProviderSimple) GetTranscript(ctx context.Context, sessionID string) (string, error) {
	if m.Transcripts != nil {
		return m.Transcripts[sessionID], nil
	}
	return "", nil
}

func TestMemoryIndexManager_SessionsSource(t *testing.T) {
	dir := t.TempDir()
	provider := &mockSessionProviderSimple{
		SessionIDs: []string{"sess-001", "sess-002"},
		Transcripts: map[string]string{
			"sess-001": "# Session 1\n\nUser: What is the project about?\nAssistant: It uses Go and React.",
			"sess-002": "# Session 2\n\nUser: How to deploy?\nAssistant: Use Docker.",
		},
	}
	cfg := config.MemorySearchConfig{
		Enabled: true,
		Sources: []string{"memory", "sessions"},
		Store:   config.MemoryStoreConfig{Path: filepath.Join(dir, ".mem.db")},
		Sync: config.MemorySyncConfig{
			Sessions: &config.MemorySessionsConfig{DeltaBytes: 100, DeltaMessages: 2},
		},
	}
	resolved := ResolveMemorySearch(cfg, dir)
	if resolved == nil {
		t.Fatal("expected resolved config")
	}
	resolved.StorePath = filepath.Join(dir, ".mem.db")
	mgr, err := NewMemoryIndexManager(resolved, dir, "agent-1", nil, provider)
	if err != nil {
		t.Fatal(err)
	}
	defer mgr.Close()

	ctx := context.Background()
	// Force sync 以索引所有 sessions
	if err := mgr.Sync(ctx, &SyncParams{Reason: "test", Force: true}); err != nil {
		t.Fatal(err)
	}

	// 应能检索到会话内容
	results, err := mgr.Search(ctx, "Docker", &SearchOpts{MaxResults: 5, MinScore: 0})
	if err != nil {
		t.Fatal(err)
	}
	foundSession := false
	for _, r := range results {
		if r.Source == "sessions" && (r.Path == "sessions/sess-002.md" || r.Path == "sessions/sess-001.md") {
			foundSession = true
			break
		}
	}
	if !foundSession {
		t.Errorf("expected sessions source in results, got %+v", results)
	}

	// ReadFile 应能读取 sessions 虚拟路径
	res, err := mgr.ReadFile(ctx, &ReadFileParams{RelPath: "sessions/sess-001.md"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Text == "" || res.Path != "sessions/sess-001.md" {
		t.Errorf("expected session transcript, got path=%q text_len=%d", res.Path, len(res.Text))
	}
	if res.Text != provider.Transcripts["sess-001"] {
		t.Errorf("expected transcript content, got %q", res.Text)
	}
}

func TestMemoryIndexManager_NotifySessionDirty(t *testing.T) {
	dir := t.TempDir()
	provider := &mockSessionProviderSimple{
		SessionIDs:  []string{"sess-x"},
		Transcripts: map[string]string{"sess-x": "test content"},
	}
	cfg := config.MemorySearchConfig{
		Enabled: true,
		Sources: []string{"sessions"},
		Store:   config.MemoryStoreConfig{Path: filepath.Join(dir, ".mem2.db")},
		Sync: config.MemorySyncConfig{
			Sessions: &config.MemorySessionsConfig{DeltaBytes: 10, DeltaMessages: 2},
		},
	}
	resolved := ResolveMemorySearch(cfg, dir)
	resolved.StorePath = filepath.Join(dir, ".mem2.db")
	mgr, err := NewMemoryIndexManager(resolved, dir, "agent-2", nil, provider)
	if err != nil {
		t.Fatal(err)
	}
	defer mgr.Close()

	// 验证实现了 SessionDirtyNotifier
	var _ SessionDirtyNotifier = (*MemoryIndexManager)(nil)

	// NotifySessionDirty 不 panic，累积超过阈值会触发异步 sync
	mgr.NotifySessionDirty("sess-x", 5, 1)
	mgr.NotifySessionDirty("sess-x", 5, 1)
	time.Sleep(600 * time.Millisecond)
	// 手动 Sync 确保索引
	ctx := context.Background()
	_ = mgr.Sync(ctx, &SyncParams{Reason: "manual", Force: true})
	results, _ := mgr.Search(ctx, "test", &SearchOpts{MaxResults: 5, MinScore: 0})
	if len(results) == 0 {
		t.Errorf("expected session indexed after Force sync, got 0 results")
	}
}

func TestResolveMemorySearch_SessionsConfig(t *testing.T) {
	cfg := config.MemorySearchConfig{
		Enabled: true,
		Sources: []string{"memory", "sessions"},
		Sync: config.MemorySyncConfig{
			Sessions: &config.MemorySessionsConfig{
				DeltaBytes:    2048,
				DeltaMessages: 10,
			},
		},
	}
	resolved := ResolveMemorySearch(cfg, "/tmp/ws")
	if resolved == nil {
		t.Fatal("expected resolved")
	}
	if resolved.SessionsDeltaBytes != 2048 {
		t.Errorf("expected SessionsDeltaBytes=2048, got %d", resolved.SessionsDeltaBytes)
	}
	if resolved.SessionsDeltaMessages != 10 {
		t.Errorf("expected SessionsDeltaMessages=10, got %d", resolved.SessionsDeltaMessages)
	}
}
