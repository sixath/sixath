package skills

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/fsnotify/fsnotify"
)

const defaultWatchDebounce = 300 * time.Millisecond

// Watcher 在 skill 目录上维护一个可热重载的索引。
//
// 用 atomic.Pointer[Index] 原子替换，保证并发读取始终拿到完整一致的快照
// （不会出现半成品索引）；重载失败时保留旧索引，不破坏当前可服务状态。
// 复用 memorysearch/builtin.go 的 fsnotify + 去抖模式；默认由调用方决定是否启用
// （一次性构建仍直接用 NewIndex，避免无谓的监听开销）。
type Watcher struct {
	dirs     []string
	enabled  []string
	disabled []string
	debounce time.Duration
	onReload func(*Index)

	current atomic.Pointer[Index]

	mu     sync.Mutex
	fs     *fsnotify.Watcher
	stopCh chan struct{}
	stop   sync.Once
}

// WatcherOption 配置 Watcher。
type WatcherOption func(*Watcher)

// WithWatchDebounce 设置「变化 → 重载」之间的去抖时长（<=0 用默认 300ms）。
func WithWatchDebounce(d time.Duration) WatcherOption {
	return func(w *Watcher) { w.debounce = d }
}

// WithReloadCallback 设置每次重载后的回调（供观测/测试）。
func WithReloadCallback(fn func(*Index)) WatcherOption {
	return func(w *Watcher) { w.onReload = fn }
}

// NewWatcher 构建初始索引并启动目录监听。
// 监听启动失败不致命：仍返回可用的 watcher（Reload 手动触发仍可用，只是无自动热重载）。
func NewWatcher(dirs, enabled, disabled []string, opts ...WatcherOption) (*Watcher, error) {
	w := &Watcher{
		dirs:     dirs,
		enabled:  enabled,
		disabled: disabled,
		debounce: defaultWatchDebounce,
	}
	for _, o := range opts {
		o(w)
	}
	if w.debounce <= 0 {
		w.debounce = defaultWatchDebounce
	}

	idx, err := NewIndex(dirs, enabled, disabled)
	if err != nil {
		return nil, err
	}
	w.current.Store(idx)

	_ = w.startWatch() // 失败忽略
	return w, nil
}

// Current 返回当前索引快照（构造成功后永不返回 nil）。
func (w *Watcher) Current() *Index {
	if w == nil {
		return nil
	}
	return w.current.Load()
}

// Reload 手动重建索引并原子替换；失败时保留旧索引并返回错误。
func (w *Watcher) Reload() error {
	idx, err := NewIndex(w.dirs, w.enabled, w.disabled)
	if err != nil {
		return err
	}
	w.current.Store(idx)
	if w.onReload != nil {
		w.onReload(idx)
	}
	return nil
}

// Close 停止监听并释放资源，幂等。
func (w *Watcher) Close() {
	if w == nil {
		return
	}
	w.stop.Do(func() {
		w.mu.Lock()
		fs := w.fs
		stopCh := w.stopCh
		w.mu.Unlock()
		if fs != nil {
			_ = fs.Close()
		}
		if stopCh != nil {
			close(stopCh)
		}
	})
}

func (w *Watcher) startWatch() error {
	if len(w.dirs) == 0 {
		return nil
	}
	fs, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}
	for _, dir := range w.dirs {
		if dir == "" {
			continue
		}
		// 递归监听所有子目录：skill 位于嵌套子目录的 SKILL.md 中。
		_ = filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return nil // 跳过不可读目录
			}
			if d.IsDir() {
				_ = fs.Add(path)
			}
			return nil
		})
	}

	w.mu.Lock()
	w.fs = fs
	w.stopCh = make(chan struct{})
	w.mu.Unlock()
	go w.watchLoop()
	return nil
}

func (w *Watcher) watchLoop() {
	w.mu.Lock()
	fs := w.fs
	stopCh := w.stopCh
	w.mu.Unlock()
	if fs == nil || stopCh == nil {
		return
	}

	var timer *time.Timer
	for {
		select {
		case <-stopCh:
			return
		case event, ok := <-fs.Events:
			if !ok {
				return
			}
			// 新建目录时加入监听（后续其下的 SKILL.md 才能触发事件）。
			if event.Op&fsnotify.Create != 0 {
				if st, err := os.Stat(event.Name); err == nil && st.IsDir() {
					_ = fs.Add(event.Name)
				}
			}
			if !relevantSkillEvent(event) {
				continue
			}
			if timer != nil {
				timer.Stop()
			}
			timer = time.AfterFunc(w.debounce, func() {
				_ = w.Reload()
			})
		case <-fs.Errors:
			// ignore
		}
	}
}

// relevantSkillEvent 判定事件是否可能影响 skill 集合。
func relevantSkillEvent(event fsnotify.Event) bool {
	if event.Op&(fsnotify.Write|fsnotify.Create|fsnotify.Remove|fsnotify.Rename) == 0 {
		return false
	}
	if strings.EqualFold(filepath.Base(event.Name), "SKILL.md") {
		return true
	}
	// 目录级 Create/Remove/Rename 可能新增/删除 skill 目录，一并触发重载。
	return event.Op&(fsnotify.Create|fsnotify.Remove|fsnotify.Rename) != 0
}
