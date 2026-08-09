package toolskill

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/sixath/framework/growth"
	"github.com/sixath/framework/skills"
	"github.com/sixath/framework/tool"
)

// SkillManageConfig configures skill_manage runtime writes.
type SkillManageConfig struct {
	Index                      *skills.Index
	Lease                      *growth.RuntimeWriteLease
	PendingStore               SkillManagePendingStore
	TokenGen                   tool.TokenGenerator
	RequireCreateDeleteConfirm bool
	// RequirePatchConfirm when true, patch/edit/write_file/remove_file also need confirm_token (G4 chat path).
	RequirePatchConfirm bool
	// RequireUIConfirm when true, confirm_token is rejected unless tool.WithSkillManageUIConfirm
	// is set on ctx. Prevents the model from self-confirming after propose and consuming the
	// pending before the chat UI card is clicked (already_used).
	RequireUIConfirm  bool
	ConfirmTTLSeconds int
}

// RegisterSkillManageTool registers Hermes skill_manage (runtime CRUD).
func RegisterSkillManageTool(reg *tool.Registry, cfg *SkillManageConfig) error {
	if reg == nil {
		return errors.New("skill_manage: registry is nil")
	}
	lease := growth.DefaultRuntimeWriteLease
	requireConfirm := true
	requirePatchConfirm := false
	ttl := 300
	if cfg != nil {
		if cfg.Lease != nil {
			lease = cfg.Lease
		}
		requireConfirm = cfg.RequireCreateDeleteConfirm
		requirePatchConfirm = cfg.RequirePatchConfirm
		if cfg.ConfirmTTLSeconds > 0 {
			ttl = cfg.ConfirmTTLSeconds
		}
	}

	return reg.Register(tool.Tool{
		Name: "skill_manage",
		Description: "Manage skills (create, patch, edit, delete, write_file, remove_file). " +
			"Pinned skills reject writes. create/delete require user confirm via the chat confirmation card; " +
			"Do NOT pass confirm_token yourself — wait for the user to confirm in the UI. " +
			"patch/edit/file writes may also require the same UI confirm when configured.",
		Toolset: tool.ToolsetSkills,
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"action": map[string]any{
					"type": "string",
					"enum": []string{"create", "patch", "edit", "delete", "write_file", "remove_file"},
				},
				"name": map[string]any{
					"type":        "string",
					"description": "Skill name (kebab-case).",
				},
				"content": map[string]any{
					"type":        "string",
					"description": "Full SKILL.md body for create/edit.",
				},
				"old_string": map[string]any{
					"type":        "string",
					"description": "For patch: text to replace.",
				},
				"new_string": map[string]any{
					"type":        "string",
					"description": "For patch: replacement text.",
				},
				"replace_all": map[string]any{
					"type":        "boolean",
					"description": "For patch: replace all occurrences (default false).",
				},
				"file_path": map[string]any{
					"type":        "string",
					"description": "Relative path under skill directory for write_file/remove_file.",
				},
				"file_content": map[string]any{
					"type":        "string",
					"description": "Content for write_file.",
				},
				"confirm_token": map[string]any{
					"type":        "string",
					"description": "UI-only confirmation token. Agents must not set this; the chat UI applies it after the user confirms.",
				},
			},
			"required": []string{"action", "name"},
		},
		Execute: buildSkillManageExecute(cfg, lease, requireConfirm, requirePatchConfirm, ttl),
	})
}

func buildSkillManageExecute(cfg *SkillManageConfig, lease *growth.RuntimeWriteLease, requireConfirm, requirePatchConfirm bool, ttl int) tool.ExecuteFunc {
	return func(ctx context.Context, params map[string]any) (any, error) {
		ws, _ := ctx.Value(tool.ContextKeyWorkspaceRoot).(string)
		if strings.TrimSpace(ws) == "" {
			return map[string]any{"error": "workspace_root not set"}, nil
		}

		if token, _ := params["confirm_token"].(string); strings.TrimSpace(token) != "" {
			return confirmSkillManage(ctx, cfg, lease, ws, token, ttl)
		}

		action, _ := params["action"].(string)
		name, _ := params["name"].(string)
		if action == "" || name == "" {
			return map[string]any{"error": "action and name are required"}, nil
		}

		if skillManageRequiresConfirm(action, requireConfirm, requirePatchConfirm) {
			return proposeSkillManage(ctx, cfg, ws, action, name, params, ttl)
		}

		return applySkillManage(ctx, lease, ws, action, name, params)
	}
}

