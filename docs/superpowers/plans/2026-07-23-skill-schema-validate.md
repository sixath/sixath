# Skill Schema Validate Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** `skill_manage` 在写盘/pending 前硬拦不合规 SKILL.md（name+description），并附带质量 `warnings[]`（不阻断）。

**Architecture:** 在 `framework/skills` 抽出 content 版 frontmatter 解析，新增 `ValidateSkillMarkdown` + `AssessSkillQuality`；`skillops` 在 propose/apply 前合成待写入全文并调用。与 Index 单源解析；schema 失败 soft-map 返回；warn 挂在 pending/ok 响应。

**Tech Stack:** Go 1.26、`go.yaml.in/yaml/v2`、现有 `framework/skills` + `framework/tool/skillops` 单测。

**Spec:** [2026-07-23-skill-schema-validate-design.md](../specs/2026-07-23-skill-schema-validate-design.md)

---

## File structure

| 文件 | 职责 |
|------|------|
| Create `framework/skills/validate.go` | `ValidateSkillMarkdown`、`AssessSkillQuality`、warn 常量、kebab 正则、错误/`SkillWarning` 类型 |
| Create `framework/skills/validate_test.go` | H1–H6 + warn 表驱动单测 |
| Modify `framework/skills/index.go` | `parseSkillFrontmatter` 委托 `parseSkillFrontmatterContent`；抽出 content 解析 |
| Modify `framework/tool/skillops/skill_manage_pending.go` | `SkillManagePendingResponse` 增加 `Warnings` |
| Modify `framework/tool/skillops/skill_manager_tool.go` | propose/apply 接线；合成全文 helper；schema 失败 map |
| Modify `framework/tool/skillops/skill_manager_tool_test.go` | §6 接线用例；现有短 description 用例可断言 warnings 或忽略 |
| Modify `docs/superpowers/specs/2026-07-23-skill-schema-validate-design.md` | 状态改为已落地（收尾） |

**工作目录：** 代码与 `go test` 在 `framework/`；本 plan/spec 在 monorepo 根 `docs/`。

---

### Task 1: Content 解析抽取 + ValidateSkillMarkdown（硬规则）

**Files:**
- Create: `framework/skills/validate.go`
- Create: `framework/skills/validate_test.go`
- Modify: `framework/skills/index.go`（`parseSkillFrontmatter`）

- [ ] **Step 1: 写失败测试（H6 / H5）**

在 `framework/skills/validate_test.go`：

```go
package skills

import (
	"strings"
	"testing"
)

func TestValidateSkillMarkdown_RequiresDescription(t *testing.T) {
	content := "---\nname: my-skill\n---\n# Body\n"
	_, _, err := ValidateSkillMarkdown(content, "my-skill")
	if err == nil || !strings.Contains(err.Error(), "description") {
		t.Fatalf("want description error, got %v", err)
	}
}

func TestValidateSkillMarkdown_NameMustMatchParam(t *testing.T) {
	content := "---\nname: other\ndescription: Use when testing name mismatch for skills.\n---\n# Body with enough runes for later quality checks maybe\n"
	_, _, err := ValidateSkillMarkdown(content, "my-skill")
	if err == nil || !strings.Contains(err.Error(), "name") {
		t.Fatalf("want name mismatch error, got %v", err)
	}
}

func TestValidateSkillMarkdown_OK(t *testing.T) {
	content := "---\nname: my-skill\ndescription: Use when validating the happy path for skill schema.\n---\n# My Skill\n\nSteps and success checklist here with ok: true signal.\n"
	meta, body, err := ValidateSkillMarkdown(content, "my-skill")
	if err != nil {
		t.Fatal(err)
	}
	if meta.Name != "my-skill" || meta.Description == "" {
		t.Fatalf("meta: %#v", meta)
	}
	if !strings.Contains(body, "# My Skill") {
		t.Fatalf("body: %q", body)
	}
}
```

- [ ] **Step 2: 跑测确认失败**

Run:

```bash
cd framework
go test ./skills/ -run 'TestValidateSkillMarkdown_' -count=1
```

Expected: FAIL — `ValidateSkillMarkdown` undefined

