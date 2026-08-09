# 跨 Agent 安装与 Framework 运行时

## 安装位置

| 环境 | 全局 | 项目级 |
|------|------|--------|
| Cursor | `~/.cursor/skills/sixath-framework/` | `framework/.cursor/skills/` |
| Claude Code | `~/.claude/skills/` | `framework/.claude/skills/` |
| Codex | `~/.codex/skills/` | — |
| **Framework 扫描** | — | `framework/skillpack/`（config `skills_dirs`） |

权威副本：`framework/skillpack/sixath-framework/`

## Skills CLI

```bash
npx skills add <owner>/sixath --skill sixath-framework -g -y
npx skills add ./framework/skillpack --skill sixath-framework -g -y
```

## Framework 内激活

1. `config.yaml` → `skills.skills_dirs: [skillpack, ...]`
2. 使用 `NewSkillsAwareChatHandlerFromConfig` 或 Portal 等价装配
3. 对话中模型调用 `skill_view("sixath-framework")` 或 `load_skill("sixath-framework")`

## 触发词

- sixath-framework / sath framework / sixath 框架
- ReAct / load_skill / skill_view / execute_skill_script
- context compression / L0 L1 L2 / context_budget
- 写 Skill / SKILL.md / skills_dirs
- go test framework / 改 agent model tool

## 工具映射（Framework 运行时）

| Skill 中动作 | Framework 工具 |
|--------------|----------------|
| 加载本 skill | `skill_view("sixath-framework")` |
| 跑全量测试 | `execute_skill_script(..., "scripts/verify.ps1")` |
| 读扩展文档 | `read_skill_file("sixath-framework", "authoring.md")` |

外部 Agent 无上述工具时：直接 Read 文件 + Shell 跑 `go test`。

## Monorepo 路径

```
sixath/
├── framework/          ← go.mod 在此，go test ./...
│   ├── skillpack/
│   ├── skills/         ← Go 包（Index 代码），不是 Skill 资产目录
│   └── skills_examples/
└── portal/
```
