# [DS-B3] `ConfigFromMap` 不读连接池参数与 read_only

| 字段 | 值 |
|------|-----|
| **优先级** | P0 |
| **模块** | framework/datasource |
| **状态** | 已完成 |
| **完成批次** | [2026-06-03-p0-quickwin-batch](../../superpowers/plans/2026-06-03-p0-quickwin-batch.md) |
| **关联报告** | [01-datasource.md B3](../01-datasource.md) |
| **预估工作量** | 30 分钟 |
| **依赖** | 无 |

## 问题位置

- `framework/datasource/datasource.go: ConfigFromMap`

## 现状

```go
func ConfigFromMap(m map[string]interface{}) Config {
    var c Config
    if m == nil { return c }
    if v, ok := m["id"].(string); ok { c.ID = strings.TrimSpace(v) }
    if v, ok := m["type"].(string); ok { c.Type = strings.TrimSpace(strings.ToLower(v)) }
    if v, ok := m["dsn"].(string); ok { c.DSN = v }
    if v, ok := m["host"].(string); ok { c.Host = v }
    if p, ok := intFromAny(m["port"]); ok { c.Port = p }
    if v, ok := m["user"].(string); ok { c.User = v }
    if v, ok := m["password"].(string); ok { c.Password = v }
    if v, ok := m["dbname"].(string); ok { c.DBName = v }
    if v, ok := m["read_only"].(bool); ok { c.ReadOnly = v }
    return c
}
```

## 问题分析

`Config` 已经定义了 `MaxOpenConns / MaxIdleConns / ConnMaxLifetime`,而且 `mysqlDataSource` 里也用上了它们。但 `ConfigFromMap` **完全没读这三个字段** —— 也就是说从 portal 通过 map(protobuf Struct → map)配置进来的数据源,**永远拿不到调优参数**:
- `MaxOpenConns=0` → 不限上限,雪崩风险
- `MaxIdleConns=0` → 频繁建连断连
- `ConnMaxLifetime=0` → 长连接被中间件 timeout 断开后驱动不知道,出现 invalid conn 错误

## 改进方案

补齐缺失字段:

```go
if p, ok := intFromAny(m["max_open_conns"]); ok { c.MaxOpenConns = p }
if p, ok := intFromAny(m["max_idle_conns"]); ok { c.MaxIdleConns = p }
if p, ok := intFromAny(m["conn_max_lifetime_sec"]); ok { c.ConnMaxLifetime = p }
```

## 验收标准

- [ ] 通过 map 传入 `max_open_conns` / `max_idle_conns` / `conn_max_lifetime_sec`,生成的 `Config` 字段正确
- [ ] 数值类型容忍(JSON 反序列化可能是 `float64` / `json.Number`),复用已有的 `intFromAny`
- [ ] 不传时保持零值(行为不变)

## 测试要求

补充单测 `TestConfigFromMap_PoolFields`:
```go
m := map[string]interface{}{
    "id": "ds1", "type": "mysql",
    "max_open_conns": 100,
    "max_idle_conns": float64(20),       // 模拟 JSON 反序列化
    "conn_max_lifetime_sec": json.Number("3600"),
}
c := ConfigFromMap(m)
// 断言: c.MaxOpenConns == 100, c.MaxIdleConns == 20, c.ConnMaxLifetime == 3600
```

## 风险

- 极小,仅补缺失映射,不影响现有调用方