- [ ] **Step 3: 实现解析抽取 + ValidateSkillMarkdown**

在 `index.go`：将 `parseSkillFrontmatter` 改为读文件后调用新函数：

```go
func parseSkillFrontmatter(path string) (SkillMeta, bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return SkillMeta{}, false, err
	}
	meta, ok, err := parseSkillFrontmatterContent(string(data), path)
	return meta, ok, err
}
```

在 `validate.go` 实现 `parseSkillFrontmatterContent(content, pathForErr string) (SkillMeta, bool, error)`：逻辑从原 `parseSkillFrontmatter` 迁入（BOM、`---`、YAML、缺 name 报错、`ok=false` 无 frontmatter）。**Index 行为不变**：无 frontmatter → `ok=false, err=nil`；缺 name → error（与今日一致，**不**在 Index 路径强制 description）。

```go
package skills

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"
)

var skillNameKebab = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)

const (
	ErrCodeSkillSchemaInvalid = "skill_schema_invalid"
	skillSchemaHint           = "SKILL.md must start with YAML frontmatter containing name and description."
	skillSchemaExample        = "---\nname: my-skill\ndescription: >\n  Use when …\n---\n\n# My Skill\n"
)

type SkillWarning struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// ValidateSkillMarkdown enforces H1–H6 for skill_manage writes.
// Returns meta, body (after frontmatter), or error whose Error() starts with "skill_schema_invalid:".
func ValidateSkillMarkdown(content, expectName string) (SkillMeta, string, error) {
	content = strings.TrimLeft(content, "\ufeff")
	meta, ok, err := parseSkillFrontmatterContent(content, "SKILL.md")
	if err != nil {
		return SkillMeta{}, "", fmt.Errorf("%s: %w", ErrCodeSkillSchemaInvalid, err)
	}
	if !ok {
		return SkillMeta{}, "", fmt.Errorf("%s: missing YAML frontmatter", ErrCodeSkillSchemaInvalid)
	}
	name := strings.TrimSpace(meta.Name)
	expectName = strings.TrimSpace(expectName)
	if name == "" {
		return SkillMeta{}, "", fmt.Errorf("%s: name is required", ErrCodeSkillSchemaInvalid)
	}
	if !skillNameKebab.MatchString(name) {
		return SkillMeta{}, "", fmt.Errorf("%s: name must be kebab-case", ErrCodeSkillSchemaInvalid)
	}
	if name != expectName {
		return SkillMeta{}, "", fmt.Errorf("%s: frontmatter name %q != param name %q", ErrCodeSkillSchemaInvalid, name, expectName)
	}
	if strings.TrimSpace(meta.Description) == "" {
		return SkillMeta{}, "", fmt.Errorf("%s: description is required", ErrCodeSkillSchemaInvalid)
	}
	parts := strings.SplitN(content, "---", 3)
	body := ""
	if len(parts) >= 3 {
		body = parts[2]
	}
	_ = utf8.RuneCountInString // used in AssessSkillQuality
	return meta, body, nil
}
```

把原 frontmatter struct / 归一化逻辑放进 `parseSkillFrontmatterContent`（可仍留在 `index.go` 或移到 `validate.go`；**同一包**即可）。

另加表驱动：无 frontmatter、非法 kebab（`My_Skill`）、YAML 坏。

- [ ] **Step 4: 跑测通过**

```bash
cd framework
go test ./skills/ -run 'TestValidateSkillMarkdown_|TestNewIndex|TestParse' -count=1
```

Expected: PASS（含既有 Index 测）

- [ ] **Step 5: Commit（framework 仓库）**

```bash
cd framework
git add skills/validate.go skills/validate_test.go skills/index.go
git commit -m "feat(skills): add ValidateSkillMarkdown for write-path schema (H1-H6)"
```

---

### Task 2: AssessSkillQuality（软 warn）

**Files:**
- Modify: `framework/skills/validate.go`
- Modify: `framework/skills/validate_test.go`

- [ ] **Step 1: 写失败测试**

