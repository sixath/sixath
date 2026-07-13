# RCA 工具链接线 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax.

**Goal:** 把已交付的 5 个 RCA 工具接入框架装配层,让 `NewSkillsAwareChatHandlerFromConfig` 构建的 Agent 能实际调用它们;新增 `RCA` 配置节驱动。

**Architecture:** 在 `config.Config` 增加 `RCA` 配置节。在 `templates/skills_handler.go` 的 per-request 装配处(已有 `reg := tool.NewRegistry()`)注册三组 RCA 工具:`RegisterRCACodeTools`(多仓库根)、`RegisterJaegerTool`(无鉴权 URL)、`RegisterESLogTool`(复用一个专为日志构建的 ES datasource + `executor.NewBundle(dsReg).Reader`)。全部按配置存在性条件注册——未配置则跳过,不报错。

**Tech Stack:** Go;复用现有 `datasource.NewRegistry`/`datasource.RegisterElasticsearch`/`executor.NewBundle`(见 `templates/dataquery.go:381-398`);env-only 密钥沿用 datasource 现有机制。

**依赖现状(current main):** 数据查询工具已迁到 `tool/data`;`es_log_query` 依赖的 `executor`/`datasource` 为独立包,不受影响。装配入口 `templates/skills_handler.go:104` 每请求 `tool.NewRegistry()`。ES datasource 构建样板见 `templates/dataquery.go:381-398`。

**Spec:** `docs/superpowers/specs/2026-07-07-rca-toolchain-design.md`(§4 配置、§2 复用 datasource)

---

## File Structure

| 文件 | 改动 |
|------|------|
| `config/config.go` | 新增 `RCAConfig` 及其子结构;在顶层 `Config` 加 `RCA RCAConfig` 字段 |
| `config/config_test.go` | 新增:YAML 解析出 RCA 配置的测试 |
| `templates/rca_wiring.go`（新建） | `registerRCATools(reg *tool.Registry, cfg config.Config) error`:根据 cfg 条件注册三组工具;内部构建日志用 ES datasource registry + reader |
| `templates/rca_wiring_test.go`（新建） | 验证:配置齐全→三工具注册;各配置缺失→对应工具跳过、不报错;ES datasource 构建 |
| `templates/skills_handler.go` | 在 per-request 装配处调用 `registerRCATools(reg, cfg)` |

> 接线逻辑单独放 `templates/rca_wiring.go`,避免继续膨胀 `skills_handler.go`,且便于独立测试。

---

## Task 1: RCA 配置结构

**Files:**
- Modify: `config/config.go`
- Test: `config/config_test.go`

- [ ] **Step 1: 写失败测试**

在 `config/config_test.go` 追加(若文件不存在则创建,`package config`):

```go
func TestRCAConfig_YAML(t *testing.T) {
	yml := []byte(`
model: openai/gpt-4o
rca:
  jaeger:
    query_url: http://jaeger:16686
  es:
    datasource_id: es-logs
    default_index: app-logs-*
    trace_id_field: trace_id
  repos:
    roots:
      - /repos/service-a
      - /repos/service-b
`)
	var cfg Config
	if err := yaml.Unmarshal(yml, &cfg); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if cfg.RCA.Jaeger.QueryURL != "http://jaeger:16686" {
		t.Fatalf("jaeger url = %q", cfg.RCA.Jaeger.QueryURL)
	}
	if cfg.RCA.ES.DatasourceID != "es-logs" || cfg.RCA.ES.DefaultIndex != "app-logs-*" || cfg.RCA.ES.TraceIDField != "trace_id" {
		t.Fatalf("es cfg = %+v", cfg.RCA.ES)
	}
	if len(cfg.RCA.Repos.Roots) != 2 || cfg.RCA.Repos.Roots[0] != "/repos/service-a" {
		t.Fatalf("repos = %+v", cfg.RCA.Repos)
	}
}
```

如果 `config_test.go` 已存在且已 import yaml,复用其 import;否则确保测试文件顶部有(仓库确认使用 `go.yaml.in/yaml/v2`,与 config.go 一致):
```go
import (
	"testing"

	yaml "go.yaml.in/yaml/v2"
)
```
注意:v2 的 map 解码把嵌套映射解成 `map[interface{}]interface{}`,但本测试是解到强类型 `Config` 结构体,不受影响。

