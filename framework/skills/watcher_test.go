package skills

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func writeSkill(t *testing.T, root, subdir, name string) {
	t.Helper()
	dir := filepath.Join(root, subdir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	content := fmt.Sprintf("---\nname: %s\ndescription: test\n---\n\n# %s\n", name, name)
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestWatcher_ReloadsOnSkillChange(t *testing.T) {
	root := t.TempDir()
	writeSkill(t, root, "skill-a", "alpha")

	w, err := NewWatcher([]string{root}, nil, nil, WithWatchDebounce(50*time.Millisecond))
	if err != nil {
		t.Fatalf("NewWatcher: %v", err)
	}
	defer w.Close()

	if _, ok := w.Current().GetByName("alpha"); !ok {
		t.Fatal("alpha not indexed initially")
	}

	writeSkill(t, root, "skill-b", "beta")

	// 轮询等待热重载（fsnotify 异步）。
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if _, ok := w.Current().GetByName("beta"); ok {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("beta not reloaded within timeout")
}

func TestWatcher_DebouncesRapidWrites(t *testing.T) {
	root := t.TempDir()
	writeSkill(t, root, "skill-a", "alpha")

	var mu sync.Mutex
	reloads := 0
	w, err := NewWatcher([]string{root}, nil, nil,
		WithWatchDebounce(300*time.Millisecond),
		WithReloadCallback(func(*Index) { mu.Lock(); reloads++; mu.Unlock() }),
	)
	if err != nil {
		t.Fatalf("NewWatcher: %v", err)
	}
	defer w.Close()

	// 连续 5 次快速重写同一 skill，应在去抖窗口内合并为 1 次重建。
	for i := 0; i < 5; i++ {
		writeSkill(t, root, "skill-a", "alpha")
	}

	time.Sleep(900 * time.Millisecond) // 等最后一次去抖结算
	mu.Lock()
	n := reloads
	mu.Unlock()
	if n != 1 {
		t.Fatalf("reloads = %d, want 1 (rapid writes coalesced by debounce)", n)
	}
}

func TestWatcher_ReloadIsAtomic(t *testing.T) {
	root := t.TempDir()
	writeSkill(t, root, "skill-a", "alpha")

	w, err := NewWatcher([]string{root}, nil, nil)
	if err != nil {
		t.Fatalf("NewWatcher: %v", err)
	}
	defer w.Close()

	var failed atomic.Bool
	stop := make(chan struct{})
	var wg sync.WaitGroup

	// 读者：并发读取当前索引，断言始终拿到完整一致的快照。
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				idx := w.Current()
				if idx == nil {
					failed.Store(true)
					return
				}
				if _, ok := idx.GetByName("alpha"); !ok {
					failed.Store(true)
					return
				}
				_ = idx.All()
			}
		}()
	}

	// 写者：反复 Reload 触发原子替换。
	for i := 0; i < 50; i++ {
		if err := w.Reload(); err != nil {
			t.Fatalf("Reload: %v", err)
		}
	}
	close(stop)
	wg.Wait()

	if failed.Load() {
		t.Fatal("concurrent reader observed nil or inconsistent index")
	}
}

func TestWatcher_ManualReloadAndCloseIdempotent(t *testing.T) {
	root := t.TempDir()
	writeSkill(t, root, "skill-a", "alpha")

	w, err := NewWatcher([]string{root}, nil, nil)
	if err != nil {
		t.Fatalf("NewWatcher: %v", err)
	}

	// 手动 Reload 能看到新 skill。
	writeSkill(t, root, "skill-b", "beta")
	if err := w.Reload(); err != nil {
		t.Fatalf("Reload: %v", err)
	}
	if _, ok := w.Current().GetByName("beta"); !ok {
		t.Fatal("beta not visible after manual Reload")
	}

	// Close 幂等。
	w.Close()
	w.Close()
}
