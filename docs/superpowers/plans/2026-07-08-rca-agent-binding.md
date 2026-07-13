# RCA 工具接入 Agent 绑定 UI — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax.

**Goal:** 在 portal 新增顶级工具类型 `rca`,让用户能在"新建工具"页创建并配置 RCA 工具、绑定给 Agent,运行时按配置构造出 framework 的 RCA 工具。

**Architecture:** portal 后端加 `ToolTypeRCA` 类型 + 校验 + `BuildRegistry` 运行时分发(按 `func_path` 调 framework 的 `RegisterRCACodeTools`/`RegisterJaegerTool`/`RegisterESLogTool`);web 前端 ToolForm 加 RCA 表单。framework 与 proto 不改。

**Tech Stack:** Go(portal,go-kratos,gorm,structpb)、React/TS(web)、framework tool 包(已实现的 RCA 工具)。

**目标目录:** 主 `portal/`、`web/`(不碰 `.worktrees/page-confirmation/`)。portal 的 `go` 命令在 `D:\workspace\github\sixath\portal` 下执行。

**Spec:** `docs/superpowers/specs/2026-07-08-rca-agent-binding-design.md`

**已验证的关键事实(实现时依赖):**
- `biz.ToolType` 常量在 `portal/internal/biz/tool.go:18-22`;`Create` 对非法 type 回退 builtin(:75-77);`Update` 直接透传 `type`(:119-121)。
- `BuildRegistry`(`portal/internal/chat/agent_builder.go:64`)`for _, t := range tools { cfg := toolConfigToMap(t.Config); switch t.Type {...} }`。`toolConfigToMap`(:211)把 `*structpb.Struct` 转 `map[string]interface{}`。
- `registerBuiltinTool(reg, cfg)`(:226)按 `cfg["func_path"]` switch。
- framework:`tool.RegisterRCACodeTools(reg, roots []string) error`、`tool.RegisterJaegerTool(reg, queryURL string) error`、`tool.RegisterESLogTool(reg, reader executor.Reader, cfg tool.ESLogConfig) error`,`tool.ESLogConfig{DatasourceID, DefaultIndex, TraceIDField string}`。
- `*executor.ESExecutor` 实现 `executor.Reader`(有 `Query`);`executor.NewESExecutor(dsReg)`。`datasource.NewRegistry()`/`datasource.RegisterElasticsearch(dsReg)`/`dsReg.Register(datasource.Config)`/`datasource.ConfigFromMap(map)`。

---

## File Structure

| 文件 | 改动 | 责任 |
|------|------|------|
| `portal/internal/biz/tool.go` | 加 `ToolTypeRCA` 常量;`Create` 允许 rca;新增 `validRCAFuncPath` 校验 | 类型合法性 |
| `portal/internal/biz/tool_test.go` | 校验单测 | — |
| `portal/internal/chat/rca_builder.go`（新建） | `registerRCATool(reg, cfg, agentTools)` + `buildESReaderFromAgentTools` | 运行时构造 RCA 工具 |
| `portal/internal/chat/rca_builder_test.go`（新建） | 三分支单测 | — |
| `portal/internal/chat/agent_builder.go` | `BuildRegistry` switch 加 `case biz.ToolTypeRCA` | 分发 |
| `web/src/api/client.ts` | type 联合 + ToolConfig 加 rca | 前端类型 |
| `web/src/pages/ToolForm.tsx` | type 下拉加 RCA + 条件表单 | 前端表单 |

> 运行时逻辑单独放 `rca_builder.go`,不塞进已较大的 `agent_builder.go`,便于独立测试。

---

## Task 1: biz 层新增 RCA 类型与校验

**Files:**
- Modify: `portal/internal/biz/tool.go`
- Test: `portal/internal/biz/tool_test.go`

- [ ] **Step 1: 写失败测试**

在 `portal/internal/biz/tool_test.go` 追加(若无该文件则创建 `package biz`,import `testing`):