- [ ] **Step 2: 运行,确认失败**

Run: `cd /d/workspace/github/sixath/framework && go test ./config/ -run TestRCAConfig_YAML -v`
Expected: 编译失败 `cfg.RCA undefined`。

- [ ] **Step 3: 实现**

在 `config/config.go` 顶层 `Config` 结构体内(与 `HyperTool` 等字段并列)加入:
```go
	// RCA 可选;线上根因分析工具链(Jaeger + ELK + 多仓库代码检索)。
	RCA RCAConfig `json:"rca" yaml:"rca"`
```
并在文件内合适位置(与其它子配置结构一起)加入:
```go
// RCAConfig 配置线上问题排查(RCA)工具链。各子节缺省时对应工具不注册。
type RCAConfig struct {
	Jaeger RCAJaegerConfig `json:"jaeger" yaml:"jaeger"`
	ES     RCAESConfig     `json:"es" yaml:"es"`
	Repos  RCAReposConfig  `json:"repos" yaml:"repos"`
}

// RCAJaegerConfig Jaeger Query 无鉴权访问配置。
type RCAJaegerConfig struct {
	QueryURL string `json:"query_url" yaml:"query_url"`
}

// RCAESConfig ELK 日志查询配置;复用已注册的 ES datasource(密钥走 datasource 的 env)。
type RCAESConfig struct {
	DatasourceID string `json:"datasource_id" yaml:"datasource_id"`
	DefaultIndex string `json:"default_index" yaml:"default_index"`
	TraceIDField string `json:"trace_id_field" yaml:"trace_id_field"`
}

// RCAReposConfig 多仓库代码检索的仓库根白名单。
type RCAReposConfig struct {
	Roots []string `json:"roots" yaml:"roots"`
}
```

- [ ] **Step 4: 运行,确认通过**

Run: `cd /d/workspace/github/sixath/framework && go test ./config/ -run TestRCAConfig_YAML -v`
Expected: PASS。

- [ ] **Step 5: Commit**

```
cd /d/workspace/github/sixath/framework
git add config/config.go config/config_test.go
git commit -m "feat(config): add RCA toolchain config section"
```

---

## Task 2: RCA 接线函数

**Files:**
- Create: `templates/rca_wiring.go`
- Create: `templates/rca_wiring_test.go`

**关键设计:** `es_log_query` 需要一个 `executor.Reader` 指向配置的 ES datasource。RCA 的 ES datasource 独立于数据查询的 `DataSources`——本函数按 `cfg.RCA.ES.DatasourceID` 从 `cfg.DataSources` 里找到同 ID 的 datasource 配置来构建 registry+reader。若 `DataSources` 中找不到该 ID,则跳过 es_log_query(记 log,不报错)。

- [ ] **Step 1: 写失败测试**

创建 `templates/rca_wiring_test.go`:

```go
package templates

import (
	"testing"

	"github.com/sixath/framework/config"
	"github.com/sixath/framework/datasource"
	"github.com/sixath/framework/tool"
)

func hasTool(reg *tool.Registry, name string) bool {
	_, ok := reg.Get(name)
	return ok
}

func TestRegisterRCATools_AllConfigured(t *testing.T) {
	cfg := config.Config{
		DataSources: []datasource.Config{
			{ID: "es-logs", Type: "elasticsearch", DSN: "http://localhost:9200"},
		},
		RCA: config.RCAConfig{
			Jaeger: config.RCAJaegerConfig{QueryURL: "http://jaeger:16686"},
			ES:     config.RCAESConfig{DatasourceID: "es-logs", DefaultIndex: "app-logs-*", TraceIDField: "trace_id"},
			Repos:  config.RCAReposConfig{Roots: []string{"/repos/a", "/repos/b"}},
		},
	}
	reg := tool.NewRegistry()
	if err := registerRCATools(reg, cfg); err != nil {
		t.Fatalf("registerRCATools: %v", err)
	}
	for _, n := range []string{"rca_grep", "rca_glob", "rca_read", "jaeger_trace", "es_log_query"} {
		if !hasTool(reg, n) {
			t.Fatalf("expected %s registered", n)
		}
	}
}

func TestRegisterRCATools_PartialSkips(t *testing.T) {
	// 只配 repos;jaeger/es 缺省 -> 仅代码工具注册,其余跳过且不报错
	cfg := config.Config{
		RCA: config.RCAConfig{
			Repos: config.RCAReposConfig{Roots: []string{"/repos/a"}},
		},
	}
	reg := tool.NewRegistry()
	if err := registerRCATools(reg, cfg); err != nil {
		t.Fatalf("registerRCATools: %v", err)
	}
	if !hasTool(reg, "rca_grep") {
		t.Fatal("rca_grep should be registered when roots set")
	}
	if hasTool(reg, "jaeger_trace") {
		t.Fatal("jaeger_trace should be skipped when query_url empty")
	}
	if hasTool(reg, "es_log_query") {
		t.Fatal("es_log_query should be skipped when datasource_id empty")
	}
}

func TestRegisterRCATools_Empty(t *testing.T) {
	reg := tool.NewRegistry()
	if err := registerRCATools(reg, config.Config{}); err != nil {
		t.Fatalf("empty config must not error: %v", err)
	}
	if hasTool(reg, "rca_grep") || hasTool(reg, "jaeger_trace") || hasTool(reg, "es_log_query") {
		t.Fatal("no RCA tools should register with empty config")
	}
}
```

