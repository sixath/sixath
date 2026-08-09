---
name: vm_log_analyze
version: 1.0.0
description: 根据 TraceId 查询 VM 日志。必须先 describe_table → execute_read → http_request，禁止跳过。
tags: [vm, log, analyze, trace]
allowed_tools: [list_tables, describe_table, execute_read, http_request]
---

# VM 日志分析

根据 TraceId 查询相关 VM 的日志，帮助定位问题。

## 执行顺序（不可跳过）

1. **describe_table**：获取 `t_game_virtual_machine_info` 表结构
2. **execute_read**：查询 VM 信息，拿到 `mgr_ipv4_address`
3. **构建HTTP请求**: 
	   - 使用 http_request 工具发送HTTP请求
	   - 请求URL格式: http://{ip}:49997/v1/taskmanager/exec?shell=ps
	   - 请求方法: POST
	   - 查询参数shell的值是PowerShell命令
	   - **timeout_seconds=300**（日志查询可能较慢，需 5 分钟超时）
       - 其中 {ip} 是execute_read执行返回的mgr_ipv4_address（例如: 192.101.2.23）
	   
4. **构建PowerShell命令**: 
	   - 命令模板: Get-Content -Path "D:\\CloudGameBundle\\apps\\cgvmagent\\current\\logs\\cgvmagent.log" |Select-String "{traceId}" |Where-Object { $_ | Select-String "error","warn","ERROR", "Error" -Quiet } |Select-Object -Last 10
	   - 其中 {traceId} 是用户提供的Trace ID（例如: f3264eec912651f263ab86f5ace1499a）
	   - 注意：路径中的反斜杠需要转义为 \\\\，命令中的引号需要正确转义

5. **执行日志查询**: 
	   - 将完整的PowerShell命令作为shell参数的值
	   - URL编码处理：需要对特殊字符进行URL编码（如空格、|、引号等）
	   - 执行HTTP请求获取日志内容

**禁止**：跳过 1、2 直接调用 http_request。mgr_ipv4_address 只能从 execute_read 结果获取，禁止杜撰。

## 三步调用示例

```
describe_table(table_name="t_game_virtual_machine_info", datasource_id="...")
execute_read(dsl="SELECT mgr_ipv4_address, id, name FROM t_game_virtual_machine_info WHERE ...", datasource_id="...")
http_request(url="http://{ip}:49997/v1/taskmanager/exec?shell=ps", method="POST", body="...")
```

第三步仅当第二步有返回结果时执行

## 参数说明

| 参数 | 来源 |
|------|------|
| trace_id | 用户提供 |
| mgr_ipv4_address | 必须来自 execute_read 返回的该列 |
| timeout_seconds | http_request 超时，建议 300（5 分钟） |

## 前置条件

- Agent 需**绑定数据源工具**，能访问 `t_game_virtual_machine_info` 表（含 mgr_ipv4_address 列）
- 需开启 `skills.allow_script_execution`
- 若脚本执行被禁用，向用户说明需开启配置，不要编造结果

## datasource_id 说明

- 若 Agent 已绑定数据源，`describe_table` 和 `execute_read` 通常有默认 datasource_id，可直接调用，无需用户提供。
- 若调用时报「datasource_id is required」，应向用户说明：

  > 根据当前错误信息，您需要在 Agent 管理页中为该 Agent 绑定一个数据源工具，类型为 datasource，并指向包含 t_game_virtual_machine_info 表的数据库。绑定完成后，我们可以继续执行查询以获取 mgr_ipv4_address 并进一步分析日志。如果您已经绑定了数据源，请提供数据源 ID。
- 若用户主动提供 datasource_id（如「用数据源 ds1」），则将其传入 describe_table 和 execute_read 的 datasource_id 参数。