```go
func TestValidRCAFuncPath(t *testing.T) {
	for _, fp := range []string{"rca_code", "jaeger_trace", "es_log_query"} {
		if !validRCAFuncPath(fp) {
			t.Fatalf("%q should be valid", fp)
		}
	}
	for _, fp := range []string{"", "rca_grep", "unknown"} {
		if validRCAFuncPath(fp) {
			t.Fatalf("%q should be invalid", fp)
		}
	}
}

func TestToolType_RCAIsValid(t *testing.T) {
	if !isValidToolType(string(ToolTypeRCA)) {
		t.Fatal("rca must be a valid tool type")
	}
	if isValidToolType("bogus") {
		t.Fatal("bogus must be invalid")
	}
}
```

- [ ] **Step 2: 运行,确认失败**

Run: `cd /d/workspace/github/sixath/portal && go test ./internal/biz/ -run 'TestValidRCAFuncPath|TestToolType_RCAIsValid' -v`
Expected: 编译失败 `undefined: validRCAFuncPath` / `isValidToolType` / `ToolTypeRCA`。

- [ ] **Step 3: 实现**

在 `portal/internal/biz/tool.go` 的 const 块加入 `ToolTypeRCA`:
```go
const (
	ToolTypeBuiltin    ToolType = "builtin"
	ToolTypeMCP        ToolType = "mcp"
	ToolTypeDatasource ToolType = "datasource"
	ToolTypeRCA        ToolType = "rca"
)
```

加入两个 helper(放在 const 块下方):
```go
// isValidToolType 返回 t 是否为已知工具类型。
func isValidToolType(t string) bool {
	switch ToolType(t) {
	case ToolTypeBuiltin, ToolTypeMCP, ToolTypeDatasource, ToolTypeRCA:
		return true
	default:
		return false
	}
}

// validRCAFuncPath 返回 fp 是否为受支持的 RCA 子工具。
func validRCAFuncPath(fp string) bool {
	switch fp {
	case "rca_code", "jaeger_trace", "es_log_query":
		return true
	default:
		return false
	}
}
```

修改 `Create`,用 `isValidToolType` 替换硬编码判断(保留"非法回退 builtin"行为,但把 rca 纳入合法):
```go
func (uc *ToolUsecase) Create(ctx context.Context, name, description, toolType string, config *structpb.Struct) (*ToolMeta, error) {
	tt := ToolType(toolType)
	if !isValidToolType(toolType) {
		tt = ToolTypeBuiltin
	}
	tool, err := uc.repo.Create(ctx, name, description, tt, config)
	if err != nil && errors.Is(err, pkgErrors.ErrDuplicateName) {
		return nil, ErrToolDuplicateName
	}
	return tool, err
}
```

- [ ] **Step 4: 运行,确认通过**

Run: `cd /d/workspace/github/sixath/portal && go test ./internal/biz/ -run 'TestValidRCAFuncPath|TestToolType_RCAIsValid' -v`
Expected: PASS。再跑整包:`go test ./internal/biz/` → ok。

- [ ] **Step 5: Commit**

```
cd /d/workspace/github/sixath/portal
git add internal/biz/tool.go internal/biz/tool_test.go
git commit -m "feat(biz): add rca tool type and func_path validation"
```

---

## Task 2: RCA 运行时构造器

**Files:**
- Create: `portal/internal/chat/rca_builder.go`
- Test: `portal/internal/chat/rca_builder_test.go`

- [ ] **Step 1: 写失败测试**

创建 `portal/internal/chat/rca_builder_test.go`:

```go
package chat

import (
	"testing"

	"backend/internal/biz"
	"github.com/sixath/framework/tool"
	"google.golang.org/protobuf/types/known/structpb"
)

func rcaMeta(t *testing.T, m map[string]any) *biz.ToolMeta {
	t.Helper()
	s, err := structpb.NewStruct(m)
	if err != nil {
		t.Fatalf("structpb: %v", err)
	}
	return &biz.ToolMeta{Type: biz.ToolTypeRCA, Config: s}
}

func has(reg *tool.Registry, name string) bool {
	_, ok := reg.Get(name)
	return ok
}

func TestRegisterRCATool_Code(t *testing.T) {
	reg := tool.NewRegistry()
	cfg := map[string]any{"func_path": "rca_code", "roots": []any{"/repos/a", "/repos/b"}}
	registerRCATool(reg, cfg, nil)
	for _, n := range []string{"rca_grep", "rca_glob", "rca_read"} {
		if !has(reg, n) {
			t.Fatalf("expected %s registered", n)
		}
	}
}

func TestRegisterRCATool_Jaeger(t *testing.T) {
	reg := tool.NewRegistry()
	registerRCATool(reg, map[string]any{"func_path": "jaeger_trace", "query_url": "http://j:16686"}, nil)
	if !has(reg, "jaeger_trace") {
		t.Fatal("jaeger_trace should be registered")
	}
}

func TestRegisterRCATool_ESFound(t *testing.T) {
	reg := tool.NewRegistry()
	// 该 agent 另绑定了一个 id=es-logs 的 datasource 工具
	esDS, _ := structpb.NewStruct(map[string]any{
		"datasource": map[string]any{"id": "es-logs", "type": "elasticsearch", "dsn": "http://localhost:9200"},
	})
	agentTools := []*biz.ToolMeta{{Name: "es-logs", Type: biz.ToolTypeDatasource, Config: esDS}}
	cfg := map[string]any{"func_path": "es_log_query", "datasource_id": "es-logs", "default_index": "app-*", "trace_id_field": "trace_id"}
	registerRCATool(reg, cfg, agentTools)
	if !has(reg, "es_log_query") {
		t.Fatal("es_log_query should be registered when datasource found")
	}
}

func TestRegisterRCATool_ESNotFound_Skips(t *testing.T) {
	reg := tool.NewRegistry()
	cfg := map[string]any{"func_path": "es_log_query", "datasource_id": "missing"}
	registerRCATool(reg, cfg, nil) // 无匹配 datasource → 跳过,不 panic
	if has(reg, "es_log_query") {
		t.Fatal("es_log_query should be skipped when datasource missing")
	}
}

func TestRegisterRCATool_UnknownFuncPath(t *testing.T) {
	reg := tool.NewRegistry()
	registerRCATool(reg, map[string]any{"func_path": "nope"}, nil)
	// 不 panic;无 RCA 工具注册
	if has(reg, "rca_grep") || has(reg, "jaeger_trace") || has(reg, "es_log_query") {
		t.Fatal("unknown func_path must register nothing")
	}
}
```

> 说明:`biz` 包 import 路径为 `backend/internal/biz`(见 tool.go 里 `pkgErrors "backend/internal/pkg/errors"`,module 名为 `backend`)。ES datasource 注册不 Ping,离线可注册(与 framework 侧同结论)。

- [ ] **Step 2: 运行,确认失败**

Run: `cd /d/workspace/github/sixath/portal && go test ./internal/chat/ -run 'TestRegisterRCATool' -v`
Expected: `undefined: registerRCATool`。

- [ ] **Step 3: 实现**

创建 `portal/internal/chat/rca_builder.go`:

```go
package chat

import (
	"log/slog"

	"backend/internal/biz"

	"github.com/sixath/framework/datasource"
	"github.com/sixath/framework/executor"
	"github.com/sixath/framework/tool"
)

// registerRCATool 按 cfg["func_path"] 构造并注册 RCA 工具。
// agentTools 为该 Agent 绑定的全部工具,供 es_log_query 查找其依赖的 datasource 工具。
// 缺配置/依赖缺失时跳过并记 warn,绝不 panic 或阻断整体构建。
func registerRCATool(reg *tool.Registry, cfg map[string]interface{}, agentTools []*biz.ToolMeta) {
	if reg == nil {
		return
	}
	funcPath, _ := cfg["func_path"].(string)
	switch funcPath {
	case "rca_code":
		roots := stringSliceFromAny(cfg["roots"])
		if len(roots) == 0 {
			slog.Warn("rca: rca_code has no roots, skip")
			return
		}
		_ = tool.RegisterRCACodeTools(reg, roots)
	case "jaeger_trace":
		queryURL, _ := cfg["query_url"].(string)
		if queryURL == "" {
			slog.Warn("rca: jaeger_trace has no query_url, skip")
			return
		}
		_ = tool.RegisterJaegerTool(reg, queryURL)
	case "es_log_query":
		dsID, _ := cfg["datasource_id"].(string)
		if dsID == "" {
			slog.Warn("rca: es_log_query has no datasource_id, skip")
			return
		}
		reader, ok := buildESReaderFromAgentTools(agentTools, dsID)
		if !ok {
			slog.Warn("rca: es_log_query datasource not found among agent tools, skip", "datasource_id", dsID)
			return
		}
		defaultIndex, _ := cfg["default_index"].(string)
		traceIDField, _ := cfg["trace_id_field"].(string)
		_ = tool.RegisterESLogTool(reg, reader, tool.ESLogConfig{
			DatasourceID: dsID,
			DefaultIndex: defaultIndex,
			TraceIDField: traceIDField,
		})
	default:
		slog.Warn("rca: unknown func_path, skip", "func_path", funcPath)
	}
}

// buildESReaderFromAgentTools 在 agentTools 中找到 id==dsID 的 datasource 工具,
// 构建只含该数据源的 ES executor(实现 executor.Reader)。找不到/构建失败返回 ok=false。
func buildESReaderFromAgentTools(agentTools []*biz.ToolMeta, dsID string) (executor.Reader, bool) {
	for _, t := range agentTools {
		if t.Type != biz.ToolTypeDatasource {
			continue
		}
		m := toolConfigToMap(t.Config)
		dsMap := m
		if nested, ok := m["datasource"].(map[string]interface{}); ok {
			dsMap = nested
		}
		dsCfg := datasource.ConfigFromMap(dsMap)
		// 运行时数据源 ID 对齐工具名(与 canonicalDatasourceConfig 一致)。
		id := dsCfg.ID
		if t.Name != "" {
			id = t.Name
		}
		if id != dsID {
			continue
		}
		dsCfg.ID = dsID
		dsReg := datasource.NewRegistry()
		datasource.RegisterElasticsearch(dsReg)
		if _, err := dsReg.Register(dsCfg); err != nil {
			slog.Warn("rca: register es datasource failed", "err", err)
			return nil, false
		}
		return executor.NewESExecutor(dsReg), true
	}
	return nil, false
}

// stringSliceFromAny 把 structpb 解出的 []any / []string 归一化为 []string。
func stringSliceFromAny(v interface{}) []string {
	switch xs := v.(type) {
	case []string:
		return xs
	case []interface{}:
		out := make([]string, 0, len(xs))
		for _, e := range xs {
			if s, ok := e.(string); ok && s != "" {
				out = append(out, s)
			}
		}
		return out
	default:
		return nil
	}
}
```

> 说明:`toolConfigToMap`、`datasource.ConfigFromMap`、`executor.NewESExecutor` 均为现有符号(见 agent_builder.go)。ID 对齐逻辑复刻 `canonicalDatasourceConfig`(agent_builder.go:114)——datasource 工具运行时 ID = 工具名。

- [ ] **Step 4: 运行,确认通过**

Run: `cd /d/workspace/github/sixath/portal && go test ./internal/chat/ -run 'TestRegisterRCATool' -v`
Expected: 5 用例 PASS。

- [ ] **Step 5: Commit**

```
cd /d/workspace/github/sixath/portal
git add internal/chat/rca_builder.go internal/chat/rca_builder_test.go
git commit -m "feat(chat): add RCA tool runtime builder"
```

---

## Task 3: BuildRegistry 分发 RCA

**Files:**
- Modify: `portal/internal/chat/agent_builder.go`
- Test: `portal/internal/chat/rca_builder_test.go`（追加集成用例)

- [ ] **Step 1: 写失败测试**

在 `portal/internal/chat/rca_builder_test.go` 追加:

```go
func TestBuildRegistry_RCADispatch(t *testing.T) {
	jaeger, _ := structpb.NewStruct(map[string]any{"func_path": "jaeger_trace", "query_url": "http://j:16686"})
	tools := []*biz.ToolMeta{
		{Name: "rca-jaeger", Type: biz.ToolTypeRCA, Config: jaeger},
	}
	reg := tool.NewRegistry()
	if _, err := BuildRegistry(tools, reg); err != nil {
		t.Fatalf("BuildRegistry: %v", err)
	}
	if !has(reg, "jaeger_trace") {
		t.Fatal("BuildRegistry should dispatch rca type to registerRCATool")
	}
}
```

- [ ] **Step 2: 运行,确认失败**

Run: `cd /d/workspace/github/sixath/portal && go test ./internal/chat/ -run 'TestBuildRegistry_RCADispatch' -v`
Expected: FAIL — `jaeger_trace` 未注册(rca 类型当前落到 switch default,被忽略)。

- [ ] **Step 3: 实现**