```go
func TestAssessSkillQuality_DescTooShortAndNotTrigger(t *testing.T) {
	meta := SkillMeta{Name: "x", Description: "short"}
	body := "# Hi"
	ws := AssessSkillQuality(meta, body)
	codes := map[string]bool{}
	for _, w := range ws {
		codes[w.Code] = true
	}
	for _, want := range []string{"desc_too_short", "desc_not_trigger", "body_too_short", "no_success_signal"} {
		if !codes[want] {
			t.Fatalf("missing %s in %#v", want, ws)
		}
	}
}

func TestAssessSkillQuality_GoodSampleQuiet(t *testing.T) {
	meta := SkillMeta{
		Name:        "rca-investigation",
		Description: "Use when diagnosing production failures with RCA tools and traces.",
	}
	body := strings.Repeat("步骤与验收 checklist，成功信号 ok: true。\n", 20)
	ws := AssessSkillQuality(meta, body)
	if len(ws) != 0 {
		t.Fatalf("want no warnings, got %#v", ws)
	}
}
```

- [ ] **Step 2: 跑测确认失败**

```bash
cd framework
go test ./skills/ -run 'TestAssessSkillQuality_' -count=1
```

Expected: FAIL — undefined

- [ ] **Step 3: 实现 AssessSkillQuality**

常量（本期封闭，扩展另开变更）：

```go
const (
	descTooShortRunes = 40
	bodyTooShortRunes = 120
)

var triggerPrefixes = []string{"use when", "使用时机", "当"} // match on strings.ToLower(desc) for "use when"; 中文保持原样前缀

var successSignals = []string{"成功", "success", "checklist", "验收", "ok:"}
```

规则按 spec §4.2：`utf8.RuneCountInString`；`desc_not_trigger`：trim 后，`Use when` 用 `strings.HasPrefix(strings.ToLower(desc), "use when")`，`使用时机`/`当` 用原文 `HasPrefix`。

`AssessSkillQuality` **永不返回 error**。

- [ ] **Step 4: 跑测通过**

```bash
cd framework
go test ./skills/ -run 'TestAssessSkillQuality_|TestValidateSkillMarkdown_' -count=1
```

Expected: PASS

- [ ] **Step 5: Commit**

```bash
cd framework
git add skills/validate.go skills/validate_test.go
git commit -m "feat(skills): add AssessSkillQuality soft warnings for skill writes"
```

---

### Task 3: skillops 接线 — schema 硬拦 + warnings

**Files:**
- Modify: `framework/tool/skillops/skill_manage_pending.go`
- Modify: `framework/tool/skillops/skill_manager_tool.go`
- Modify: `framework/tool/skillops/skill_manager_tool_test.go`

- [ ] **Step 1: 写失败测试（无 description 不写盘；write_file skill.md）**

在 `skill_manager_tool_test.go` 追加：

