# 代码根预挂载 + Agent Workspace 选择（模式 C）

> 状态：已实现（M0/M1）  

> 日期：2026-08-13  
> 评审：已吸收 blocking 修订（整仓/ro、conf.proto、创建流、鉴权、里程碑）

## 1. 背景与目标

Docker **无法**在运行中由 Web 安全地动态 bind-mount 宿主机任意路径。采用：

1. Compose **预先**把宿主机代码根挂到容器固定点 `/mnt/codes`（默认 **ro**）
2. Web 在 Agent 表单里 **浏览并选择** 该根下的子目录
3. 支持模式 **C**：
   - **整仓作 Workspace**：`workspace = /mnt/codes/<rel>`（只读场景，受限）
   - **挂到子目录（推荐默认）**：`workspace = {data_root}/agents/{id}/`，`{workspace}/code` → symlink → 所选路径

**非目标：** 动态 Docker volume、浏览任意绝对路径、多租户分 root、特权 `mount --bind`、本版 rw 挂载。

挂载范围仅 **portal** 容器（gateway/web 不碰 workspace FS）。

## 2. 决策（已锁定）

| 项 | 选择 |
|----|------|
| 模式 | C；**UI 默认「挂到 workspace/code」** |
| 浏览范围 | 仅 `code_roots`（默认 `/mnt/codes`） |
| 默认挂载 | **ro** |
| 链接名 | 固定 **`code`**（本版不可改；减少 UI/API 分叉） |
| 权限 | browse：登录且可管理 Agent；link：对该 Agent 的编辑权限 |

## 3. 整仓模式 + ro 的约束（blocking 已澄清）

`/mnt/codes` 为 ro 时，下列写路径会失败或应被拒绝：

- 技能包解压到 `workspace/skills`
- Growth / skill 写盘、workspace write/patch（若开启）
- 其它以 Workspace 为根的写入

**产品规则：**

| 模式 | 行为 |
|------|------|
| **子目录（默认）** | `workspace` 在可写 `data_root`；skills / 工具写入正常；只读代码经 `workspace/code` 阅读 |
| **整仓** | 允许保存；UI 显著提示「代码根只读」；**禁用**技能上传入口（或上传 API 若 `workspace` 落在 `code_roots` 下则返回明确 400）；文档说明 MEA/写工具在 ro 下会失败 |

推荐用户日常用子目录模式；整仓仅用于「只读浏览代码 + 对话」类 Agent。

## 4. 基础设施

### 4.1 docker-compose

```yaml
# portal volumes
- ${HOST_CODE_ROOT:-./codes}:/mnt/codes:ro
```

- Windows 例：`HOST_CODE_ROOT=E:/workspace/github`
- 仓库可 gitignore `codes/`；默认空目录保证未配置也能启动

### 4.2 配置落地（须改 conf）

1. `portal/internal/conf/conf.proto` 的 `Data` 增加 `repeated string code_roots = N`
2. 重新生成 `conf.pb.go`
3. `config.docker.yaml`：

```yaml
data:
  data_root: /data/portal
  code_roots:
    - /mnt/codes
```

4. Wire：browse / workspace-link 从 conf 读取；本地空 `code_roots` → browse 空列表，仅手输 workspace

### 4.3 .env.example

在现有 `PORTAL_DATA_DIR` 旁追加：

```env
# Host dir → portal /mnt/codes (read-only). Default ./codes
HOST_CODE_ROOT=./codes
```

## 5. API

### 5.1 `GET /api/v1/code-roots`

返回配置且目录存在的 roots。

**鉴权：** 已登录 + 具备 Agent 管理/创建能力（与「打开新建 Agent 页」同级；**不**绑定具体 agent id）。

### 5.2 `GET /api/v1/code-roots/browse`

| 参数 | 说明 |
|------|------|
| `root` | 必须是 `code_roots` 之一 |
| `path` | 相对 root；默认空；禁止绝对路径与空字节 |

响应示例（`path=""` 时根下条目）：

```json
{
  "root": "/mnt/codes",
  "path": "",
  "entries": [
    { "name": "sixath", "path": "sixath", "type": "dir" }
  ]
}
```

选中后 Web 使用服务端拼好的绝对路径（或再调一次 resolve）；服务端用 `filepath.Join` + Clean，不用字符串 `+ "/"`。

仅列 **目录**。上限：深度 32、单次 500 条。

**安全：** Clean；拒 `..`；存在则 `EvalSymlinks`；root 与目标均规范化后做前缀校验（注意尾部 `/`）。

### 5.3 `POST /api/v1/agents/{id}/workspace-link`

Body：`{ "target": "/mnt/codes/foo/bar" }`（link 名固定 `code`）

- 校验 target ∈ code_roots（EvalSymlinks + 前缀）
- 在 `{agent.Workspace}/code` 创建 symlink
- 已存在且指向不同目标 → **409**；本版不提供 DELETE（运维：容器内删 `code` 或后续加 unlink）
- **鉴权：** 对该 Agent 的 `PermEdit`（或等价）

### 5.4 创建流（子目录模式）— 与现表单对齐

现网 AgentForm **强制 workspace 非空**；服务端在 workspace 空时才填默认 `{data_root}/agents/{id}`。

**M1/M2 UI 约定：**

1. 子目录模式：提交前把 workspace 设为占位或显式 `{data_root}/agents/...`——更稳：  
   - **Create：** `workspace` 传空字符串 **或** 放宽校验改为「子目录模式允许空，由服务端默认」；响应含 `id` + 最终 `workspace`  
   - 立即 `workspace-link`；失败则展示错误，**Agent 已创建、无 link**（可重试 link；不自动回滚删 Agent）
2. 整仓模式：workspace = browse 绝对路径；**不**调 workspace-link；禁用技能上传 UI

须改 AgentForm：子目录模式允许空 workspace；create 后用返回 id 调 link，再 navigate。

## 6. Web UI

- 默认选中「挂到 workspace/code」
- 「浏览代码根」→ 选目录 → 显示目标路径
- 整仓：写入 workspace 输入框 + 只读警告 + 隐藏/禁用技能上传（详情页同样按 workspace 是否在 code_roots 下判断）
- 保留手输 workspace（高级）

## 7. 与现有行为

| 现有 | 关系 |
|------|------|
| `data_root` 卷 | 不变；子目录模式 skills 仍在可写 workspace |
| 手输 workspace | 保留 |
| MEA WorkDir | = Agent.Workspace（整仓=只读树；子目录=可写根，代码在 `code/`） |

## 8. 测试

- browse：正常、`..`、symlink 逃逸、绝对 path、空 code_roots
- workspace-link：成功、越界 400、冲突 409
- 整仓 + ro：UploadSkill → 明确失败/拒绝
- 子目录：skills 仍可写；`code` 指向只读目标可读
- compose：默认 `./codes` 可 up

## 9. 里程碑

| 阶段 | 内容 |
|------|------|
| **M0** | conf.proto `code_roots` + 生成 + compose ro + browse API + 单测 |
| **M1** | AgentForm 浏览 + **默认子目录模式** + create→workspace-link；整仓可选但带 ro 约束 |
| **M2** | 详情页按 code_roots 禁用技能上传；冲突/重试文案；可选 unlink |
| **M3** | rw 开关、多 root、列文件 |

**M0 后 M1 必须同时交付子目录链路**（不把「仅整仓」作为可发布中间态）。

## 10. 风险

- Win/WSL 路径与 bind 性能：文档说明 `HOST_CODE_ROOT`
- Symlink 建在 **Linux `data_root` 卷** 上，指向 `/mnt/codes/...`，不在 Windows 侧建链接
