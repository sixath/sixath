---
name: vm_log_analyze
version: 2.0.0
description: 进入云游戏实例查 agent（cgvmagent / xagent）日志。必须先在 D 盘搜索日志文件拿到真实路径，禁止假设日志目录。
tags: [vm, log, analyze, trace]
allowed_tools: [vm_run_cmd]
---

# VM 日志分析

根据 traceId / flow_id 查询云游戏实例上 agent 的日志，帮助定位问题。

实例上的 agent 进程是 `cgvmagent.exe` 或 `xagent.exe` 之一，日志目录因实例而异
（例如 `D:\CloudGameBundle\logs\cgvmagent\cgvmagent.log`），不能写死。

## 执行顺序（不可跳过）

1. **定位日志文件**：在 D 盘搜索 cgvmagent.log 或 xagent.log
   ```
   vm_run_cmd(vmid="<vmid>", op="find", path="D:\\", patterns=["cgvmagent.log", "xagent.log"])
   ```
   - 输出即日志完整路径
   - 命中多个时，对各候选目录用 `op="ls_recent"`，选修改时间最新的
   - 找不到时如实告诉用户，不要猜路径

2. **查询日志**：用第 1 步拿到的路径
   - 按 traceId / flow_id 过滤：
     ```
     vm_run_cmd(vmid="<vmid>", op="grep", path="<日志路径>", patterns=["<traceId>"])
     ```
   - 只看错误：`patterns=["<traceId>", "error", "warn"]` 会按"任一命中"返回，需要时再对结果二次筛选
   - 看最新内容：`vm_run_cmd(vmid="<vmid>", op="tail", path="<日志路径>", lines=200)`
   - 看同目录的轮转 / 历史日志：`vm_run_cmd(vmid="<vmid>", op="ls_recent", path="<日志所在目录>")`

**禁止**：跳过第 1 步直接使用 `D:\CloudGameBundle\apps\cgvmagent\current\logs` 等固定路径。

## 参数说明

| 参数 | 来源 |
|------|------|
| vmid / host | 用户提供，或来自上游排查结果；只给 vmid 时工具会自动解析实例地址 |
| trace_id / flow_id | 用户提供 |
| 日志路径 | 必须来自第 1 步 `op=find` 的输出 |

## 注意

- `vm_run_cmd` 只执行 cmd.exe 命令，不要发送 PowerShell（Get-Content、Select-String 等）。
- 查日志优先用 `op=grep` / `op=tail`，不要 `type` 整个大文件（输出过大时只能拿到文件开头的旧内容）。
- grep 结果为空不等于没有问题，可能是关键字不对或日志已轮转，应结合 `ls_recent` 查看历史文件。
