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

// vaultWireState 保存进程级单例 watcher 的创建结果，供 WireVault 幂等返回。
type vaultWireState struct {
	w       *vault.Watcher
	cleanup func()
	err     error
}

var vaultWireResult vaultWireState

// WireVault 装配 vault 索引与 vault_recall 工具。
//
// 降级语义（fail-open）：enabled 为 false、Root 为空、或 embedder/index 未就绪时
// 直接跳过，返回 nil cleanup 且不报错，agent 照常构建，只是没有 vault_recall 能力。
//
// watcher 生命周期是进程级：首次调用成功时创建并返回 cleanup（w.Close），
// 后续调用幂等返回同一 watcher 的 cleanup。上层应把 cleanup 挂到进程退出处；
// 在 service 层无法触达进程级生命周期时，至少 defer 调用（Close 幂等，多余调用无害）。
func WireVault(ctx context.Context, reg *tool.Registry, cfg conf.VaultConfig) (cleanup func(), err error) {
	if !cfg.Enabled || cfg.Root == "" {
		return nil, nil
	}
	opts := DefaultMemoryStoreOptions()
	if opts.UnitEmbedder == nil || opts.UnitVectors == nil {
		slog.Warn("vault: embedder/index not ready, skipping vault_recall wiring",
			"embedder_nil", opts.UnitEmbedder == nil, "index_nil", opts.UnitVectors == nil)
		return nil, nil
	}

	vaultWireOnce.Do(func() {
		w, werr := vault.NewWatcher(cfg.Root, opts.UnitEmbedder, opts.UnitVectors)
		if werr != nil {
			vaultWireResult.err = werr
			return
		}
		vaultWireResult.w = w
		vaultWireResult.cleanup = w.Close
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
	return vaultWireResult.cleanup, nil
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
