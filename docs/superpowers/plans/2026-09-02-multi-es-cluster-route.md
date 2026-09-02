# Multi-ES Cluster Routing Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** An Agent can bind multiple elasticsearch datasources; `es_log_query` always requires `cluster=<tool name>` and routes that call to the matching DSN, including mid-task switches.

**Architecture:** Keep one runtime tool named `es_log_query`. Extend `ESLogConfig` with `Clusters []ESLogCluster` (single-cluster shorthand: `DatasourceID` + `DefaultIndex` still works). Portal `BuildRegistry` collects elasticsearch datasources plus transitional RCA ES rows, registers ES connections on a dedicated registry, then registers `es_log_query` once. Elasticsearch datasources count as `FamilyRCA` so Turn Tool Surface still exposes the tool without a bound RCA ELK entry.

**Tech Stack:** Go (framework + portal), protobuf `make api`, React ToolForm/AgentDetail, existing `datasource`/`executor` ES stack.

**Spec:** [docs/superpowers/specs/2026-09-02-multi-es-cluster-route-design.md](../specs/2026-09-02-multi-es-cluster-route-design.md)

---

## File map

| File | Responsibility |
|------|----------------|
| Modify `framework/tool/es_log_tool.go` | Multi-cluster resolve, required `cluster`, stamp `cluster` on results |
| Modify `framework/tool/es_log_tool_test.go` (+ `evalgolden_test.go`) | Route tests; all Execute calls pass `cluster` |
| Modify `framework/tool/es_log_mapping.go` | Mapping uses this call's cluster id, not a baked-in DatasourceID |
| Modify `framework/tool/query_spill.go` | `QuerySpillStub` / `SpillView` carry `cluster` |
| Modify `framework/agent/truncated_page_gate.go` (+ test) | Nudge names cluster; last successful call's `(cluster,index)` |
| Modify `framework/templates/rca_wiring.go` (+ test) | YAML single cluster still requires `cluster` param at execute |
| Modify `portal/api/tool/v1/tool.proto` + `make api` | `DatasourceConfig.default_index/trace_id_field/purpose` |
| Modify `portal/internal/service/tool.go` (+ `tool_rca_test.go` or new test) | Encode/decode the three fields |
| Modify `portal/internal/biz/rca_es_validate.go` (+ test, `tool.go`) | ES datasource save rules; **Create** rejects RCA `es_log_query`; Update still allows |
| Create `portal/internal/chat/es_log_clusters.go` (+ test) | Collect/merge cluster table; register once |
| Modify `portal/internal/chat/rca_builder.go` | `es_log_query` case no longer `RegisterESLogTool` |
| Modify `portal/internal/chat/agent_builder.go` | After tool loop, `registerESLogFromAgentTools` |
| Modify `portal/internal/chat/tool_families.go` + `filterToolsForSurface` (+ tests) | ES datasource → FamilyRCA, not FamilyData |
| Modify `portal/internal/chat/datasource_prompt.go` (+ test) | List ES clusters + `cluster=` |
| Modify `web/src/api/client.ts` | Datasource fields + camelCase normalize |
| Modify `web/src/utils/toolExportFormat.ts` (+ `web/tests/toolImportExport.test.ts`) | Same normalize |
| Modify `web/src/pages/ToolForm.tsx` | ES fields; hide new RCA ELK; edit existing still shows |
| Modify `web/src/pages/AgentDetail.tsx` | Binding table: default index + purpose |
| Modify `framework/skills_examples/skills/rca-investigation/SKILL.md` | `cluster=` + switch note |
| Modify `portal/docs/rca-es-log-query.md` | Positive path is elasticsearch datasource |

**Out of scope:** Auto-guess cluster by index; ES in data trio; YAML `rca.es` as a list; deleting `es_log_query` from `ValidRCAFuncPath`; DB migration script.

---

### Task 1: `es_log_query` required `cluster` + multi-cluster route

**Files:**
- Modify: `framework/tool/es_log_tool.go`
- Modify: `framework/tool/es_log_tool_test.go`
- Modify: `framework/tool/evalgolden_test.go` (every `Execute` must pass `cluster` matching `DatasourceID`)

- [ ] **Step 1: Write failing tests** (append to `es_log_tool_test.go`)