在 `portal/internal/chat/agent_builder.go` 的 `BuildRegistry` 的 `switch t.Type` 中,增加一个 case(放在 `case biz.ToolTypeDatasource:` 之后、`}` 之前):

```go
		case biz.ToolTypeRCA:
			registerRCATool(reg, cfg, tools)
```

`tools` 即 `BuildRegistry` 的入参(该 Agent 绑定的全部工具),供 es_log_query 查 datasource。`cfg` 已在循环顶部由 `toolConfigToMap(t.Config)` 得到。

- [ ] **Step 4: 运行,确认通过**

Run: `cd /d/workspace/github/sixath/portal && go test ./internal/chat/ -run 'TestBuildRegistry_RCADispatch|TestRegisterRCATool' -v`
Expected: 全部 PASS。

- [ ] **Step 5: Commit**

```
cd /d/workspace/github/sixath/portal
git add internal/chat/agent_builder.go internal/chat/rca_builder_test.go
git commit -m "feat(chat): dispatch rca tool type in BuildRegistry"
```

---

## Task 4: portal 全量回归

- [ ] **Step 1:** `cd /d/workspace/github/sixath/portal && go build ./...` → 成功
- [ ] **Step 2:** `go test ./...` → 全绿
- [ ] **Step 3: Commit(如有未提交)**

```
cd /d/workspace/github/sixath/portal
git add -A
git commit -m "test: portal regression for rca tool binding" || echo "nothing to commit"
```
> 注意:此处 `git add -A` 仅在 portal 仓库内,且前序任务已把改动分别提交;若工作区干净则 `|| echo` 跳过。若 portal 工作区存在与本任务无关的既有改动,改为精确 `git add` 本任务文件。执行者先 `git status` 确认。

---

## Task 5: 前端 API 类型

**Files:**
- Modify: `web/src/api/client.ts`

- [ ] **Step 1: 读现状**

先 `grep -n "ToolConfig\|datasource\|type:\s*'builtin'\|'builtin' | 'mcp' | 'datasource'" web/src/api/client.ts` 找到 `ToolConfig` 类型与工具 type 联合的确切定义。

- [ ] **Step 2: 实现**

在 `web/src/api/client.ts`:
- 找到工具 `type` 的联合类型(形如 `'builtin' | 'mcp' | 'datasource'`),加入 `'rca'`。
- 在 `ToolConfig` 接口加入可选字段:
```ts
  rca?: {
    func_path?: 'rca_code' | 'jaeger_trace' | 'es_log_query'
    roots?: string[]
    query_url?: string
    datasource_id?: string
    default_index?: string
    trace_id_field?: string
  }
```
(若 `ToolConfig` 是 `Record<string, unknown>` 之类的宽类型,则只需扩 type 联合;RCA 字段随 config 自由存放。以实际类型定义为准。)

- [ ] **Step 3: 构建校验**

Run: `cd /d/workspace/github/sixath/web && npx tsc --noEmit`(或项目既有类型检查命令)
Expected: 无新增类型错误。

- [ ] **Step 4: Commit**

```
cd /d/workspace/github/sixath/web
git add src/api/client.ts
git commit -m "feat(web): add rca tool type to client types"
```

---

## Task 6: 前端 ToolForm RCA 表单

**Files:**
- Modify: `web/src/pages/ToolForm.tsx`

- [ ] **Step 1: 读现状**

`grep -n "type ===\|setType\|<option\|datasource\|config.datasource\|submitConfig" web/src/pages/ToolForm.tsx` —— 定位 type useState 联合、type 下拉、datasource 条件块、submit 映射、编辑回填五处。RCA 严格照 datasource 模式加。

- [ ] **Step 2: 实现**

