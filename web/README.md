# Web

Agent 平台前端，基于 React + Vite 构建，对接 [portal](../portal) 后端 API，提供工具管理、Agent 管理、流式对话等功能。

## 功能概览

| 模块 | 说明 |
|------|------|
| **对话** | 首页 Agent 选择器 + 流式对话，支持 Markdown 渲染、代码高亮 |
| **工具管理** | 工具列表、新建/编辑（内置、MCP、数据源） |
| **Agent 管理** | Agent 列表、新建/编辑、详情、技能包上传、工具绑定、对话入口 |
| **渠道管理** | 渠道列表、新建/编辑（web/api/webhook），Webhook 入站配置 |
| **定时任务** | 定时任务列表、新建/编辑、详情、立即执行、执行历史 |

## 技术栈

- **框架**：React 19 + TypeScript
- **构建**：Vite 8
- **路由**：React Router 7
- **Markdown**：react-markdown + remark-gfm + rehype-highlight

## 快速开始

### 前置条件

- Node.js 18+
- 后端 portal 已启动（默认 `http://localhost:8000`）

### 安装与运行

```bash
# 安装依赖
npm install

# 开发模式（默认端口 5173，/api 代理到 localhost:8000）
npm run dev

# 构建
npm run build

# 预览构建产物
npm run preview
```

## E2E 测试（Playwright）

UI 层 E2E 使用 **API Mock**，无需 MySQL/Portal；会自动启动 Vite dev server。

```bash
cd web
npm install
npx playwright install chromium
npm run test:e2e
```

可选：针对 **真实 Portal + MySQL** 的全栈冒烟（需 portal 在 `localhost:8000`）：

```powershell
# PowerShell
$env:E2E_LIVE='1'
npm run test:e2e:live
```

| 脚本 | 说明 |
|------|------|
| `npm run test:e2e` | Mock API，覆盖 runtime_tools 表单/详情/保存 payload |
| `npm run test:e2e:ui` | Playwright UI 调试模式 |
| `npm run test:e2e:live` | 全栈（需 `E2E_LIVE=1`） |

报告输出：`web/playwright-report/`、 `web/output/playwright/test-results/`

开发模式下，`/api` 请求会代理到 `http://localhost:8000`，需确保 portal 已启动。

## 路由

| 路径 | 说明 |
|------|------|
| `/` | 首页：Agent 选择 + 对话 |
| `/tools` | 工具列表 |
| `/tools/new` | 新建工具 |
| `/tools/:id/edit` | 编辑工具 |
| `/agents` | Agent 列表 |
| `/agents/new` | 新建 Agent |
| `/agents/:id/edit` | 编辑 Agent |
| `/agents/:id` | Agent 详情 |
| `/agents/:id/chat` | 对话页（无 session 时自动创建） |
| `/agents/:id/chat/:sessionId` | 对话页（指定 session） |
| `/channels` | 渠道列表 |
| `/channels/new` | 新建渠道 |
| `/channels/:id/edit` | 编辑渠道 |
| `/cron` | 定时任务列表 |
| `/cron/new` | 新建定时任务 |
| `/cron/:id` | 任务详情与执行历史 |
| `/cron/:id/edit` | 编辑定时任务 |

## 项目结构

```
web/
├── src/
│   ├── api/
│   │   └── client.ts      # API 封装（tool、agent、chat）
│   ├── components/
│   │   └── MarkdownContent.tsx  # Markdown 渲染 + 代码高亮
│   ├── pages/
│   │   ├── ChatHome.tsx   # 首页：Agent 选择 + ChatPage
│   │   ├── ChatPage.tsx   # 对话区（流式 SSE）
│   │   ├── ToolList.tsx
│   │   ├── ToolForm.tsx
│   │   ├── AgentList.tsx
│   │   ├── AgentForm.tsx
│   │   └── AgentDetail.tsx
│   ├── App.tsx
│   └── App.css
├── vite.config.ts         # /api 代理到 localhost:8000
└── package.json
```

## API 代理

`vite.config.ts` 中配置：

```ts
server: {
  port: 5173,
  proxy: {
    '/api': {
      target: 'http://localhost:8000',
      changeOrigin: true,
    },
  },
}
```

生产环境需通过 Nginx 等将 `/api` 反向代理到 portal 服务。

### Docker Compose

与 MySQL、portal 一起在 **monorepo 根目录**启动完整栈：

```bash
# 在仓库根目录（与 portal、framework 同级）
docker compose up --build -d
```

浏览器访问 http://localhost:18080 。镜像内 nginx（[`nginx.conf`](nginx.conf)）将 `/api` 反代到 `portal:8000`，并拉长超时以支持 SSE 流式对话。

Compose 宿主端口映射（避免本机冲突）：

| 服务 | 地址 |
|------|------|
| Web | http://localhost:18080 |
| Portal | http://localhost:18000 |
| MySQL | localhost:13306 |

单独构建前端镜像：

```bash
cd web
docker build -t sixath-web .
```

更完整说明见 monorepo 根目录 [`README.md`](../README.md)。

## 流式对话

- 使用 `POST /api/v1/sessions/:id/messages/stream` 发送消息
- 响应为 SSE：`chunk` 增量文本、`done` 结束、`error` 错误
- 支持停止生成（AbortController）
