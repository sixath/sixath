---
id: case-prestart-repair-aborted
status: confirmed
title: 修复模式补丁被中断导致预启动失败
symptom: 实例 vm 预启动失败 prestart game launch failed exit_code=1，同批次其它实例正常
signature:
  - prestart game launch failed exit_code=1
  - repair aborted
  - repair.flag
chain:
  - kind: root
    statement: 补丁 agent 以修复模式校验游戏文件时被文件锁中断（repair aborted），游戏目录停在不一致状态
  - kind: mechanism
    statement: 预启动拉起游戏时加载了不一致的文件，进程 exit_code=1
onset: 修复被中断之后的第一次预启动
last_good: 修复开始之前的最后一次 prestart ok
key_evidence:
  - tool: vm_run_cmd
    quote: "repair aborted: bin/engine.dll is locked by launcher.exe"
  - tool: vm_run_cmd
    quote: repair.flag
verify_probes:
  - tool: vm_run_cmd
    args: {cmd: "type C:\\game\\patch_log\\repair.log"}
    expect: 出现 repair aborted / state=inconsistent 即同一根因
  - tool: vm_run_cmd
    args: {cmd: "dir C:\\game /O-D"}
    expect: 游戏目录下有 repair.flag 或 Patch 目录且修改时间在 onset 之前
fix: 在游戏进程退出后重新执行完整修复（或回滚到修复前版本），并让补丁 agent 在文件被锁时等待而不是中断
created_at: 2026-09-21T10:00:00Z
confirmed_at: 2026-09-21T11:00:00Z
confirmed_by: eval
---

（评测用的已确认案例；正文不参与加载。）
