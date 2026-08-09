# [EX-A2] `ExecuteOptions{}` 零值改为 `ReadOnly=true` (deny by default)

| 字段 | 值 |
|------|-----|
| **优先级** | P0 |
| **模块** | framework/executor |
| **状态** | 已完成 |
| **关联报告** | [02-executor.md A2](../02-executor.md) |
| **预估工作量** | 0.5 天 |
| **依赖** | 无 |

## 问题位置

- `framework/executor/executor.go: ExecuteOptions`
- `framework/executor/mysql.go: (*MySQLExecutor).Execute`
- 全部调用点(grep 一遍 `executor.ExecuteOptions{`)

## 现状

```go
type ExecuteOptions struct {
    Timeout  int
    MaxRows  int
    ReadOnly bool   // 零值 false → 默认允许写
    Params   map[string]any
}
```

```go
// MySQL Execute 内
if opts.ReadOnly && isWriteDSL(dsl) { return nil, ErrReadOnlyViolation }
if isWriteDSL(dsl) { return e.execWrite(ctx, db, dsl) }   // 默认放行
```

## 问题分析

- 任何忘记设 `ReadOnly: true` 的调用方都默认拥有写权限
- LLM Agent 平台对外暴露执行能力,**安全默认值方向反了**
- `MultiExecutor` 当前把 mongo / es 当只读,但**写判定靠默认 false**;若未来加 Mongo 写,定时炸弹

## 改进方案

### 方案 A(推荐): 反转语义,改字段名

```go
type ExecuteOptions struct {
    Timeout       int
    MaxRows       int
    AllowWrite    bool   // 零值 false → 默认禁写,显式 opt-in
    Params        map[string]any
}
```

- 旧 `ReadOnly` 字段标记 deprecated 别名(非 nil-bool 兼容旧调用方一段时间)
- 所有写操作必须显式 `AllowWrite: true`,LLM 工具的 `execute_write` 才传

### 方案 B(温和): 反转默认但保留字段名

```go
type ExecuteOptions struct {
    ReadOnly bool   // **零值改为 true**(via 构造器)
}

func NewExecuteOptions() ExecuteOptions { return ExecuteOptions{ReadOnly: true} }
```

要求所有调用方走 `NewExecuteOptions()` 构造,**直接 `ExecuteOptions{}` 零值不允许使用**。配合 `go vet` 自定义规则。

**推荐方案 A**,语义清晰、方向正确、无歧义。

## 验收标准

- [ ] 新增 `AllowWrite` 字段,deprecated `ReadOnly`(保留兼容字段一个 minor release)
- [ ] 所有 executor 内部判断改为 `if !opts.AllowWrite && isWriteDSL(...)`
- [ ] grep 全代码库,把 `ReadOnly: true` 改成移除该行(因为 read-only 现在是默认),`ReadOnly: false` 改成 `AllowWrite: true`
- [ ] LLM 工具 `execute_read` 不传任何 write 标志即可工作;`execute_write` 必须显式 `AllowWrite: true`
- [ ] CHANGELOG 明确记录:**这是 breaking change**

## 测试要求

- 表驱动单测覆盖:
  - `ExecuteOptions{}` + INSERT → ErrReadOnlyViolation
  - `ExecuteOptions{AllowWrite: true}` + INSERT → 正常执行
  - `ExecuteOptions{AllowWrite: false}` + SELECT → 正常执行
  - 老字段 `ReadOnly: true` 仍能阻止写(兼容期)
- 集成测试: 用 portal 的 `chat` 流走一遍 `execute_read`,断言不会误触发写

## 风险

- **Breaking change**: 所有外部调用方需要审查
- **Mitigation**:
  1. 在 `ExecuteOptions` 加 `// Deprecated: use AllowWrite instead.` 注释
  2. 提供 migration script(sed 一行)
  3. 跨两个 minor release 后再删旧字段

## 关联 issue

- [EX-A1](EX-A1-reader-writer-split.md): Reader/Writer 接口拆分,本 issue 是其铺垫(类型层面消除越权前的最小修补)
