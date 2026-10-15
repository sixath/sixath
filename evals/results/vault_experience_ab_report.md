# Vault 经验库「二次排查命中」A/B 对照报告

- 样本：`evals/tasks/vault_experience.jsonl`（`vault-experience-001`，rca_handbook，portal 模式）
- agent：`b880051a-a7de-4d91-afea-2ad41269191c`（ops-agent）
- 后端：`portal/backend.exe -conf E:/configs/sixath/portal_vault`（8010 端口）
- 差异变量：`SATH_VAULT_ENABLED` / `SATH_VAULT_ROOT`（on 侧设为 true + vault 根目录，off 侧不设）

## 结果对照

| 指标 | on（vault 启用） | off（vault 关闭） |
|------|------------------|-------------------|
| passed | **true** | false（评测超时） |
| steps | **20** | 1（无工具轨迹） |
| tools_used | vault_recall, read_file, skills_list, list_tables, describe_table | （无终答） |
| 终答 | 正确给出两张表 + 关联字段 + 查询模板 | 未产出（10 分钟超时） |

## on 侧行为（`/tmp/vault_on_v3.jsonl`）

agent 先 `vault_recall` 命中已晋升笔记 `archive-unmount-fail.md`（5 次召回，每次 hits=1），
再 `read_file` 读笔记正文，`describe_table` 核对表结构，20 步内输出正确查询路径：

- `t_game_virtual_machine_info`：`flow_id` / `vmid` / `assign_state` / `online_state`
- `t_game_virtual_machine_extend_info`：`game_disk_state` / `deploy_type` / `mount_path` / `mount_recover_path`

机器证据（三重）：
1. portal 启动日志 `vault: wired vault_recall root=... indexed_notes=1`
2. 5 次 `vault_recall: hit` 日志
3. runner 结果 `steps:20` + `tools_used` 含 `vault_recall`

## off 侧行为（本次重跑）

评测 10 分钟超时（`failure_reason: timeout`，`steps:1`，`trace.calls: null`）。
手动复现（SSE 直发同题）观察到 agent 陷入工具探索循环，180 秒无终答：

```
tool_search  ×18   ← 反复搜索「该用什么工具/技能」
tool_describe ×6   ← 反复读工具说明
list_tools    ×2
list_tables   ×2
discover_tools ×1
tool_call     ×3
```

off 侧 agent 靠模型内置知识「重新发现」查询工具与路径，卡在探索阶段无法收敛；
on 侧则因 vault_recall 直接命中既有经验，一步到位。

## 结论

经验库闭环生效：同一 agent、同一问题，**on 侧 20 步命中经验答对，off 侧 10 分钟超时答不出**。
对照符合设计目标——agent 的下一次排查会话通过 `vault_recall` 复用已沉淀经验，
而不是靠模型现场重新摸索工具与查询路径。

## 附注

- off 评测超时并非基础设施故障（off 版 agent 对简单问题「说你好」可正常响应、正常调 skills_list），
  而是问题特异性的「无检索指引时排查类问题探索发散」。
- 该 timeout 与更早的 handbook A/B off 侧 `rca-handbook-004 timeout` 同属一类现象。
