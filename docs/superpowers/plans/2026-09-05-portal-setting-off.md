# S38 Portal Setting Off Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 停 `PortalSetting` AutoMigrate 并删模型；不 DROP 表。

**Architecture:** 先锁源文件不含 `PortalSetting`，再从 `data.go` 去掉 AutoMigrate 项并删除 `model/portal_setting.go`。已有 SQLite/MySQL 表不 DROP。

**Tech Stack:** Go、GORM AutoMigrate

**规格:** [`2026-09-05-portal-setting-off-design.md`](../specs/2026-09-05-portal-setting-off-design.md)

**分支:** 从 `feature/s37-proto-dead-keys-off` 切 `feature/s38-portal-setting-off`。不要在 `main` 上改。PowerShell 无 HEREDOC。不要 `--no-verify`。不要提交 `_neo4j_q/`。

---

## File map

| 动作 | 路径 |
|------|------|
| 删 | `portal/internal/data/model/portal_setting.go` |
| 改 | `portal/internal/data/data.go`（AutoMigrate 去掉 `PortalSetting`） |
| 测 | `portal/internal/data/portal_settings_off_test.go` |

禁止：DROP 表；改 Channel；改 MaybeSpill；合 assembler。

---

### Task 1: 失败锁定测试

- [ ] 扩展 `TestPortalSettingsGoRemoved` 的同文件：
  - `TestPortalSettingModelRemoved`：`model/portal_setting.go` 不存在
  - `TestDataGo_omitsPortalSettingAutoMigrate`：`data.go` 源码不含 `PortalSetting`
- [ ] 先跑必须红

```go
func TestPortalSettingModelRemoved(t *testing.T) {
	if _, err := os.Stat("model/portal_setting.go"); err == nil {
		t.Fatal("model/portal_setting.go must not exist")
	} else if !os.IsNotExist(err) {
		t.Fatal(err)
	}
}

func TestDataGo_omitsPortalSettingAutoMigrate(t *testing.T) {
	b, err := os.ReadFile("data.go")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "PortalSetting") {
		t.Fatal("data.go must not AutoMigrate PortalSetting")
	}
}
```

Run: `cd portal; $env:GOMAXPROCS='1'; go test ./internal/data -count=1 -run "TestPortalSettingModelRemoved|TestDataGo_omitsPortalSettingAutoMigrate"`

Expected: FAIL（文件仍在、`data.go` 仍含 `PortalSetting`）

---

### Task 2: 停 AutoMigrate 并删模型

- [ ] `data.go` AutoMigrate 列表去掉 `&model.PortalSetting{},`
- [ ] 删除 `portal/internal/data/model/portal_setting.go`
- [ ] 再跑 Task 1 测试必须绿
- [ ] **Commit** `fix(data): stop AutoMigrate of unused portal_settings model`

---

### Task 3: 回归

- [ ] `cd portal && go test ./internal/data ./internal/conf ./internal/service -count=1`（skip 预存 SQLITE_BUSY / biz 权限用例）
- [ ] 不要 merge/push，除非用户明确要求。