```go
func TestESLogQuery_RequiresCluster(t *testing.T) {
	fr := &fakeReader{result: &executor.QueryResult{Columns: []string{"m"}, Rows: [][]any{{"x"}}}}
	reg := &Registry{tools: map[string]Tool{}, mcpServerIDs: map[string]struct{}{}}
	_ = RegisterESLogTool(reg, fr, ESLogConfig{DatasourceID: "zj-elk", DefaultIndex: "app-*", TraceIDField: "trace_id"})
	tl, _ := reg.Get("es_log_query")
	out, err := tl.Execute(context.Background(), map[string]any{"query": "a:b"})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	m := out.(map[string]any)
	if m["ok"] != false {
		t.Fatal("missing cluster must fail")
	}
	msg, _ := m["error"].(string)
	if !strings.Contains(msg, "zj-elk") || !strings.Contains(msg, "cluster") {
		t.Fatalf("error should list cluster names, got %q", msg)
	}
}

func TestESLogQuery_RoutesByCluster(t *testing.T) {
	fr := &fakeReader{result: &executor.QueryResult{Columns: []string{"m"}, Rows: [][]any{{"x"}}}}
	reg := &Registry{tools: map[string]Tool{}, mcpServerIDs: map[string]struct{}{}}
	err := RegisterESLogTool(reg, fr, ESLogConfig{Clusters: []ESLogCluster{
		{ID: "zj-elk", DefaultIndex: "app-*", Purpose: "应用日志"},
		{ID: "zj-elk_flow", DefaultIndex: "1_game_flow_all-*", Purpose: "流水"},
	}})
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	tl, _ := reg.Get("es_log_query")
	_, err = tl.Execute(context.Background(), map[string]any{"cluster": "zj-elk_flow", "query": "a:b"})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if fr.gotDatasource != "zj-elk_flow" || fr.gotIndex != "1_game_flow_all-*" {
		t.Fatalf("got ds=%q index=%q", fr.gotDatasource, fr.gotIndex)
	}
}

func TestESLogQuery_UnknownCluster(t *testing.T) {
	fr := &fakeReader{result: &executor.QueryResult{}}
	reg := &Registry{tools: map[string]Tool{}, mcpServerIDs: map[string]struct{}{}}
	_ = RegisterESLogTool(reg, fr, ESLogConfig{Clusters: []ESLogCluster{
		{ID: "zj-elk", DefaultIndex: "app-*", Purpose: "应用"},
	}})
	tl, _ := reg.Get("es_log_query")
	out, _ := tl.Execute(context.Background(), map[string]any{"cluster": "zj-elk_flow", "query": "a:b"})
	m := out.(map[string]any)
	if m["ok"] != false {
		t.Fatal("unknown cluster must fail")
	}
	if fr.gotDatasource != "" {
		t.Fatal("must not query any cluster")
	}
}

func TestESLogQuery_MissingDefaultIndexRequiresIndexParam(t *testing.T) {
	fr := &fakeReader{result: &executor.QueryResult{}}
	reg := &Registry{tools: map[string]Tool{}, mcpServerIDs: map[string]struct{}{}}
	_ = RegisterESLogTool(reg, fr, ESLogConfig{Clusters: []ESLogCluster{
		{ID: "zj-elk", DefaultIndex: "", Purpose: "应用"},
	}})
	tl, _ := reg.Get("es_log_query")
	out, _ := tl.Execute(context.Background(), map[string]any{"cluster": "zj-elk", "query": "a:b"})
	m := out.(map[string]any)
	if m["ok"] != false {
		t.Fatal("empty default_index without index param must fail")
	}
	if fr.gotDatasource != "" {
		t.Fatal("must not Query with empty index")
	}
}
```

Also add `"cluster": "<DatasourceID>"` to **every existing** `tl.Execute` / `Execute(` in `es_log_tool_test.go` and `evalgolden_test.go` (IDs used today: `es-logs` or `es`). Without this, old tests fail once cluster is required.

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd framework; go test ./tool -run "TestESLogQuery_RequiresCluster|TestESLogQuery_RoutesByCluster|TestESLogQuery_UnknownCluster|TestESLogQuery_MissingDefaultIndexRequiresIndexParam" -count=1`

Expected: FAIL (`ESLogCluster` undefined and/or missing-cluster still succeeds).

- [ ] **Step 3: Implement**

In `es_log_tool.go`:

```go
type ESLogCluster struct {
	ID            string
	DefaultIndex  string
	TraceIDField  string
	Purpose       string
}

