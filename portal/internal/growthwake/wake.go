package growthwake

import "sync"

// 供 biz 在置位 pending 后唤醒 growth worker（非阻塞；未 Register 时为 no-op）。

var (
	mu sync.RWMutex
	fn func()
)

// Register 由进程内唯一 GrowthWorker 在构造时注册；重复调用覆盖上一次。
func Register(f func()) {
	mu.Lock()
	defer mu.Unlock()
	fn = f
}

// Wake 触发一次 worker 轮询（实现为向带缓冲 channel 发送，丢重即可）。
func Wake() {
	mu.RLock()
	f := fn
	mu.RUnlock()
	if f != nil {
		f()
	}
}
