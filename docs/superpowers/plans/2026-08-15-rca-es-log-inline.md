# RCA es_log_query Inline ES Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.  
> Spec confirmed 2026-08-15.  
> **Do not commit unless the user asks.** Prefer one logical commit per Task when committing is requested.

**Goal:** Let RCA `es_log_query` register from an inline ES `endpoint` (optional auth) while keeping `datasource_id` as a mutually exclusive save-time alternative.

**Architecture:** Add `ValidateRCAESLogConfig` in Portal biz and enforce it on Create/Update for RCA/`es_log_query` (exactly one of `endpoint` or `datasource_id`). Extend `RCAConfig` proto + ToolForm with `endpoint`/`user`/`password`. At runtime, `registerRCATool` builds a synthetic elasticsearch reader (`ID=rca-es`) when only `endpoint` is set; otherwise keep `buildESReaderFromAgentTools`. Mirror inline fields on framework `config.RCAESConfig` + `templates/rca_wiring.go` with the same exactly-one rule.

**Tech Stack:** Go (Portal + framework), protobuf/`make api`, React `ToolForm`, existing `datasource`/`executor` ES stack.

**Spec:** [docs/superpowers/specs/2026-08-15-rca-es-log-inline-design.md](../specs/2026-08-15-rca-es-log-inline-design.md)

---

## File map

| File | Responsibility |
|------|----------------|
| Create `portal/internal/biz/rca_es_validate.go` | `ValidateRCAESLogConfig(endpoint, datasourceID string) error` + sentinel errors |
| Create `portal/internal/biz/rca_es_validate_test.go` | Matrix: both/neither/only-one |
| Modify `portal/internal/biz/tool.go` | Call validate on Create/Update when type=rca and func_path=es_log_query |
| Modify `portal/api/tool/v1/tool.proto` | `RCAConfig.endpoint/user/password` |
| Regenerate | `portal/api/tool/v1/*.pb.go` via `make api` (from `portal/`) |
| Modify `portal/internal/service/tool.go` | Encode/decode new RCA fields |
| Modify `portal/internal/service/tool_rca_test.go` | Round-trip new fields |
| Modify `portal/internal/chat/rca_builder.go` | Inline ES register path; defensive skip on both/neither |
| Modify `portal/internal/chat/rca_builder_test.go` | Inline / both / neither / legacy id |
| Modify `framework/config/config.go` | `RCAESConfig.Endpoint/User/Password` |
| Modify `framework/templates/rca_wiring.go` | Exactly one of Endpoint / DatasourceID (yaml process config) |
| Modify `framework/templates/rca_wiring_test.go` (+ config tests as needed) | Cover inline Endpoint registration |
| Modify `web/src/pages/ToolForm.tsx` | Inline fields + mutual exclusion UX |
| Modify `web/src/api/client.ts` | Types + `normalizeRCAConfig` must pass `endpoint`/`user`/`password` |
| Modify `web/src/utils/toolExportFormat.ts` | Export/import must not drop new keys |
| Modify docs (short) | Operator note: prefer endpoint |

**Out of scope:** MEA/ReAct entry changes; dropdown of bound datasources; removing datasource tool type; changing `es_log_query` tool parameters.

---

### Task 1: Save-time validation (`ValidateRCAESLogConfig`)

**Files:**
- Create: `portal/internal/biz/rca_es_validate.go`
- Create: `portal/internal/biz/rca_es_validate_test.go`
- Modify: `portal/internal/biz/tool.go` (`Create` / `Update`)

- [ ] **Step 1: Write failing tests**

```go
package biz

import "testing"

func TestValidateRCAESLogConfig(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name, endpoint, dsID string
		wantErr              bool
	}{
		{"both", "http://es:9200", "es-logs", true},
		{"neither", "", "", true},
		{"endpoint_only", "http://es:9200", "", false},
		{"ds_only", "", "es-logs", false},
		{"trim_spaces_both", "  ", "  ", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateRCAESLogConfig(tc.endpoint, tc.dsID)
			if tc.wantErr && err == nil {
				t.Fatal("expected error")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("unexpected: %v", err)
			}
		})
	}
}
```

- [ ] **Step 2: Run test — expect FAIL**

```bash
cd portal && go test ./internal/biz/ -run TestValidateRCAESLogConfig -count=1
```

Expected: FAIL (`ValidateRCAESLogConfig` undefined)

- [ ] **Step 3: Implement validator**

