package chat

import (
	"context"
	"log/slog"
	"sync"

	"backend/internal/conf"

	"github.com/sixath/framework/tool"
	toolvault "github.com/sixath/framework/tool/vault"
	"github.com/sixath/framework/vault"
)

// vaultWireOnce 保证 vault watcher 进程级只建一次：fsnotify 监听不该每请求重建。
// 单例快照（byID）由 watcher 内部 atomic.Pointer 持有，Recall 每次读最新快照。
var vaultWireOnce sync.Once

// vaultBuildWatcher 是 watcher 构造入口，默认 vault.NewWatcher；测试可替换为带计数的 fake。
var vaultBuildWatcher = vault.NewWatcher

// vaultStoreOpts 提供 embedder/index，默认 DefaultMemoryStoreOptions；测试可替换。
var vaultStoreOpts = DefaultMemoryStoreOptions

// vaultWireState 保存进程级单例 watcher 的创建结果，供 WireVault 幂等返回。
type vaultWireState struct {
	w   *vault.Watcher
	err error
}

var vaultWireResult vaultWireState

// WireVault 装配 vault 索引与 vault_recall 工具。
//
// 降级语义（fail-open）：enabled 为 false、Root 为空、或 embedder/index 未就绪时
// 直接跳过，返回 nil cleanup 且不报错，agent 照常构建，只是没有 vault_recall 能力。
//
// watcher 生命周期是进程级单例：首次调用成功时创建，随进程存活，由 OS 在进程退出时回收。
// 调用方无需也【不应】关闭它（任一请求 defer 关闭都会终止 fsnotify 监听、使热重载永久失效），
// 因此 cleanup 恒为 nil。
func WireVault(ctx context.Context, reg *tool.Registry, cfg conf.VaultConfig) (cleanup func(), err error) {
	if !cfg.Enabled || cfg.Root == "" {
		return nil, nil
	}
	opts := vaultStoreOpts()
	if opts.UnitEmbedder == nil || opts.UnitVectors == nil {
		slog.Warn("vault: embedder/index not ready, skipping vault_recall wiring",
			"embedder_nil", opts.UnitEmbedder == nil, "index_nil", opts.UnitVectors == nil)
		return nil, nil
	}

	vaultWireOnce.Do(func() {
		w, werr := vaultBuildWatcher(cfg.Root, opts.UnitEmbedder, opts.UnitVectors)
		if werr != nil {
			vaultWireResult.err = werr
			return
		}
		vaultWireResult.w = w
	})
	if vaultWireResult.err != nil {
		// 首建失败：记录并跳过，不中断请求，也不把工具注册成永远报错。
		slog.Warn("vault: watcher init failed, vault_recall not wired", "err", vaultWireResult.err)
		return nil, nil
	}
	w := vaultWireResult.w

	if reg != nil {
		if rerr := toolvault.RegisterVaultRecallTool(reg, func(ctx context.Context, query string, limit int) ([]vault.Hit, error) {
			return vault.Recall(ctx, opts.UnitEmbedder, opts.UnitVectors, w.ByID(), query, limit)
		}); rerr != nil {
			slog.Warn("vault: register vault_recall tool failed", "err", rerr)
		}
	}
	// watcher 是进程级单例，随进程存活，不返回 cleanup（调用方不应关闭）。
	return nil, nil
}

// VaultDepositHint 返回沉淀经验指引；enabled 为 false 返回空串。
// 注入到 agent system prompt，引导 agent 在排查结束时把可复用的经验写入 vault 草稿。
func VaultDepositHint(enabled bool) string {
	if !enabled {
		return ""
	}
	return vaultDepositHintText
}

const vaultDepositHintText = `## 经验沉淀（Experience Vault）

排查结束时，如果你确认某条经验「下次还用得上」（例如：定位某类问题的固定流程、某个工具的坑、某条日志的读法），
请用 workspace 文件写入工具（如 write_file）把它写到 vault 的草稿目录 ` + "`_agent_notes/`" + ` 下。

写入的笔记必须满足：
- frontmatter 必须含 ` + "`status: pending`" + `（草稿，等待人 review 后再晋升）；
- frontmatter 必须含 ` + "`type`" + `（如 workflow / gotcha / tool / case）；
- frontmatter 必须含 ` + "`tags`" + `（逗号分隔的标签）；
- frontmatter 必须含 ` + "`source: session:<session_id>`" + `（记录这次会话来源）。

约束：
- 禁止写 ` + "`ops/`" + `、` + "`mine/`" + `、` + "`shared/`" + ` 等正式目录，只写 ` + "`_agent_notes/`" + `；
- 禁止把 ` + "`status`" + ` 设成 ` + "`promoted`" + `，晋升必须由人 review 完成；
- 不确定是否可复用就不要写。`

// resetVaultWireForTest 清空 Once 与单例结果、恢复默认 seam，供测试重装。
func resetVaultWireForTest() {
	vaultWireOnce = sync.Once{}
	vaultWireResult = vaultWireState{}
	vaultBuildWatcher = vault.NewWatcher
	vaultStoreOpts = DefaultMemoryStoreOptions
}