func proposeSkillManage(ctx context.Context, cfg *SkillManageConfig, workspace, action, name string, params map[string]any, ttl int) (any, error) {
	if cfg == nil || cfg.PendingStore == nil || cfg.TokenGen == nil {
		return map[string]any{"error": "skill_manage: confirm store not configured"}, nil
	}
	sessionID, _ := ctx.Value(tool.ContextKeySessionID).(string)
	if sessionID == "" {
		return map[string]any{"error": "session_id is required for skill_manage confirm"}, nil
	}

	content, _ := params["content"].(string)
	if action == "create" && strings.TrimSpace(content) == "" {
		return map[string]any{"error": "content is required for create"}, nil
	}
	if action == "edit" && strings.TrimSpace(content) == "" {
		return map[string]any{"error": "content is required for edit"}, nil
	}
	if action == "patch" {
		oldS, _ := params["old_string"].(string)
		if strings.TrimSpace(oldS) == "" {
			return map[string]any{"error": "old_string is required for patch"}, nil
		}
	}
	if err := skillManageScanParams(action, params); err != nil {
		return map[string]any{"error": err.Error()}, nil
	}

	warnings, errMap := validateSkillManageContent(workspace, action, name, params)
	if errMap != nil {
		return errMap, nil
	}

	pinned, err := growth.IsSkillPinned(workspace, name)
	if err != nil {
		return map[string]any{"error": err.Error()}, nil
	}
	if pinned {
		return map[string]any{
			"error": "skill_pinned",
			"hint":  "unpin via curator before modifying this skill",
			"name":  name,
		}, nil
	}

	token, err := cfg.TokenGen.NewToken()
	if err != nil {
		return map[string]any{"error": fmt.Sprintf("generate token: %v", err)}, nil
	}
	pending := PendingSkillManage{
		Token:     token,
		Action:    action,
		Name:      name,
		Content:   content,
		CreatedAt: time.Now(),
	}
	if action == "patch" {
		pending.OldString, _ = params["old_string"].(string)
		pending.NewString, _ = params["new_string"].(string)
		pending.ReplaceAll, _ = params["replace_all"].(bool)
	}
	if action == "write_file" || action == "remove_file" {
		pending.FilePath, _ = params["file_path"].(string)
		pending.FileContent, _ = params["file_content"].(string)
	}
	if err := cfg.PendingStore.SavePending(ctx, sessionID, pending); err != nil {
		return map[string]any{"error": err.Error()}, nil
	}

	preview := skillManagePreview(action, name, content, pending)
	return SkillManagePendingResponse{
		Status:    "pending",
		Token:     token,
		Action:    action,
		Name:      name,
		Preview:   preview,
		ExpiresIn: ttl,
		Warnings:  warnings,
	}, nil
}

func confirmSkillManage(ctx context.Context, cfg *SkillManageConfig, lease *growth.RuntimeWriteLease, workspace, token string, ttl int) (any, error) {
	if cfg == nil || cfg.PendingStore == nil {
		return map[string]any{"error": "skill_manage: confirm store not configured"}, nil
	}
	if cfg.RequireUIConfirm && !tool.SkillManageUIConfirmAllowed(ctx) {
		return map[string]any{
			"error":      "请等待用户在确认卡上点确认；勿自行传 confirm_token",
			"error_code": "ui_confirm_required",
		}, nil
	}
	sessionID, _ := ctx.Value(tool.ContextKeySessionID).(string)
	if sessionID == "" {
		return map[string]any{"error": "session_id is required"}, nil
	}

	pending, err := cfg.PendingStore.GetPending(ctx, sessionID, token)
	if err != nil {
		return map[string]any{"error": err.Error()}, nil
	}
	if pending == nil {
		reason, ok := cfg.PendingStore.TombstoneReason(ctx, sessionID, token)
		if !ok {
			reason = "not_found"
		}
		return skillManageConfirmError(reason), nil
	}
	if ttl <= 0 {
		ttl = 300
	}
	if time.Since(pending.CreatedAt) > time.Duration(ttl)*time.Second {
		_ = cfg.PendingStore.DeletePending(ctx, sessionID, token)
		return skillManageConfirmError("expired"), nil
	}

	params := pendingParamsFromSkillManage(*pending)
	result, err := applySkillManage(ctx, lease, workspace, pending.Action, pending.Name, params)
	if err != nil {
		// 保留 pending，允许用户用同一 token 重试。
		return result, err
	}
	if m, ok := result.(map[string]any); ok {
		if ev, has := m["error"]; has && ev != nil && fmt.Sprint(ev) != "" {
			return result, nil
		}
	}
	_ = cfg.PendingStore.ConsumePending(ctx, sessionID, token)
	return result, nil
}

