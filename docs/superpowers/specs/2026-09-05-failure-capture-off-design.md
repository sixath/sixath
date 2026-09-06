# S32 收口：删除无调用者的 FailureCaptureHook

**日期**: 2026-09-05  
**状态**: 已确认（用户继续清货架；S31 leftover；2026-09-05 实施）  
**范围**: `framework/harness/failure_capture_hook.go` 中的 Hook 类型。保留 `WithRequestMetadata`（ReAct 仍注入）。不删 `framework/growth`。  
**父规格**: [`2026-09-05-agent-model-workspace-harness-design.md`](./2026-09-05-agent-model-workspace-harness-design.md)  
**前置**: [S31](./2026-09-05-growth-shell-leftover-off-design.md)；[S12](./2026-09-05-unwire-growth-react-opts-design.md)

**一句话**: 默认 Harness 已不装配 FailureCapture；实现只剩包内测试，删掉以免 env 打开就假装还能写 ERRORS.md。

---

## 1. 背景

S12 把 FailureCapture 并进 `HarnessReActOptions`；S31 拆掉默认装配。现网：

- `NewFailureCaptureHook` 生产路径零调用（只活在 `failure_capture_hook.go` 与其测试）
- 同文件的 `WithRequestMetadata` 仍被 `react_agent.go` 调用，**必须留下**

skillops / worker 仍 import growth，**不在本刀**。

---

## 2. 已锁定决策

| 项 | 选择 |
|----|------|
| `FailureCaptureHook` / `FailureCaptureConfig` / `NewFailureCaptureHook` | **删除** |
| `WithRequestMetadata` / `RequestMetadataFromContext` | **保留**（迁到 `request_metadata.go`） |
| `SATH_GROWTH_FAILURE_CAPTURE` | 默认入口仍无效（无装配点） |
| `framework/growth` / skillops / assembler | **不改 / 不合** |

---

## 3. 行为

```text
NewFailureCaptureHook → 不存在
ReAct 仍 WithRequestMetadata 给 ToolHook
默认 Chat 仍不写 .learnings/ERRORS.md
harness 包不再 import growth
```

---

## 4. 非目标

- 不删 `framework/growth`
- 不改 skill_manage / append_learning
- 不合 assembler

---

## 5. 成功标准

1. `framework/harness/failure_capture_hook.go` 不存在。
2. 现网 `*.go`（排除 `_neo4j_q`）不含 `NewFailureCaptureHook`。
3. `cd framework && go test ./harness ./tool ./templates -count=1` 绿。
4. `TestHarnessGo_doesNotWireFailureCapture` 仍通过。