```go
func TestSkillManage_CreateRejectsMissingDescription(t *testing.T) {
	root := t.TempDir()
	cfg := skillManageTestConfig(nil, false)
	tl := registerSkillManageForTest(t, cfg)
	res, err := tl.Execute(skillManageTestCtx(root), map[string]any{
		"action":  "create",
		"name":    "bad-skill",
		"content": "---\nname: bad-skill\n---\n# No desc\n",
	})
	if err != nil {
		t.Fatal(err)
	}
	m, ok := res.(map[string]any)
	if !ok {
		t.Fatalf("got %T", res)
	}
	if m["error_code"] != "skill_schema_invalid" {
		t.Fatalf("got %#v", m)
	}
	if _, err := os.Stat(filepath.Join(root, "skills", "bad-skill", "SKILL.md")); !os.IsNotExist(err) {
		t.Fatalf("must not write disk, stat=%v", err)
	}
}

func TestSkillManage_CreatePendingRejectsMissingDescription(t *testing.T) {
	root := t.TempDir()
	cfg := skillManageTestConfig(nil, true)
	tl := registerSkillManageForTest(t, cfg)
	res, err := tl.Execute(skillManageTestCtx(root), map[string]any{
		"action":  "create",
		"name":    "bad-skill",
		"content": "---\nname: bad-skill\n---\n# No desc\n",
	})
	if err != nil {
		t.Fatal(err)
	}
	m, ok := res.(map[string]any)
	if !ok {
		t.Fatalf("expected error map, got %T %#v", res, res)
	}
	if m["error_code"] != "skill_schema_invalid" {
		t.Fatalf("got %#v", m)
	}
}

func TestSkillManage_WriteFileSkillMDEqualFoldValidates(t *testing.T) {
	root := t.TempDir()
	cfg := skillManageTestConfig(nil, false)
	tl := registerSkillManageForTest(t, cfg)
	// seed skill
	good := "---\nname: wf-skill\ndescription: Use when testing write_file validation path for skills.\n---\n# Body\n\nSuccess checklist ok: true\n"
	if _, err := tl.Execute(skillManageTestCtx(root), map[string]any{
		"action": "create", "name": "wf-skill", "content": good,
	}); err != nil {
		t.Fatal(err)
	}
	res, err := tl.Execute(skillManageTestCtx(root), map[string]any{
		"action":       "write_file",
		"name":         "wf-skill",
		"file_path":    "skill.md",
		"file_content": "---\nname: wf-skill\n---\n# no description\n",
	})
	if err != nil {
		t.Fatal(err)
	}
	m := res.(map[string]any)
	if m["error_code"] != "skill_schema_invalid" {
		t.Fatalf("got %#v", m)
	}
}

func TestSkillManage_CreateOKIncludesDescTooShortWarning(t *testing.T) {
	root := t.TempDir()
	cfg := skillManageTestConfig(nil, false)
	tl := registerSkillManageForTest(t, cfg)
	content := "---\nname: new-skill\ndescription: test\n---\n# Hello"
	res, err := tl.Execute(skillManageTestCtx(root), map[string]any{
		"action": "create", "name": "new-skill", "content": content,
	})
	if err != nil {
		t.Fatal(err)
	}
	m := res.(map[string]any)
	if m["status"] != "ok" {
		t.Fatalf("%#v", m)
	}
	ws, _ := m["warnings"].([]skills.SkillWarning)
	// if JSON-shaped as []any after map, accept either []SkillWarning or detect code in fmt
	found := false
	switch v := m["warnings"].(type) {
	case []skills.SkillWarning:
		for _, w := range v {
			if w.Code == "desc_too_short" {
				found = true
			}
		}
	case []any:
		for _, it := range v {
			if w, ok := it.(skills.SkillWarning); ok && w.Code == "desc_too_short" {
				found = true
			}
		}
	}
	_ = ws
	if !found {
		// also allow warnings as []SkillWarning stored directly
		if wlist, ok := m["warnings"].([]skills.SkillWarning); ok {
			for _, w := range wlist {
				if w.Code == "desc_too_short" {
					found = true
				}
			}
		}
	}
	if !found {
		t.Fatalf("want desc_too_short in warnings: %#v", m["warnings"])
	}
}
```

（实现时可将 warnings 断言整理成 helper，避免双重 switch。）

- [ ] **Step 2: 跑测确认失败**

```bash
cd framework
go test ./tool/skillops/ -run 'TestSkillManage_CreateRejects|TestSkillManage_CreatePendingRejects|TestSkillManage_WriteFileSkillMD|TestSkillManage_CreateOKIncludes' -count=1
```

Expected: FAIL（仍写盘或无 error_code）

- [ ] **Step 3: 实现接线**

1. `SkillManagePendingResponse` 增加：

```go
Warnings []skills.SkillWarning `json:"warnings,omitempty"`
```

2. Helper（`skill_manager_tool.go`）：

