package vault

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/sixath/framework/memory"
)

const vaultDebounce = 300 * time.Millisecond

// Watcher 在 vault 根上维护向量索引的同步，笔记增删改时热重载。
// 用 atomic.Pointer 持有最新「unitID 集合」快照，供 Recall 反查 byID 与后续 Sync 清理。
type Watcher struct {
	root  string
	embed memory.UnitEmbedder
	idx   memory.UnitVectorIndex
	byID  atomic.Pointer[map[string]Note] // unitID(rel path) -> Note，供召回反查
	rels  atomic.Pointer[[]string]        // 当前 unitID 集合快照，供下次 Sync 清理

	mu     sync.Mutex
	fs     *fsnotify.Watcher
	stopCh chan struct{}
	stop   sync.Once
}

// NewWatcher 建索引并启动目录监听；监听启动失败不致命（仍可手动 Reload）。
func NewWatcher(root string, embed memory.UnitEmbedder, idx memory.UnitVectorIndex) (*Watcher, error) {
	w := &Watcher{root: root, embed: embed, idx: idx}
	if err := w.Reload(); err != nil {
		return nil, err
	}
	_ = w.startWatch() // 失败忽略
	return w, nil
}

// Reload 重扫 vault、同步索引、更新快照；失败保留旧状态并返回错误。
func (w *Watcher) Reload() error {
	prev := w.currentRels()
	rels, err := Sync(context.Background(), w.embed, w.idx, w.root, prev)
	if err != nil {
		return err
	}
	notes, err := Scan(w.root)
	if err != nil {
		return err
	}
	byID := make(map[string]Note, len(notes))
	for _, n := range notes {
		rel, _ := filepath.Rel(w.root, n.Path)
		byID[filepath.ToSlash(rel)] = n
	}
	w.rels.Store(&rels)
	w.byID.Store(&byID)
	return nil
}

// ByID 返回当前 unitID->Note 快照（供召回反查）。
func (w *Watcher) ByID() map[string]Note {
	if w == nil {
		return nil
	}
	p := w.byID.Load()
	if p == nil {
		return nil
	}
	return *p
}

func (w *Watcher) currentRels() []string {
	p := w.rels.Load()
	if p == nil {
		return nil
	}
	return *p
}

// Close 停止监听，幂等。
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

// startWatch 递归监听 root 下所有子目录（参照 skills/watcher.go），
// 变化经 debounce 去抖后触发 Reload。
func (w *Watcher) startWatch() error {
	if w.root == "" {
		return nil
	}
	fs, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}
	_ = filepath.WalkDir(w.root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil // 跳过不可读目录
		}
		if d.IsDir() {
			_ = fs.Add(path)
		}
		return nil
	})

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
			// 新建目录时加入监听（后续其下的 .md 才能触发事件）。
			if event.Op&fsnotify.Create != 0 {
				if st, err := os.Stat(event.Name); err == nil && st.IsDir() {
					_ = fs.Add(event.Name)
				}
			}
			if !relevantNoteEvent(event) {
				continue
			}
			if timer != nil {
				timer.Stop()
			}
			timer = time.AfterFunc(vaultDebounce, func() {
				_ = w.Reload()
			})
		case <-fs.Errors:
			// ignore
		}
	}
}

// relevantNoteEvent 判定事件是否可能影响笔记集合。
func relevantNoteEvent(event fsnotify.Event) bool {
	if event.Op&(fsnotify.Write|fsnotify.Create|fsnotify.Remove|fsnotify.Rename) == 0 {
		return false
	}
	if strings.EqualFold(filepath.Ext(event.Name), ".md") {
		return true
	}
	// 目录级 Create/Remove/Rename 可能新增/删除笔记目录，一并触发重载。
	return event.Op&(fsnotify.Create|fsnotify.Remove|fsnotify.Rename) != 0
}