> 注意:`tool.NewRegistry()` 默认已注册 `http_request`;这些测试只查 RCA 工具名,互不影响。`es_log_query` 注册只需构建 datasource registry(不实际连 ES),`datasource.Register` 对 elasticsearch 类型只建 client 不 Ping,故无需真实 ES。若 `datasource.Register` 会主动连接导致测试失败,改为在测试中用一个已知可离线构建的 DSN;先按上例尝试,若失败按实际调整并在报告中说明。

- [ ] **Step 2: 运行,确认失败**

Run: `cd /d/workspace/github/sixath/framework && go test ./templates/ -run TestRegisterRCATools -v`
Expected: 编译失败 `undefined: registerRCATools`。

- [ ] **Step 3: 实现**

创建 `templates/rca_wiring.go`:

```go
package templates

import (
	"log/slog"

	"github.com/sixath/framework/config"
	"github.com/sixath/framework/datasource"
	"github.com/sixath/framework/executor"
	"github.com/sixath/framework/tool"
)

// registerRCATools 按配置条件注册 RCA 工具链的三组工具。
// 各子节缺省时对应工具跳过(记 log),不返回错误——缺配置不应阻断整个 handler。
func registerRCATools(reg *tool.Registry, cfg config.Config) error {
	if reg == nil {
		return nil
	}

	// 1) 多仓库代码检索:有 roots 才注册。
	if len(cfg.RCA.Repos.Roots) > 0 {
		if err := tool.RegisterRCACodeTools(reg, cfg.RCA.Repos.Roots); err != nil {
			return err
		}
	} else {
		slog.Info("rca: repos.roots empty, skip rca_grep/rca_glob/rca_read")
	}

	// 2) Jaeger:有 query_url 才注册。
	if cfg.RCA.Jaeger.QueryURL != "" {
		if err := tool.RegisterJaegerTool(reg, cfg.RCA.Jaeger.QueryURL); err != nil {
			return err
		}
	} else {
		slog.Info("rca: jaeger.query_url empty, skip jaeger_trace")
	}

	// 3) ES 日志:有 datasource_id 且能在 DataSources 中找到该 ES 数据源才注册。
	if cfg.RCA.ES.DatasourceID != "" {
		reader, ok := buildRCAESReader(cfg)
		if !ok {
			slog.Warn("rca: es datasource not found in data_sources, skip es_log_query",
				"datasource_id", cfg.RCA.ES.DatasourceID)
		} else {
			if err := tool.RegisterESLogTool(reg, reader, tool.ESLogConfig{
				DatasourceID: cfg.RCA.ES.DatasourceID,
				DefaultIndex: cfg.RCA.ES.DefaultIndex,
				TraceIDField: cfg.RCA.ES.TraceIDField,
			}); err != nil {
				return err
			}
		}
	} else {
		slog.Info("rca: es.datasource_id empty, skip es_log_query")
	}

	return nil
}

// buildRCAESReader 依据 cfg.RCA.ES.DatasourceID 从 cfg.DataSources 找到对应 ES 数据源,
// 构建一个只含该数据源的 registry 与只读 Reader。找不到则返回 ok=false。
func buildRCAESReader(cfg config.Config) (executor.Reader, bool) {
	var dsCfg *datasource.Config
	for i := range cfg.DataSources {
		if cfg.DataSources[i].ID == cfg.RCA.ES.DatasourceID {
			dsCfg = &cfg.DataSources[i]
			break
		}
	}
	if dsCfg == nil {
		return nil, false
	}
	dsReg := datasource.NewRegistry()
	datasource.RegisterElasticsearch(dsReg)
	if _, err := dsReg.Register(*dsCfg); err != nil {
		slog.Warn("rca: register es datasource failed", "err", err)
		return nil, false
	}
	bundle := executor.NewBundle(dsReg)
	return bundle.Reader, true
}
```

