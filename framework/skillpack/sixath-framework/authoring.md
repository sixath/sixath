# 编写 Framework 可执行的 Skill

本框架的 Skill 是 **运行时资产**，不是 Cursor 专用文件。遵循本文即可让 `skills.NewIndex` 扫描并在 ReAct 中通过工具加载。

## 目录结构

```
my-skill/
├── SKILL.md              # 必需
├── scripts/              # 可选，execute_skill_script 仅允许此目录下
│   └── run.ps1
├── docs/                 # read_skill_file / skill_view linked_files
├── references/
├── assets/
└── templates/
```

放入 `skills_dirs` 指定的目录（如 `skillpack/`、`skills_examples/`）即可被扫描。

## SKILL.md Frontmatter（必需）

```yaml
---
name: my-skill                    # kebab-case，全局唯一
description: >
  一句话说明做什么、何时用。会注入 system 摘要（数十 token）。
tags: [domain, task]
scope: [chat]                     # 可选：chat / dataquery / portal
allowed_tools:                    # 可选：声明期望工具
  - load_skill
  - read_skill_file
  - execute_skill_script
mcp_servers: [my-mcp-id]          # 可选：load_skill 时按需注册 MCP
---
```

正文：工作流、步骤、异常处理、**不要编造执行结果**。

## 运行时工具用法

| 工具 | 说明 |
|------|------|
| `skills_list` | 列 name + description |
| `skill_view(name)` | 读 SKILL.md + linked_files 列表 |
| `skill_view(name, file_path="docs/x.md")` | 读捆绑文件 |
| `load_skill(name)` | 读全文 + 注册 Skill 声明的 MCP |
| `read_skill_file(name, path)` | 读任意捆绑文件（不执行） |
| `execute_skill_script(name, path, input?, args?)` | 执行 scripts/ 下脚本 |

**path 必须以 `scripts/` 开头**，如 `scripts/verify.ps1`。

## 脚本约定

### 扩展名与运行时

| 扩展名 | 命令 |
|--------|------|
| `.sh` | sh |
| `.ps1` | powershell -ExecutionPolicy Bypass -File |
| `.py` | python |
| `.js` | node |

由 `config.skills.script_allowed_extensions` 白名单控制。

### Node.js 可执行入口

若导出 `module.exports = async fn`，末尾需加 stdin/argv 分支，结果 JSON 写 stdout（见 `docs/best-practices.md` §4.5）。

### PowerShell / Shell

可直接运行；输出走 CombinedOutput 返回给模型。

## config.yaml 必配项

```yaml
skills:
  skills_dirs: [skillpack, skills_examples, my-skills]
  allow_script_execution: true    # 否则 execute_skill_script 报错
  script_allowed_extensions: [".sh", ".ps1", ".py", ".js"]
  script_timeout_seconds: 60      # 最大 300
  enabled_skills: []              # 非空 = 白名单
  disabled_skills: []
```

生产环境默认 `allow_script_execution: false`，仅可信 Skill 开启。

## 渐进式披露（三层）

1. **Frontmatter** — 启动时进 system 摘要（name/description）
2. **SKILL.md 正文** — 任务相关时 `load_skill` / `skill_view` 加载
3. **docs/scripts/assets** — 按需 `read_skill_file` 或 `execute_skill_script`

## 验收清单

- [ ] `name` 唯一，kebab-case
- [ ] `description` 含 WHAT + WHEN（第三人称）
- [ ] 正文说明先 `load_skill` 再调其他工具
- [ ] 脚本在 `scripts/` 下，本地手动跑通
- [ ] `go test ./skills/...` 若改了 Index 解析逻辑
- [ ] 配置 `skills_dirs` 后 `skills_list` 能看到该 skill

## 与本仓库示例对照

- 简单：`skills_examples/python-data-helper/`
- 复杂：`skills_examples/skills/archive-move-ops/`（scripts + references + tests）

## 分发给外部 Agent（Cursor 等）

将 Skill 目录放入 `skillpack/<name>/`，附带 `skillpack/install.sh`，用户可：

```bash
npx skills add <owner>/sixath --skill sixath-framework -g -y
```

Framework 运行时与 Cursor 安装 **共用同一份 SKILL.md**。