func skillManageConfirmError(code string) map[string]any {
	msg := "确认已失效（可能已被替换、已使用或服务重启），请重新发起"
	switch code {
	case "superseded":
		msg = "确认已失效：已被更新的提案替换，请确认最新卡片"
	case "already_used":
		msg = "该确认已使用过"
	case "expired":
		msg = "确认已过期，请让助手重新发起操作"
	case "not_found":
		// default message
	default:
		code = "not_found"
	}
	return map[string]any{"error": msg, "error_code": code}
}

func applySkillManage(ctx context.Context, lease *growth.RuntimeWriteLease, workspace, action, name string, params map[string]any) (any, error) {
	content, _ := params["content"].(string)
	if action == "create" && strings.TrimSpace(content) == "" {
		return map[string]any{"error": "content is required for create"}, nil
	}
	if action == "edit" && strings.TrimSpace(content) == "" {
		return map[string]any{"error": "content is required for edit"}, nil
	}
	if action == "patch" {
		oldS, _ := params["old_string"].(string)
		if strings.TrimSpace(oldS) == "" {
			return map[string]any{"error": "old_string is required for patch"}, nil
		}
	}

	// Params-only injection scan (no disk). Schema validate of composed markdown runs under lease.
	if err := skillManageScanParams(action, params); err != nil {
		return map[string]any{"error": err.Error()}, nil
	}

	if isSkillManageWriteAction(action) {
		pinned, err := growth.IsSkillPinned(workspace, name)
		if err != nil {
			return map[string]any{"error": err.Error()}, nil
		}
		if pinned {
			return map[string]any{
				"error": "skill_pinned",
				"hint":  "unpin via curator before modifying this skill",
				"name":  name,
			}, nil
		}
	}

	holder := skillManageHolderID(ctx)
	if isSkillManageWriteAction(action) {
		ok, retryAfter := lease.TryAcquire(workspace, holder, 30*time.Second)
		if !ok {
			return map[string]any{
				"error":           "workspace_busy",
				"retry_after_sec": retryAfter,
			}, nil
		}
		defer lease.Release(workspace, holder)
	}

	// Single ToPatches under lease: validate and apply the same batch (avoids TOCTOU).
	batch, err := skillManageToPatches(workspace, action, name, params)
	if err != nil {
		return map[string]any{"error": err.Error()}, nil
	}

	warnings, errMap := validateSkillManageBatch(action, name, batch)
	if errMap != nil {
		return errMap, nil
	}

	if action == "delete" {
		if err := removeSkillDir(workspace, name); err != nil {
			return map[string]any{"error": err.Error()}, nil
		}
	} else if len(batch) > 0 {
		if err := growth.ApplyPatchBatch(workspace, batch); err != nil {
			return map[string]any{"error": err.Error()}, nil
		}
	} else if action != "delete" {
		return map[string]any{"error": "no changes applied"}, nil
	}

	growth.DefaultSkillsIndexTracker.Bump(workspace)
	path := skillSkillMDRel(name)
	if action == "delete" {
		path = filepath.ToSlash(filepath.Join("skills", name))
	}
	return map[string]any{
		"status":   "ok",
		"action":   action,
		"name":     name,
		"path":     path,
		"warnings": warnings,
	}, nil
}