> 说明:`datasource.Config` 字段(ID/Type/DSN/Host/Port/User/Password)见 `datasource` 包;`datasource.RegisterElasticsearch` / `datasource.NewRegistry` / `executor.NewBundle` 用法对齐 `templates/dataquery.go:381-398`。若 `dsReg.Register` 签名/返回值不同,以实际为准并在报告中说明。

- [ ] **Step 4: 运行,确认通过**

Run: `cd /d/workspace/github/sixath/framework && go test ./templates/ -run TestRegisterRCATools -v`
Expected: PASS(3 用例)。若某用例因 datasource 真实连接失败,按 Step 1 注记调整并报告。

- [ ] **Step 5: Commit**

```
cd /d/workspace/github/sixath/framework
git add templates/rca_wiring.go templates/rca_wiring_test.go
git commit -m "feat(templates): add RCA tools wiring helper"
```

---

## Task 3: 在 handler 中调用接线

**Files:**
- Modify: `templates/skills_handler.go`

- [ ] **Step 1: 写失败测试**

复用 Task 2 的接线测试已覆盖注册逻辑;此处只需保证 handler 装配处调用了 `registerRCATools`。加一个针对装配的轻量测试。先确认 `NewSkillsAwareChatHandlerFromConfig` 是否可在无模型密钥下构造(它调用 `model.NewFromIdentifier`)。若不易单测,则用一个直接断言"handler 装配代码路径调用了 registerRCATools"的替代:抽出的 registerRCATools 已被 Task 2 充分测试,本任务改为**编译期接线 + 全量回归**验证,不新增脆弱的 handler 集成测试。

因此本任务采用:**不新增测试**,靠 Task 2 的单测 + 全量 `go test` 保证。直接进入实现。

- [ ] **Step 2: 实现**

在 `templates/skills_handler.go` 的 per-request 装配块中,在 `reg := tool.NewRegistry()`(约 104 行)与 HyperTool 注册之间,加入一行:

```go
		_ = registerRCATools(reg, cfg)
```

放置位置:在 `_ = tool.RegisterHyperTool(reg, ...)` 调用之前即可。用 `_ =` 忽略错误以与该处其它 `_ =` 注册风格一致(缺配置本就不报错;真错误已在 registerRCATools 内 log)。

- [ ] **Step 3: 构建 + 全量回归**

Run: `cd /d/workspace/github/sixath/framework && go build ./... && go test ./...`
Expected: 构建成功;测试全绿。

- [ ] **Step 4: Commit**

```
cd /d/workspace/github/sixath/framework
git add templates/skills_handler.go
git commit -m "feat(templates): register RCA tools in skills-aware handler"
```

---

## Task 4: 全量验收

- [ ] **Step 1:** `cd /d/workspace/github/sixath/framework && go build ./...` → 成功
- [ ] **Step 2:** `go test ./...` → 全绿
- [ ] **Step 3:** 确认 5 个 RCA 工具在配置齐全时可被 handler 注册(由 Task 2 单测保证)

---

## 备注:验证接线生效(超出自动化范围,人工可选)
配好 `config.yaml` 的 `rca` 节 + 一个 id 匹配的 ES `data_sources` 项后,启动 `./sath serve` 并让 Agent 列出工具,应能看到 `rca_grep/rca_glob/rca_read/jaeger_trace/es_log_query`。