1. type useState 联合与 `onChange` 断言加 `'rca'`(共 3 处形如 `'builtin' | 'mcp' | 'datasource'` → 加 `| 'rca'`;含 setType 初始化回填的断言,见 :81 附近)。
2. type 下拉加选项:`<option value="rca">RCA</option>`。
3. 在 datasource 条件块之后加 RCA 条件块:
```tsx
{type === 'rca' && (
  <>
    <div className="field">
      <label>RCA 子工具</label>
      <select
        value={config.rca?.func_path || 'rca_code'}
        onChange={(e) => setConfig((c) => ({ ...c, rca: { ...(c.rca || {}), func_path: e.target.value as any } }))}
      >
        <option value="rca_code">代码检索 (grep/glob/read)</option>
        <option value="jaeger_trace">Jaeger 链路</option>
        <option value="es_log_query">ELK 日志</option>
      </select>
    </div>

    {(config.rca?.func_path || 'rca_code') === 'rca_code' && (
      <div className="field">
        <label>仓库根路径(每行一个绝对路径)</label>
        <textarea
          value={(config.rca?.roots || []).join('\n')}
          onChange={(e) => setConfig((c) => ({ ...c, rca: { ...(c.rca || {}), roots: e.target.value.split('\n').map((s) => s.trim()).filter(Boolean) } }))}
          placeholder={'/abs/path/service-a\n/abs/path/service-b'}
        />
      </div>
    )}

    {config.rca?.func_path === 'jaeger_trace' && (
      <div className="field">
        <label>Jaeger Query URL</label>
        <input
          value={config.rca?.query_url || ''}
          onChange={(e) => setConfig((c) => ({ ...c, rca: { ...(c.rca || {}), query_url: e.target.value } }))}
          placeholder="http://jaeger-host:16686"
        />
      </div>
    )}

    {config.rca?.func_path === 'es_log_query' && (
      <>
        <div className="field">
          <label>ES 数据源工具 ID(需先创建 datasource 工具并绑定给同一 Agent)</label>
          <input
            value={config.rca?.datasource_id || ''}
            onChange={(e) => setConfig((c) => ({ ...c, rca: { ...(c.rca || {}), datasource_id: e.target.value } }))}
            placeholder="es-logs"
          />
        </div>
        <div className="field">
          <label>默认索引</label>
          <input
            value={config.rca?.default_index || ''}
            onChange={(e) => setConfig((c) => ({ ...c, rca: { ...(c.rca || {}), default_index: e.target.value } }))}
            placeholder="app-logs-*"
          />
        </div>
        <div className="field">
          <label>trace_id 字段名</label>
          <input
            value={config.rca?.trace_id_field || ''}
            onChange={(e) => setConfig((c) => ({ ...c, rca: { ...(c.rca || {}), trace_id_field: e.target.value } }))}
            placeholder="trace_id"
          />
        </div>
      </>
    )}
  </>
)}
```
> `className="field"`/`<div>` 结构以文件里 datasource 块的实际写法为准(复制其包裹结构,不要臆造样式类名)。

4. submit 映射:在处理 `type === 'datasource'` 的分支旁加:
```tsx
    } else if (type === 'rca') {
      submitConfig = { rca: config.rca ?? { func_path: 'rca_code' } }
    }
```
5. 编辑回填:加载已有工具时,`type === 'rca'` 分支从 `t.config?.rca` 回填(照 datasource 回填写法)。

- [ ] **Step 3: 构建校验**

Run: `cd /d/workspace/github/sixath/web && npx tsc --noEmit && npm run build`(或项目既有命令)
Expected: 构建成功,无类型错误。

- [ ] **Step 4: Commit**

```
cd /d/workspace/github/sixath/web
git add src/pages/ToolForm.tsx
git commit -m "feat(web): add RCA tool form to ToolForm"
```

---

## Task 7: 端到端手动验证(人工)

- [ ] **Step 1:** 启动 portal 后端 + web 前端(按项目既有方式)。
- [ ] **Step 2:** 新建工具 → 类型选 RCA → 子工具 `jaeger_trace` → 填 query_url → 保存。确认 tool 列表出现该工具。
- [ ] **Step 3:**(可选,验证 es 依赖)先建一个 datasource 类型工具(ES);再建 RCA `es_log_query` 工具填其 id;把两者都绑定给同一 Agent。
- [ ] **Step 4:** Agent 详情 → 绑定 RCA 工具 → 与 Agent 对话,确认模型能看到/调用对应 RCA 工具(如让它列出可用工具)。
- [ ] **Step 5:** 记录验证结果;如有问题回到对应任务修复。

---

## 备注
- framework 侧不改;若发现 framework 的 `RegisterXxx` 需调整,应回到独立的 framework 计划,不在本计划内改。
- proto 不改(type 是 string)。若发现某处按枚举校验 type,报告后再定。