```go
package biz

import (
	"errors"
	"strings"
)

var (
	ErrRCAESLogMutualExclusive = errors.New("rca es_log_query: endpoint and datasource_id are mutually exclusive")
	ErrRCAESLogMissingConn     = errors.New("rca es_log_query: require endpoint or datasource_id")
)

// ValidateRCAESLogConfig enforces save-time mutual exclusion for es_log_query.
func ValidateRCAESLogConfig(endpoint, datasourceID string) error {
	ep := strings.TrimSpace(endpoint)
	ds := strings.TrimSpace(datasourceID)
	if ep != "" && ds != "" {
		return ErrRCAESLogMutualExclusive
	}
	if ep == "" && ds == "" {
		return ErrRCAESLogMissingConn
	}
	return nil
}
```

Map these to client-facing 400s. Prefer wrapping as `kratosErrors.BadRequest("RCA_ES_LOG_CONFIG", "…")` with messages aligned to the spec table (互斥 / 二者皆空), so ToolForm can show the same text.

- [ ] **Step 4: Wire into Create/Update**

Helper (same package):

```go
func rcaESLogFieldsFromConfig(config *structpb.Struct) (funcPath, endpoint, dsID string) {
	if config == nil {
		return "", "", ""
	}
	m := config.AsMap()
	rca, _ := m["rca"].(map[string]any)
	if rca == nil {
		return "", "", ""
	}
	fp, _ := rca["func_path"].(string)
	ep, _ := rca["endpoint"].(string)
	ds, _ := rca["datasource_id"].(string)
	return fp, ep, ds
}
```

**Create:** after normalizing `tt`, if `tt == ToolTypeRCA` and `func_path == "es_log_query"`, validate before `repo.Create`.

**Update (critical):** `toolType` may be nil on config-only updates. Load existing tool via `repo.GetByID` when needed (perm check alone does not return `ToolMeta`). Effective type =

1. `ToolType(*toolType)` if `toolType != nil` and non-empty, else
2. existing tool `.Type` from `GetByID`

Only when **effective type is `rca`**, **`config != nil`** (this request writes config), and **`func_path == "es_log_query"`**, call `ValidateRCAESLogConfig`. Do not validate other RCA func paths. Prefer a unit test covering Update with nil `toolType` + existing RCA/`es_log_query` config.

- [ ] **Step 5: Re-run tests**

```bash
cd portal && go test ./internal/biz/ -run "TestValidateRCAESLogConfig|TestTool" -count=1
```

Expected: PASS for new tests; no ACL regressions.

- [ ] **Step 6: Commit (only if user asked)**

```bash
git add portal/internal/biz/rca_es_validate.go portal/internal/biz/rca_es_validate_test.go portal/internal/biz/tool.go
git commit -m "feat(portal): validate RCA es_log_query endpoint vs datasource_id"
```

---

### Task 2: Proto + service codec

**Files:**
- Modify: `portal/api/tool/v1/tool.proto` (`RCAConfig`)
- Regenerate pb under `portal/api/tool/v1/`
- Modify: `portal/internal/service/tool.go`
- Modify: `portal/internal/service/tool_rca_test.go`
- Modify: `portal/openapi.yaml` if maintained by hand for these fields
- Modify: `web/src/api/client.ts` — RCA type fields **and** `normalizeRCAConfig` (camelCase → snake_case for `endpoint`/`user`/`password`; omitting drops edit reload)

- [ ] **Step 1: Extend proto**

In `RCAConfig`:

```protobuf
  string endpoint = 7;           // es_log_query: inline ES URL (DSN)
  string user = 8;               // es_log_query: optional basic auth user
  string password = 9;           // es_log_query: optional basic auth password
```

Keep fields 4–6 as-is (`datasource_id`, `default_index`, `trace_id_field`).

- [ ] **Step 2: Regenerate**

```bash
cd portal && make api
```

Expected: pb files update without error.

- [ ] **Step 3: Codec round-trip**

In `protoToolConfigToStruct` / map→proto paths in `tool.go`, add:

- read/write `endpoint`, `user`, `password` alongside existing RCA keys.

Extend `tool_rca_test.go` fixture to assert round-trip of the three new fields.

- [ ] **Step 4: Run tests**

```bash
cd portal && go test ./internal/service/ -run RCA -count=1
```

Expected: PASS

- [ ] **Step 5: Commit (only if user asked)**

```bash
git add portal/api/tool/v1/ portal/internal/service/tool.go portal/internal/service/tool_rca_test.go portal/openapi.yaml web/src/api/client.ts
git commit -m "feat(api): add RCA es_log_query endpoint user password fields"
```

---

### Task 3: Runtime inline registration (`rca_builder`)