func skillSchemaErrorResult(err error) map[string]any {
	return map[string]any{
		"error":      err.Error(),
		"error_code": skills.ErrCodeSkillSchemaInvalid,
		"hint":       skills.SkillSchemaHint,
		"example":    skills.SkillSchemaExample,
	}
}

// skillManageMarkdownForValidate returns the SKILL.md content that would be written (propose path).
// need=false means schema validation is not applicable for this action/path.
// Propose may call ToPatches here; apply must use validateSkillManageBatch on a single leased batch instead.
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
		return skillManageMarkdownFromBatch(action, batch)
	case "write_file":
		filePath, _ := params["file_path"].(string)
		if !strings.EqualFold(filepath.Base(filePath), "SKILL.md") {
			return "", false, nil
		}
		fc, _ := params["file_content"].(string)
		return fc, true, nil
	default:
		return "", false, nil
	}
}

// skillManageMarkdownFromBatch extracts SKILL.md bytes from an already-built patch batch
// (create Content / patch|edit New / write_file Content|New when basename is SKILL.md).
func skillManageMarkdownFromBatch(action string, batch []growth.Patch) (content string, need bool, err error) {
	switch action {
	case "create":
		if len(batch) == 0 {
			return "", true, fmt.Errorf("no patch content to validate")
		}
		return batch[0].Content, true, nil
	case "edit", "patch":
		if len(batch) == 0 {
			return "", true, fmt.Errorf("no patch content to validate")
		}
		return batch[0].New, true, nil
	case "write_file":
		if len(batch) == 0 {
			return "", false, nil
		}
		p := batch[0]
		if !strings.EqualFold(filepath.Base(p.Path), "SKILL.md") {
			return "", false, nil
		}
		if p.Op == growth.OpCreate {
			return p.Content, true, nil
		}
		return p.New, true, nil
	default:
		return "", false, nil
	}
}

func validateSkillMarkdownContent(name, content string) ([]skills.SkillWarning, map[string]any) {
	meta, body, err := skills.ValidateSkillMarkdown(content, name)
	if err != nil {
		return nil, skillSchemaErrorResult(err)
	}
	warnings := skills.AssessSkillQuality(meta, body)
	if warnings == nil {
		warnings = []skills.SkillWarning{}
	}
	return warnings, nil
}

// validateSkillManageContent is for the propose path (pre-lease; content is not applied until confirm).
func validateSkillManageContent(workspace, action, name string, params map[string]any) ([]skills.SkillWarning, map[string]any) {
	content, need, err := skillManageMarkdownForValidate(workspace, action, name, params)
	if err != nil {
		return nil, map[string]any{"error": err.Error()}
	}
	if !need {
		return []skills.SkillWarning{}, nil
	}
	return validateSkillMarkdownContent(name, content)
}

// validateSkillManageBatch validates SKILL.md from the same batch that ApplyPatchBatch will write.
func validateSkillManageBatch(action, name string, batch []growth.Patch) ([]skills.SkillWarning, map[string]any) {
	content, need, err := skillManageMarkdownFromBatch(action, batch)
	if err != nil {
		return nil, map[string]any{"error": err.Error()}
	}
	if !need {
		return []skills.SkillWarning{}, nil
	}
	return validateSkillMarkdownContent(name, content)
}

func skillManageScanParams(action string, params map[string]any) error {
	switch action {
	case "create", "edit":
		content, _ := params["content"].(string)
		return growth.ScanUserContent(content)
	case "patch":
		newS, _ := params["new_string"].(string)
		return growth.ScanUserContent(newS)
	case "write_file":
		fc, _ := params["file_content"].(string)
		return growth.ScanUserContent(fc)
	default:
		return nil
	}
}

func skillManageHolderID(ctx context.Context) string {
	if sid, ok := ctx.Value(tool.ContextKeySessionID).(string); ok && sid != "" {
		return "skill_manage:" + sid
	}
	return "skill_manage:anonymous"
}

func isSkillManageWriteAction(action string) bool {
	switch action {
	case "create", "patch", "edit", "delete", "write_file", "remove_file":
		return true
	default:
		return false
	}
}

func skillSkillMDRel(name string) string {
	return filepath.ToSlash(filepath.Join("skills", name, "SKILL.md"))
}

