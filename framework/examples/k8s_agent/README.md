# K8s Skill 使用示例

本示例演示如何通过 Skills-aware Chat Handler 使用 [k8s-ops](../../k8s-skill/SKILL.md) Skill：从指定目录加载 Skill 索引，在对话中由模型按需 `load_skill("k8s-ops")` 获取 K8s 运维指南，并可选地接入 mcp-k8s MCP 与集群交互。

## 前置条件

- 已设置 `OPENAI_API_KEY`
- 可选：`OPENAI_BASE_URL`、`OPENAI_MODEL`

## 运行方式

在**仓库根目录**执行：

```bash
# 使用默认 k8s 相关问题（会引导模型加载 k8s-ops）
go run ./examples/k8s_agent

# 自定义问题
go run ./examples/k8s_agent -message "请先加载 k8s-ops，再告诉我如何排查 Pod ImagePullBackOff"

# 指定 skills 目录（多个用逗号分隔）
go run ./examples/k8s_agent -dirs examples/k8s-skill -message "default 命名空间下有哪些 Pod？"
```

## 可选：接入 mcp-k8s MCP

若已部署 id 为 `mcp-k8s` 的 MCP 服务，可在加载 k8s-ops 后自动将该 MCP 的工具注册到当前上下文：

```bash
go run ./examples/k8s_agent \
  -mcp-endpoint http://localhost:8080/mcp \
  -mcp-backend metoro \
  -message "请加载 k8s-ops 并列出 default 命名空间的 Pod"
```

- `-mcp-endpoint`：MCP 服务地址（必填时表示启用 MCP）
- `-mcp-backend`：`metoro` 或 `mark3labs`，默认 `metoro`

## 行为说明

1. 启动时从 `-dirs` 扫描 SKILL.md，构建技能索引（默认包含 `examples/k8s-skill`）。
2. 系统提示中会包含可用 Skills 摘要，模型看到「k8s-ops」及其描述。
3. 用户提问 K8s 相关问题时，模型会先调用 `load_skill("k8s-ops")` 获取完整操作指南与工作流。
4. 若配置了 `-mcp-endpoint`，加载 k8s-ops 后框架会注册 mcp-k8s，后续步骤可调用该 MCP 提供的工具。