**Files:**
- Modify: `portal/internal/chat/rca_builder.go`
- Modify: `portal/internal/chat/rca_builder_test.go`

- [ ] **Step 1: Write failing tests**

```go
func TestRegisterRCATool_ESInlineEndpoint(t *testing.T) {
	reg := tool.NewRegistry()
	cfg := map[string]any{"rca": map[string]any{
		"func_path": "es_log_query",
		"endpoint":  "http://localhost:9200",
		"default_index": "app-*",
	}}
	registerRCATool(reg, cfg, nil) // no agent datasource tools
	if !rcaHas(reg, "es_log_query") {
		t.Fatal("inline endpoint should register es_log_query without agent datasource")
	}
}

func TestRegisterRCATool_ESBothSkip(t *testing.T) {
	reg := tool.NewRegistry()
	registerRCATool(reg, map[string]any{"rca": map[string]any{
		"func_path": "es_log_query", "endpoint": "http://es:9200", "datasource_id": "es-logs",
	}}, nil)
	if rcaHas(reg, "es_log_query") {
		t.Fatal("both endpoint and datasource_id must skip")
	}
}

func TestRegisterRCATool_ESNeitherSkip(t *testing.T) {
	reg := tool.NewRegistry()
	registerRCATool(reg, map[string]any{"rca": map[string]any{"func_path": "es_log_query"}}, nil)
	if rcaHas(reg, "es_log_query") {
		t.Fatal("neither connection mode must skip")
	}
}
```

Keep existing `TestRegisterRCATool_ESFound` for `datasource_id` path.

- [ ] **Step 2: Run — expect FAIL** on inline test

```bash
cd portal && go test ./internal/chat/ -run "TestRegisterRCATool_ES" -count=1
```

- [ ] **Step 3: Implement `case "es_log_query"`**

Pseudo:

```go
case "es_log_query":
	endpoint, _ := rcaMap["endpoint"].(string)
	dsID, _ := rcaMap["datasource_id"].(string)
	endpoint = strings.TrimSpace(endpoint)
	dsID = strings.TrimSpace(dsID)
	if (endpoint != "") == (dsID != "") { // both or neither
		slog.Warn("rca: es_log_query need exactly one of endpoint or datasource_id, skip")
		return
	}
	var reader executor.Reader
	var ok bool
	queryDSID := dsID
	if endpoint != "" {
		const inlineID = "rca-es"
		dsCfg := datasource.Config{ID: inlineID, Type: datasource.TypeElasticsearch, DSN: endpoint}
		if u, _ := rcaMap["user"].(string); u != "" {
			dsCfg.User = u
			dsCfg.Password, _ = rcaMap["password"].(string)
		}
		dsReg := datasource.NewRegistry()
		datasource.RegisterElasticsearch(dsReg)
		if _, err := dsReg.Register(dsCfg); err != nil {
			slog.Warn("rca: inline es register failed", "err", err)
			return
		}
		reader = executor.NewESExecutor(dsReg)
		ok = true
		queryDSID = inlineID
	} else {
		reader, ok = buildESReaderFromAgentTools(agentTools, dsID)
		if !ok {
			slog.Warn("rca: es_log_query datasource not found among agent tools, skip", "datasource_id", dsID)
			return
		}
	}
	defaultIndex, _ := rcaMap["default_index"].(string)
	traceIDField, _ := rcaMap["trace_id_field"].(string)
	_ = tool.RegisterESLogTool(reg, reader, tool.ESLogConfig{
		DatasourceID: queryDSID,
		DefaultIndex: defaultIndex,
		TraceIDField: traceIDField,
	})
```

Note: `(endpoint != "") == (dsID != "")` is true for both-empty and both-nonempty — correct skip.

- [ ] **Step 4: Run tests**

```bash
cd portal && go test ./internal/chat/ -run "TestRegisterRCATool_ES" -count=1
```

Expected: PASS

- [ ] **Step 5: Commit (only if user asked)**

```bash
git add portal/internal/chat/rca_builder.go portal/internal/chat/rca_builder_test.go
git commit -m "feat(portal): register es_log_query from inline ES endpoint"
```

---

### Task 4: Framework config + `rca_wiring`

**Files:**
- Modify: `framework/config/config.go` (`RCAESConfig`)
- Modify: `framework/templates/rca_wiring.go`
- Modify: `framework/config/config_test.go` and/or template tests as needed

- [ ] **Step 1: Extend `RCAESConfig`**