```go
func skillSchemaErrorResult(err error) map[string]any {
	return map[string]any{
		"error":      err.Error(),
		"error_code": skills.ErrCodeSkillSchemaInvalid,
		"hint":       skills.SkillSchemaHint,      // export constants from validate.go
		"example":    skills.SkillSchemaExample,
	}
}

func skillManageMarkdownForValidate(workspace, action, name string, params map[string]any) (content string, need bool, err error) {
	switch action {
	case "create", "edit":
		c, _ := params["content"].(string)
		return c, true, nil
	case "patch":
		batch, err := skillManageToPatches(workspace, action, name, params)
		if err != nil {
			return "", true, err
		}
		if len(batch) == 1 {
			return batch[0].New, true, nil
		}
		return "", true, fmt.Errorf("patch produced no content")
	case "write_file":
		fp, _ := params["file_path"].(string)
		if !strings.EqualFold(filepath.Base(fp), "SKILL.md") {
			return "", false, nil
		}
		fc, _ := params["file_content"].(string)
		return fc, true, nil
	default:
		return "", false, nil
	}
}

func validateSkillManageContent(workspace, action, name string, params map[string]any) (warnings []skills.SkillWarning, errResult map[string]any) {
	content, need, err := skillManageMarkdownForValidate(workspace, action, name, params)
	if err != nil {
		return nil, map[string]any{"error": err.Error()}
	}
	if !need {
		return nil, nil
	}
	meta, body, err := skills.ValidateSkillMarkdown(content, name)
	if err != nil {
		return nil, skillSchemaErrorResult(err)
	}
	return skills.AssessSkillQuality(meta, body), nil
}
```

导出 `SkillSchemaHint` / `SkillSchemaExample`（或包内小写 + skillops 本地常量字符串，与 spec 一致即可）。

3. **顺序（写死）：** 现有必填字段检查 → `ScanUserContent` → **validate** → pinned 检查 → SavePending / Apply。  
   Spec §6.6：注入扫描优先于 schema —— 即 **Scan 在 Validate 之前**（恶意内容先被扫掉）。

4. `proposeSkillManage`：validate 失败直接 return error map；成功则 `pending` 响应带 `Warnings`。

5. `applySkillManage`：validate 失败不写盘；成功 `ok` map 带 `"warnings": warnings`（空则省略或空切片）。

6. `confirmSkillManage` → `applySkillManage`：apply 内再次校验（防 pending 绕过）。

- [ ] **Step 4: 跑测通过**

```bash
cd framework
go test ./tool/skillops/ -count=1
go test ./skills/ -count=1
```

Expected: PASS（修复任何因 `Warnings` 字段或短 description 行为引起的既有断言）

补充用例（可同 commit）：

- patch 破坏 frontmatter → `skill_schema_invalid`
- name mismatch → 失败
- 合法长 description create → `NewIndex` 可 GetByName

- [ ] **Step 5: Commit**

```bash
cd framework
git add tool/skillops/skill_manager_tool.go tool/skillops/skill_manage_pending.go tool/skillops/skill_manager_tool_test.go skills/validate.go
git commit -m "feat(tool/skillops): gate skill_manage writes with schema validate + quality warnings"
```

---

### Task 4: 文档收尾

**Files:**
- Modify: `docs/superpowers/specs/2026-07-23-skill-schema-validate-design.md`（monorepo 根）
- Optional: `framework/docs/toolsets-hermes-mapping.md` 一行提及 write-path schema（YAGNI：可跳过）

- [ ] **Step 1: 更新 spec 状态**

将头部改为：`**状态**: 已落地`，并在 §9 注明实现计划路径。

- [ ] **Step 2: Commit（monorepo 根）**

```bash
git add docs/superpowers/specs/2026-07-23-skill-schema-validate-design.md docs/superpowers/plans/2026-07-23-skill-schema-validate.md
git commit -m "docs: mark skill schema validate design landed; add implementation plan"
```

（若 plan 已先提交，本步只改 spec 状态。）

---

## 完成定义

- [ ] 无 description / 无 frontmatter / name 不一致的 create **不能** pending 或 ok  
- [ ] `write_file` + `skill.md` EqualFold 同样硬拦  
- [ ] 短 description 仍可写入，但带 `desc_too_short`（及可能的其它 warn）  
- [ ] `go test ./skills/ ./tool/skillops/ -count=1` PASS  
- [ ] Index 对历史坏文件行为不变  

---

## 备注（给实施者）

- 既有测试 content 常用 `description: test` —— **合法（H6）**，会触发 warn；不要为了消除 warn 去放宽 H6。  
- 不要在 Index 路径强制 description（会破坏扫盘兼容）；仅 `ValidateSkillMarkdown` 强制。  
- `tags`/`scope`/`allowed_tools` **无**额外类型校验。  
- 改 frontmatter `name` rename：**不支持**（H5）。  
)