func skillFileRel(name, filePath string) (string, error) {
	filePath = filepath.ToSlash(strings.TrimPrefix(filePath, "/"))
	if filePath == "" {
		return "", fmt.Errorf("file_path is required")
	}
	if strings.Contains(filePath, "..") {
		return "", fmt.Errorf("file_path must not contain ..")
	}
	return filepath.ToSlash(filepath.Join("skills", name, filePath)), nil
}

func skillManageToPatches(workspace string, action, name string, params map[string]any) ([]growth.Patch, error) {
	switch action {
	case "create":
		content, _ := params["content"].(string)
		if strings.TrimSpace(content) == "" {
			return nil, fmt.Errorf("content is required for create")
		}
		return []growth.Patch{{
			Path:    skillSkillMDRel(name),
			Op:      growth.OpCreate,
			Content: content,
		}}, nil
	case "edit":
		content, _ := params["content"].(string)
		if strings.TrimSpace(content) == "" {
			return nil, fmt.Errorf("content is required for edit")
		}
		path := skillSkillMDRel(name)
		full, err := tool.ResolveWorkspacePath(workspace, path)
		if err != nil {
			return nil, err
		}
		if _, err := os.Stat(full); os.IsNotExist(err) {
			return nil, fmt.Errorf("skill not found: %s", name)
		}
		prev, err := os.ReadFile(full)
		if err != nil {
			return nil, err
		}
		return []growth.Patch{{
			Path: path,
			Op:   growth.OpPatch,
			Old:  string(prev),
			New:  content,
		}}, nil
	case "patch":
		oldS, _ := params["old_string"].(string)
		newS, _ := params["new_string"].(string)
		replaceAll, _ := params["replace_all"].(bool)
		if strings.TrimSpace(oldS) == "" {
			return nil, fmt.Errorf("old_string is required for patch")
		}
		path := skillSkillMDRel(name)
		full, err := tool.ResolveWorkspacePath(workspace, path)
		if err != nil {
			return nil, err
		}
		prev, err := os.ReadFile(full)
		if err != nil {
			return nil, fmt.Errorf("skill not found: %s", name)
		}
		s := string(prev)
		count := strings.Count(s, oldS)
		if count == 0 {
			return nil, fmt.Errorf("old_string not found in SKILL.md")
		}
		if !replaceAll && count != 1 {
			return nil, fmt.Errorf("old_string is ambiguous (%d matches); set replace_all or use a unique old_string", count)
		}
		var out string
		if replaceAll {
			out = strings.ReplaceAll(s, oldS, newS)
		} else {
			out = strings.Replace(s, oldS, newS, 1)
		}
		return []growth.Patch{{
			Path: path,
			Op:   growth.OpPatch,
			Old:  s,
			New:  out,
		}}, nil
	case "write_file":
		filePath, _ := params["file_path"].(string)
		fileContent, _ := params["file_content"].(string)
		rel, err := skillFileRel(name, filePath)
		if err != nil {
			return nil, err
		}
		full, err := tool.ResolveWorkspacePath(workspace, rel)
		if err != nil {
			return nil, err
		}
		if _, err := os.Stat(full); os.IsNotExist(err) {
			return []growth.Patch{{
				Path:    rel,
				Op:      growth.OpCreate,
				Content: fileContent,
			}}, nil
		}
		prev, err := os.ReadFile(full)
		if err != nil {
			return nil, err
		}
		return []growth.Patch{{
			Path: rel,
			Op:   growth.OpPatch,
			Old:  string(prev),
			New:  fileContent,
		}}, nil
	case "remove_file":
		filePath, _ := params["file_path"].(string)
		rel, err := skillFileRel(name, filePath)
		if err != nil {
			return nil, err
		}
		return []growth.Patch{{
			Path: rel,
			Op:   growth.OpDelete,
		}}, nil
	case "delete":
		return nil, nil
	default:
		return nil, fmt.Errorf("unsupported action %q", action)
	}
}

func removeSkillDir(workspace, name string) error {
	rel := filepath.ToSlash(filepath.Join("skills", name))
	full, err := tool.ResolveWorkspacePath(workspace, rel)
	if err != nil {
		return err
	}
	return os.RemoveAll(full)
}