```go
type RCAESConfig struct {
	Endpoint     string `json:"endpoint" yaml:"endpoint"`
	User         string `json:"user" yaml:"user"`
	Password     string `json:"password" yaml:"password"`
	DatasourceID string `json:"datasource_id" yaml:"datasource_id"`
	DefaultIndex string `json:"default_index" yaml:"default_index"`
	TraceIDField string `json:"trace_id_field" yaml:"trace_id_field"`
}
```

- [ ] **Step 2: Update `registerRCATools` ES branch**

Same mutual rule: exactly one of `Endpoint` / `DatasourceID`. Inline → synthetic `rca-es` + `DSN: Endpoint`. Legacy → `buildRCAESReader`.

- [ ] **Step 3: Tests**

Extend `framework/templates/rca_wiring_test.go` with a case that sets only `Endpoint` (no `DatasourceID`) and asserts `es_log_query` is registered. Adjust `framework/config/config_test.go` if fixtures assert ES field sets.

```bash
cd framework && go test ./config/ ./templates/ -count=1
```

Expected: PASS

- [ ] **Step 4: Commit (only if user asked)**

```bash
git add framework/config/config.go framework/config/config_test.go framework/templates/rca_wiring.go
git commit -m "feat(framework): support inline ES endpoint for RCA wiring"
```

---

### Task 5: Web ToolForm UX

**Files:**
- Modify: `web/src/pages/ToolForm.tsx` (`es_log_query` section ~408+)
- Modify: `web/src/api/client.ts` RCA types **and** `normalizeRCAConfig` (must pass through `endpoint`/`user`/`password`)
- Modify: `web/src/utils/toolExportFormat.ts` if it exports RCA fields (must not drop the new keys)

- [ ] **Step 1: Add form fields + normalize passthrough**

Under `func_path === 'es_log_query'`:

1. Label **ES 地址** → `config.rca.endpoint` (placeholder `http://host:9200`)
2. **用户** / **密码** → `user` / `password`
3. Keep default_index / trace_id_field
4. Relabel datasource_id: **或：引用已绑定 datasource 工具名（与上方地址二选一）**

Also update `normalizeRCAConfig` / `normalizeExportConfig` to copy `endpoint`/`user`/`password` (and camelCase variants), or edit reload / import-export will strip inline config.

- [ ] **Step 2: Client-side gate before submit**

```ts
const ep = (config.rca?.endpoint || '').trim()
const ds = (config.rca?.datasource_id || '').trim()
if (config.rca?.func_path === 'es_log_query') {
  if ((ep && ds) || (!ep && !ds)) {
    // show error; return early — same messages as API
  }
}
```

- [ ] **Step 3: Manual smoke**

Open Tool form → RCA → ELK 日志 → fill only URL → save succeeds; fill both → blocked.

- [ ] **Step 4: Commit (only if user asked)**

```bash
git add web/src/pages/ToolForm.tsx web/src/api/client.ts web/src/utils/toolExportFormat.ts
git commit -m "feat(web): RCA es_log_query inline ES fields with mutual exclusion"
```

---

### Task 6: Operator docs + acceptance checklist

**Files:**
- Modify: short note in an existing portal RCA doc if present; else add 5–10 lines under `portal/docs/` (e.g. extend whatever documents RCA tools) **or** link from `docs/superpowers/specs/2026-08-15-rca-es-log-inline-design.md` status → 「已实现」when done
- Optional: flip spec status line to implemented after verification

- [ ] **Step 1: Document preferred path**

State: prefer `endpoint` on RCA tool; `datasource_id` is mutually exclusive compatibility mode; do not put URLs in `datasource_id`.

- [ ] **Step 2: Run full relevant tests**

```bash
cd portal && go test ./internal/biz/ ./internal/chat/ ./internal/service/ -count=1
cd framework && go test ./config/ ./tool/ -run "ES|RCA|es_log" -count=1
```

Expected: PASS

- [ ] **Step 3: Spec acceptance self-check**

Walk §7 of the design spec mentally against the branch.

- [ ] **Step 4: Commit (only if user asked)**

```bash
git add portal/docs docs/superpowers/specs/2026-08-15-rca-es-log-inline-design.md
git commit -m "docs: note RCA es_log_query inline endpoint usage"
```

---

## Done when

- [ ] Create/Update rejects both-filled and neither for `es_log_query`
- [ ] Inline `endpoint` registers `es_log_query` without a bound datasource tool
- [ ] Legacy `datasource_id` path still works
- [ ] ToolForm exposes endpoint/user/password with client-side mutual exclusion
- [ ] Framework yaml RCA ES supports inline endpoint
- [ ] No MEA/ReAct entry changes