type ESLogConfig struct {
	DatasourceID string
	DefaultIndex string
	TraceIDField string
	FieldMapper  ESFieldMapper
	Clusters     []ESLogCluster
}

func (cfg ESLogConfig) resolvedClusters() []ESLogCluster {
	if len(cfg.Clusters) > 0 {
		out := append([]ESLogCluster(nil), cfg.Clusters...)
		for i := range out {
			if out[i].TraceIDField == "" {
				out[i].TraceIDField = "trace_id"
			}
		}
		return out
	}
	if strings.TrimSpace(cfg.DatasourceID) == "" {
		return nil
	}
	tf := cfg.TraceIDField
	if tf == "" {
		tf = "trace_id"
	}
	return []ESLogCluster{{ID: cfg.DatasourceID, DefaultIndex: cfg.DefaultIndex, TraceIDField: tf}}
}
```

Register: reject empty `resolvedClusters()`. Parameters add required `cluster` with `enum` of IDs. Description appends one line per cluster: `` `{id}` — {purpose}; default index `{default_index}` ``.

Execute: replace **every** use of `cfg.DatasourceID` / `cfg.DefaultIndex` / `cfg.TraceIDField` in the closure (including the empty-hit **rewrite** second `Query`) with the looked-up cluster. Do not leave the first Query patched and the retry still on `cfg.DatasourceID`.

1. Resolve `cluster` (trim). Empty or unknown → `ErrorPermanent` listing `id` / `default_index` / `purpose`. **Do not** fall back to the first cluster.
2. Index: if params `index` non-empty use it; else cluster `DefaultIndex`; if still empty → `ErrorPermanent` (do not `Query`). Trace field: cluster `TraceIDField` or `trace_id`.
3. `reader.Query(..., clusterID, ...)` for both the first search and the mapping rewrite retry.
4. Put `"cluster"` on every payload (success, empty, error).
5. Mapping: if `cfg.FieldMapper != nil` use it (unit tests); else `mapperFromReader(reader, clusterID)` for this call only.

Helper: `lookupCluster(clusters, id) (ESLogCluster, bool)` exact match, no prefix.

- [ ] **Step 4: Run tests**

Run: `cd framework; go test ./tool -count=1`

Expected: PASS (update every Execute in this package to pass `cluster`).

- [ ] **Step 5: Commit**

```bash
git add framework/tool/es_log_tool.go framework/tool/es_log_tool_test.go framework/tool/evalgolden_test.go
git commit -m "feat(tool): require cluster on es_log_query and route by name"
```

---

### Task 2: Spill + truncated-page gate carry cluster

**Files:**
- Modify: `framework/tool/query_spill.go`
- Modify: `framework/agent/truncated_page_gate.go`
- Modify: `framework/agent/truncated_page_gate_test.go`

- [ ] **Step 1: Write failing tests**

In `truncated_page_gate_test.go`:

```go
func TestEvaluateTruncatedPageGate_PromptNamesCluster(t *testing.T) {
	q := "请解析全部日志并统计分布"
	tr := &RunTrace{ToolCalls: []ToolCallRecord{{
		ToolName: "es_log_query",
		Result: map[string]any{
			"cluster":        "zj-elk",
			"truncated":      true,
			"has_more":       true,
			"continue_from":  50,
			"queried_index":  "app-*",
		},
	}}}
	got := EvaluateTruncatedPageGate(tr, q)
	if got.Allow {
		t.Fatal("truncated last page must inject")
	}
	if !strings.Contains(got.Prompt, "zj-elk") {
		t.Fatalf("prompt must name cluster, got %q", got.Prompt)
	}
}

func TestEvaluateTruncatedPageGate_LaterCompleteClusterAllowsFinish(t *testing.T) {
	q := "请解析全部日志"
	tr := &RunTrace{ToolCalls: []ToolCallRecord{
		{ToolName: "es_log_query", Result: map[string]any{"cluster": "zj-elk", "truncated": true, "has_more": true, "continue_from": 50}},
		{ToolName: "es_log_query", Result: map[string]any{"cluster": "zj-elk_flow", "truncated": false}},
	}}
	got := EvaluateTruncatedPageGate(tr, q)
	if !got.Allow {
		t.Fatal("latest successful es_log_query is complete; allow finish")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd framework; go test ./agent -run "TestEvaluateTruncatedPageGate_PromptNamesCluster|TestEvaluateTruncatedPageGate_LaterCompleteClusterAllowsFinish" -count=1`

Expected: FAIL (prompt has no cluster name).

- [ ] **Step 3: Implement**

- Add `Cluster string` to `QuerySpillStub` and `SpillView`.
- `stubFromPayload` / `spillViewFromMap` / `spillViewFromStub` copy `cluster`.
- Gate prompt: include `view.Cluster` (and `view.QueriedIndex` if useful). Keep using **last** successful `es_log_query` only (second test already passes if last call is complete).

- [ ] **Step 4: Run tests**

Run: `cd framework; go test ./agent ./tool -count=1`

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add framework/tool/query_spill.go framework/agent/truncated_page_gate.go framework/agent/truncated_page_gate_test.go
git commit -m "feat(agent): name ES cluster in truncated-page nudge"
```

---

### Task 3: YAML `rca.es` still registers; execute needs cluster

**Files:**
- Modify: `framework/templates/rca_wiring.go` only if `RegisterESLogTool` call must pass `Clusters` with ID `datasource_id` or `"rca-es"` (shorthand `DatasourceID` already works from Task 1).
- Modify: `framework/templates/rca_wiring_test.go` — if any test Executes the tool, add `cluster`. Registration-only tests should still pass.

- [ ] **Step 1: Run existing wiring tests**

Run: `cd framework; go test ./templates -run TestRegisterRCATools -count=1`

Expected: PASS if Task 1 kept `DatasourceID` shorthand. If FAIL, set `Clusters: []ESLogCluster{{ID: dsID or "rca-es", ...}}` explicitly. Inline endpoint path must use ID `"rca-es"` (YAML remains single-cluster; Portal multi-inline uses tool names in Task 6).

- [ ] **Step 2: Commit only if you changed files**

```bash
git add framework/templates/rca_wiring.go framework/templates/rca_wiring_test.go
git commit -m "fix(templates): keep YAML es_log_query cluster id"
```

---

### Task 4: Proto + codec for datasource ES metadata

**Files:**
- Modify: `portal/api/tool/v1/tool.proto`
- Regenerate: from `portal/`, `make api`
- Modify: `portal/internal/service/tool.go` (read + write the three fields; encode even when only these are set alongside type)
- Test: `portal/internal/service/tool_rca_test.go` or new `tool_datasource_test.go`

- [ ] **Step 1: Write failing round-trip test** (may not compile until proto exists — write test after proto gen if needed)

Proto add to `DatasourceConfig`:

```protobuf
  string default_index = 10;   // elasticsearch: default index/pattern
  string trace_id_field = 11;  // elasticsearch: trace field, default trace_id
  string purpose = 12;         // elasticsearch: human/model purpose
```

- [ ] **Step 2: `cd portal; make api`**

Expected: `tool.pb.go` has getters. On Windows use the same Makefile (`Git_Bash` find). If `make` is missing, run the `protoc` line from `portal/Makefile` `api` target.

- [ ] **Step 3: Encode/decode in `tool.go`**

Read: `ds["default_index"]` / `defaultIndex`, same for `trace_id_field`, `purpose`.

Write: always set the three keys when `c.Datasource != nil`.

Widen the “should emit datasource struct” condition so type+purpose+index without dsn still persist (e.g. also true when `DefaultIndex` or `Purpose` non-empty).

- [ ] **Step 4: Test**

Run: `cd portal; go test ./internal/service -count=1`

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add portal/api/tool/v1/tool.proto portal/api/tool/v1/*.pb.go portal/internal/service/tool.go portal/internal/service/*_test.go
git commit -m "feat(portal): persist ES default_index, purpose on datasource tools"
```

---

### Task 5: Save-time validation

**Files:**
- Modify: `portal/internal/biz/rca_es_validate.go`
- Modify: `portal/internal/biz/rca_es_validate_test.go`
- Modify: `portal/internal/biz/tool.go` (`Create` / `Update`)

- [ ] **Step 1: Write failing tests**

```go
func TestRejectCreateRCAESLogQuery(t *testing.T) {
	err := ValidateCreateRCAESLog(ToolTypeRCA, mustStruct(t, map[string]any{
		"rca": map[string]any{"func_path": "es_log_query", "endpoint": "http://es:9200"},
	}))
	if err == nil {
		t.Fatal("create RCA es_log_query must fail")
	}
}

func TestValidateElasticsearchDatasource(t *testing.T) {
	err := ValidateElasticsearchDatasource(ToolTypeDatasource, mustStruct(t, map[string]any{
		"datasource": map[string]any{"type": "elasticsearch", "dsn": "http://es:9200"},
	}))
	if err == nil {
		t.Fatal("missing default_index and purpose must fail")
	}
	err = ValidateElasticsearchDatasource(ToolTypeDatasource, mustStruct(t, map[string]any{
		"datasource": map[string]any{
			"type": "elasticsearch", "dsn": "http://es:9200",
			"default_index": "app-*", "purpose": "应用日志",
		},
	}))
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
}
```

`mustStruct` = `structpb.NewStruct`. Keep `ValidRCAFuncPath("es_log_query") == true`.

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd portal; go test ./internal/biz -run "TestRejectCreateRCAESLogQuery|TestValidateElasticsearchDatasource" -count=1`

Expected: FAIL (funcs missing).

- [ ] **Step 3: Implement**

- `Create`: if RCA + `func_path=es_log_query` → new sentinel `ErrRCAESLogUseDatasource` (message: create an elasticsearch datasource instead). **Skip** `ValidateRCAESLogConfig` on Create for that path. Call these validators from `ToolUsecase.Create` / `Update`, not only from unit tests of the helper.
- `Update`: still `ValidateRCAESLogConfig` (exactly one of endpoint / datasource_id).
- Datasource Create/Update: if type is elasticsearch/es, require non-empty `default_index` and `purpose`. MySQL unchanged.

- [ ] **Step 4: Run tests**

Run: `cd portal; go test ./internal/biz -count=1`

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add portal/internal/biz/rca_es_validate.go portal/internal/biz/rca_es_validate_test.go portal/internal/biz/tool.go
git commit -m "feat(portal): require ES purpose/index; block new RCA es_log_query"
```

---

### Task 6: Portal cluster table + one `RegisterESLogTool`

**Files:**
- Create: `portal/internal/chat/es_log_clusters.go`
- Create: `portal/internal/chat/es_log_clusters_test.go`
- Modify: `portal/internal/chat/rca_builder.go` — `case "es_log_query":` return without registering
- Modify: `portal/internal/chat/agent_builder.go` — after the tools loop (and after datasource trio), call `registerESLogFromAgentTools(reg, tools)`
- Modify: `portal/internal/chat/rca_builder_test.go` — tests that expected `registerRCATool` to register `es_log_query` must switch to `BuildRegistry` or `registerESLogFromAgentTools`
- Modify: `portal/internal/chat/rca_binding_acceptance_test.go` if it uses `registerRCATool` for ES

- [ ] **Step 1: Write failing tests** in `es_log_clusters_test.go`

Cover:

1. Two elasticsearch datasources, no RCA → `es_log_query` registered; Execute `cluster=b` hits reader id `b`.
2. Zero ES → tool absent.
3. RCA inline endpoint, tool name `zj-elk` → cluster id `zj-elk` (not `rca-es`); two inline RCA tools get two IDs.
4. Same name: datasource + RCA inline → Query uses datasource DSN id; still has default_index from RCA if datasource index empty.
5. RCA `datasource_id` only + bound ES without `default_index` → merged RCA `default_index`.
6. RCA `datasource_id` pointing at unbound id → no extra cluster (warn only).

Use `fakeReader` from a small local stub (duplicate 10 lines) or export nothing from framework/tool tests.

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd portal; go test ./internal/chat -run TestCollectESLog -count=1`

Expected: FAIL.

- [ ] **Step 3: Implement**

`collectESLogClusters(tools []*biz.ToolMeta) (clusters []tool.ESLogCluster, esReg *datasource.Registry)`:

Call `datasource.RegisterElasticsearch(esReg)` before any `Register` (same as current `rca_builder.go`).

1. For each `ToolTypeDatasource` with `isElasticsearchType`: `canonicalDatasourceConfig(name, ConfigFromMap)` into `esReg`; cluster ID = tool name; read `default_index` / `trace_id_field` / `purpose` from the **map** (not `datasource.Config`).
2. For each RCA `es_log_query`, mirror **current** skip rules first: if both `endpoint` and `datasource_id` are set, or both empty → skip + warn (do **not** treat “has endpoint” as inline). Then:
   - Inline endpoint only: if ID exists, keep connection, merge empty query fields + purpose from `t.Description`; else `Register` `Config{ID: t.Name, Type: elasticsearch, DSN: endpoint, User, Password}`.
   - Only `datasource_id`: if cluster exists, merge empty fields; else skip + warn.
3. `RegisterESLogTool(reg, executor.NewESExecutor(esReg), tool.ESLogConfig{Clusters: clusters})`.

Do **not** register elasticsearch into the data-trio registry (existing `SkipDataTools` path stays).

- [ ] **Step 4: Run tests**

Run: `cd portal; go test ./internal/chat -count=1`

Expected: PASS. Fix acceptance tests that still call `registerRCATool` for ES.

- [ ] **Step 5: Commit**

```bash
git add portal/internal/chat/es_log_clusters.go portal/internal/chat/es_log_clusters_test.go portal/internal/chat/rca_builder.go portal/internal/chat/agent_builder.go portal/internal/chat/rca_builder_test.go portal/internal/chat/rca_binding_acceptance_test.go
git commit -m "feat(portal): auto-register es_log_query from elasticsearch bindings"
```

---

### Task 7: Turn Tool Surface — ES datasource is FamilyRCA

**Files:**
- Modify: `portal/internal/chat/tool_families.go` (`BoundFamiliesFrom`)
- Modify: `portal/internal/chat/agent_builder.go` (`filterToolsForSurface`)
- Modify: `portal/internal/chat/tool_families_test.go` (and `agent_builder_test.go` if filter is tested there)

- [ ] **Step 1: Write failing tests**

```go
func TestBoundFamiliesFrom_ESDatasourceIsRCANotData(t *testing.T) {
	es, _ := structpb.NewStruct(map[string]any{
		"datasource": map[string]any{"type": "elasticsearch", "dsn": "http://es:9200"},
	})
	got := BoundFamiliesFrom([]*biz.ToolMeta{{
		Name: "zj-elk", Type: biz.ToolTypeDatasource, Config: es,
	}}, nil, false, false)
	hasRCA, hasData := false, false
	for _, f := range got {
		if f == FamilyRCA {
			hasRCA = true
		}
		if f == FamilyData {
			hasData = true
		}
	}
	if !hasRCA {
		t.Fatal("elasticsearch datasource must bind FamilyRCA")
	}
	if hasData {
		t.Fatal("elasticsearch must not bind FamilyData")
	}
}

func TestFilterToolsForSurface_KeepsESOnRCA(t *testing.T) {
	es, _ := structpb.NewStruct(map[string]any{
		"datasource": map[string]any{"type": "elasticsearch", "dsn": "http://es:9200"},
	})
	mysql, _ := structpb.NewStruct(map[string]any{
		"datasource": map[string]any{"type": "mysql", "dsn": "u:p@tcp(h:3306)/db"},
	})
	tools := []*biz.ToolMeta{
		{Name: "zj-elk", Type: biz.ToolTypeDatasource, Config: es},
		{Name: "pro_mysql", Type: biz.ToolTypeDatasource, Config: mysql},
	}
	out := filterToolsForSurface(tools, familySet([]string{FamilyRCA, FamilyCore}))
	got := namesOf(out) // existing helper returns []string
	join := strings.Join(got, ",")
	if !strings.Contains(join, "zj-elk") {
		t.Fatalf("RCA surface must keep elasticsearch datasource, got %v", got)
	}
	if strings.Contains(join, "pro_mysql") {
		t.Fatalf("RCA surface must not keep mysql, got %v", got)
	}
}
```

Reuse `namesOf` / `familySet` from existing tests; add `namesOf` if missing.

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd portal; go test ./internal/chat -run "TestBoundFamiliesFrom_ESDatasourceIsRCANotData|TestFilterToolsForSurface_KeepsESOnRCA" -count=1`

Expected: FAIL.

- [ ] **Step 3: Implement**

`BoundFamiliesFrom` datasource branch:

```go
case biz.ToolTypeDatasource:
    if isElasticsearchType(datasourceTypeFromMeta(t)) {
        set[FamilyRCA] = struct{}{}
        continue
    }
    if ToolFamilySplitEnabled() {
        set[FamilyData] = struct{}{}
    }
```

`isElasticsearchType` does **not** depend on `ToolFamilySplitEnabled`. Read type from the tool config map (`datasource.type` or flat `type`); do not assume a helper named `datasourceTypeFromMeta` already exists.

`filterToolsForSurface` default branch: if datasource and elasticsearch → `fam = FamilyRCA`; else existing Data/Core logic.

- [ ] **Step 4: Run tests**

Run: `cd portal; go test ./internal/chat -count=1`

Expected: PASS. Also `BuildRegistry` with only ES datasource + RCA-active surface still has `es_log_query` (add this assertion in Task 6 tests if not already).

- [ ] **Step 5: Commit**

```bash
git add portal/internal/chat/tool_families.go portal/internal/chat/agent_builder.go portal/internal/chat/tool_families_test.go
git commit -m "feat(portal): treat elasticsearch datasources as FamilyRCA"
```

---

### Task 8: Datasource prompt lists ES clusters

**Files:**
- Modify: `portal/internal/chat/datasource_prompt.go`
- Modify: `portal/internal/chat/datasource_prompt_test.go`

- [ ] **Step 1: Extend `DatasourceBinding`** with `DefaultIndex`, `Purpose` (optional). When `SkipDataTools` / ES, `FormatDatasourcePrompt` lists:

```
es_log_query(cluster=<id>) — {purpose}；默认索引 {default_index}
```

and `cluster=` in the routing hint. Do not put ES ids in the data-trio bullet list.

- [ ] **Step 2: Test** existing `TestFormatDatasourcePrompt` still skips ES from SQL list; add assert `cluster=` and purpose/index appear.

Run: `cd portal; go test ./internal/chat -run TestFormatDatasourcePrompt -count=1`

- [ ] **Step 3: Wire bindings in `BuildRegistry` / `registerDatasourceTools`**

`purpose` / `default_index` are **not** on `datasource.Config`. When appending ES `DatasourceBinding`, copy those strings from the tool config map (same extraction as Task 6). If you only change `FormatDatasourcePrompt` and leave bindings empty, unit tests with hand-built structs pass but production prompt has no purpose/index.

- [ ] **Step 4: Commit**

```bash
git add portal/internal/chat/datasource_prompt.go portal/internal/chat/datasource_prompt_test.go portal/internal/chat/agent_builder.go
git commit -m "feat(portal): list ES clusters in datasource prompt"
```

Also update `RejectElasticsearchDatasource` message in `framework/tool/data/datasource_id.go` to mention `cluster=` if that string is user-visible.

---

### Task 9: Web — form, binding table, normalize, export

**Files:**
- Modify: `web/src/api/client.ts` — `DatasourceConfig` + `normalizeDatasourceConfig` (mirror RCA camelCase)
- Modify: `web/src/utils/toolExportFormat.ts` + `web/tests/toolImportExport.test.ts`
- Modify: `web/src/pages/ToolForm.tsx`
- Modify: `web/src/pages/AgentDetail.tsx`

- [ ] **Step 1: Failing export test**

```ts
it('keeps elasticsearch purpose and default_index from camelCase', () => {
  const parsed = parseToolsImportJson(JSON.stringify({
    name: 'zj-elk',
    type: 'datasource',
    description: '',
    config: { datasource: { type: 'elasticsearch', dsn: 'http://es:9200', defaultIndex: 'app-*', purpose: '应用日志' } },
  }))
  assert.equal(parsed[0].config.datasource?.default_index, 'app-*')
  assert.equal(parsed[0].config.datasource?.purpose, '应用日志')
})
```

- [ ] **Step 2: `cd web; npm test`** — Expected: FAIL.

- [ ] **Step 3: Implement normalize** in `client.ts` and `toolExportFormat.ts`:

```ts
function normalizeDatasourceConfig(raw: unknown): DatasourceConfig | undefined {
  if (!raw || typeof raw !== 'object') return undefined
  const r = raw as Record<string, unknown>
  return {
    id: r.id as string | undefined,
    type: r.type as string | undefined,
    dsn: r.dsn as string | undefined,
    host: r.host as string | undefined,
    port: r.port as number | undefined,
    user: r.user as string | undefined,
    password: r.password as string | undefined,
    dbname: (r.dbname as string | undefined) ?? (r.dbName as string | undefined),
    read_only: (r.read_only as boolean | undefined) ?? (r.readOnly as boolean | undefined),
    default_index: (r.default_index as string | undefined) ?? (r.defaultIndex as string | undefined),
    trace_id_field: (r.trace_id_field as string | undefined) ?? (r.traceIdField as string | undefined),
    purpose: r.purpose as string | undefined,
  }
}
```

`normalizeToolConfig`: `datasource: normalizeDatasourceConfig(cfg.datasource)`.

**ToolForm:** if `datasource.type` is elasticsearch/es, show 默认索引 / trace 字段 / 用途; on submit if those two required fields empty, `setError` and return. RCA: if **creating** (`!id` from route) hide/disable option `es_log_query`; if **editing** an existing tool whose `func_path === 'es_log_query'`, keep the current ELK fields (including endpoint mutual exclusion).

**AgentDetail** bound table columns: 名称、类型、默认索引、用途、操作.

```tsx
function boundToolIndex(t: Tool): string {
  const ds = t.config?.datasource
  if (ds?.type === 'elasticsearch' || ds?.type === 'es') return ds.default_index || ''
  return t.config?.rca?.default_index || ''
}
function boundToolPurpose(t: Tool): string {
  const ds = t.config?.datasource
  if (ds?.purpose) return ds.purpose
  return t.description || ''
}
```

- [ ] **Step 4: `cd web; npm test`** Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add web/src/api/client.ts web/src/utils/toolExportFormat.ts web/tests/toolImportExport.test.ts web/src/pages/ToolForm.tsx web/src/pages/AgentDetail.tsx
git commit -m "feat(web): show ES cluster purpose and require cluster metadata"
```

---

### Task 10: Skills + operator docs

**Files:**
- Modify: `framework/skills_examples/skills/rca-investigation/SKILL.md`
- Grep: `es_log_query` under `framework/skills_examples` and in-repo `**/SKILL.md`; add `cluster` wherever the call is shown.
- Modify: `portal/docs/rca-es-log-query.md` — positive path: create elasticsearch datasource (`default_index` + `purpose`), bind it; `es_log_query(cluster="<name>")`. Note: do not create new RCA ELK tools. Keep a short “legacy RCA inline still works until migrated” paragraph.

Example skill lines:

```
es_log_query(cluster="<elasticsearch tool name>", trace_id=...)
```

Same task may call a second `cluster` for another bound ES.

Workspace skills outside this repo: skip if not present.

- [ ] **Step 1: Commit**

```bash
git add framework/skills_examples/skills/rca-investigation/SKILL.md portal/docs/rca-es-log-query.md
git commit -m "docs: es_log_query examples require cluster"
```

---

### Task 11: Full regression

- [ ] **Step 1: Framework**

Run: `cd framework; go test ./tool ./agent ./templates ./tool/data -count=1`

Expected: PASS.

- [ ] **Step 2: Portal**

Run: `cd portal; go test ./internal/chat ./internal/biz ./internal/service -count=1`

Expected: PASS.

- [ ] **Step 3: Web**

Run: `cd web; npm test`

Expected: PASS.

- [ ] **Step 4: Manual** (if stack is up)

Bind `zj-elk` and `zj-elk_flow` as elasticsearch datasources on one Agent. One turn: query with `cluster=zj-elk`, then `cluster=zj-elk_flow`, then omit `cluster` and confirm the error lists both names.

---

## Execution notes

- TDD: red test before implementation for Tasks 1, 2, 5, 6, 7, 9.
- Do not remove `es_log_query` from `ValidRCAFuncPath`.
- Do not register ES into the data-trio `MultiExecutor`.
- Mapping in production: `mapperFromReader(reader, clusterID)` per call; injected `FieldMapper` is test-only.
