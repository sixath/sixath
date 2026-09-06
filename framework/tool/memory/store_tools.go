package toolmem

import (
	"context"
	"errors"
	"path"
	"path/filepath"
	"strings"

	"github.com/sixath/framework/memory"
	"github.com/sixath/framework/tool"
	fwws "github.com/sixath/framework/workspace"
)

// StoreToolsOptions configures MemoryStore-backed tools.
type StoreToolsOptions struct {
	AgentWriteEnabled bool
	ExtraPaths        []string
}

// RegisterMemoryStoreTools registers the scoped memory_remember, memory_recall,
// and memory_get tools.
func RegisterMemoryStoreTools(reg *tool.Registry, store memory.MemoryStore, opts StoreToolsOptions) error {
	if reg == nil {
		return errors.New("memory store tools: registry is nil")
	}
	if store == nil {
		return errors.New("memory store tools: store is nil")
	}

	if err := reg.Register(memoryRememberTool(store, opts)); err != nil {
		return err
	}
	if err := reg.Register(memoryRecallTool(store)); err != nil {
		return err
	}
	return reg.Register(memoryGetTool(store, opts.ExtraPaths))
}

func memoryRememberTool(store memory.MemoryStore, opts StoreToolsOptions) tool.Tool {
	return tool.Tool{
		Name:        "memory_remember",
		Description: "Store a session fact, user-scoped memory, or durable agent memory. For scope=session|user, replace creates a new unit id; the old unit becomes superseded.",
		Toolset:     tool.ToolsetMemory,
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"scope":    enumSchema([]string{"user", "session", "agent"}),
				"action":   enumSchema([]string{"add", "replace", "remove"}),
				"content":  stringSchema("Content for add or replace."),
				"old_text": stringSchema("Existing text for agent replace or remove."),
				"unit_id":  stringSchema("Session memory unit ID for replace or remove."),
				"target":   enumSchema([]string{"memory", "user_file"}),
			},
			"required": []string{"scope", "action"},
		},
		Execute: func(ctx context.Context, params map[string]any) (any, error) {
			scope := memory.Scope(stringParam(params, "scope"))
			if scope != memory.ScopeSession && scope != memory.ScopeAgent && scope != memory.ScopeUser {
				return errorResult("invalid_scope"), nil
			}
			action := memory.RememberAction(stringParam(params, "action"))
			if action != memory.ActionAdd && action != memory.ActionReplace && action != memory.ActionRemove {
				return errorResult("invalid_action"), nil
			}

			in := memory.RememberInput{
				Scope:   scope,
				Action:  action,
				Content: stringParam(params, "content"),
				OldText: stringParam(params, "old_text"),
				UnitID:  stringParam(params, "unit_id"),
				Target:  stringParam(params, "target"),
				AgentID: contextString(ctx, tool.ContextKeyAgentID),
			}
			switch scope {
			case memory.ScopeUser:
				in.ScopeID = contextString(ctx, tool.ContextKeyUserID)
				if in.ScopeID == "" {
					return skippedUserIDResult(), nil
				}
				if sessionID := contextString(ctx, tool.ContextKeySessionID); sessionID != "" {
					in.Metadata = map[string]any{"source_session_id": sessionID}
				}
			case memory.ScopeSession:
				in.ScopeID = contextString(ctx, tool.ContextKeySessionID)
				if in.ScopeID == "" {
					return errorResult("session_id_missing"), nil
				}
			case memory.ScopeAgent:
				if !opts.AgentWriteEnabled {
					return map[string]any{"error": "agent_write_disabled", "disabled": true}, nil
				}
				in.WorkspaceRoot = contextString(ctx, tool.ContextKeyWorkspaceRoot)
				if in.WorkspaceRoot == "" {
					return errorResult("workspace_root_missing"), nil
				}
				if in.Target == "" {
					in.Target = "memory"
				}
				if in.Target != "memory" && in.Target != "user_file" {
					return errorResult("invalid_target"), nil
				}
			}

			hit, err := store.Remember(ctx, in)
			if err != nil {
				return storeErrorResult(err), nil
			}
			return memoryHitResult(hit), nil
		},
	}
}

func memoryRecallTool(store memory.MemoryStore) tool.Tool {
	return tool.Tool{
		Name: "memory_recall",
		Description: "Recall user or session units, past transcript results, or agent memory files. " +
			"For source=transcript: omit or empty query lists recent sessions; non-empty query returns anchored windows. " +
			"For units/files: query is required (empty query is rejected).",
		Toolset: tool.ToolsetMemory,
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"scope":           enumSchema([]string{"user", "session", "agent"}),
				"query":           stringSchema("Memory search query. Optional for source=transcript (empty lists recent sessions)."),
				"source":          enumSchema([]string{"units", "transcript", "files"}),
				"limit":           map[string]any{"type": "integer", "minimum": 1},
				"min_score":       map[string]any{"type": "number", "minimum": 0},
				"anchor_window":   map[string]any{"type": "integer", "minimum": 0, "description": "±N messages around transcript FTS hits (default manager window)."},
				"include_tools":   map[string]any{"type": "boolean", "description": "Include tool-role messages in transcript FTS (default true)."},
				"exclude_current": map[string]any{"type": "boolean", "description": "Exclude the current session from transcript results (default true)."},
			},
			"required": []string{"scope"},
		},
		Execute: func(ctx context.Context, params map[string]any) (any, error) {
			scope := memory.Scope(stringParam(params, "scope"))
			if scope != memory.ScopeSession && scope != memory.ScopeAgent && scope != memory.ScopeUser {
				return errorResult("invalid_scope"), nil
			}
			q := memory.RecallQuery{
				Scope:         scope,
				Query:         stringParam(params, "query"),
				Source:        memory.RecallSource(stringParam(params, "source")),
				Limit:         intParam(params, "limit"),
				MinScore:      floatParam(params, "min_score"),
				AnchorWindow:  intParam(params, "anchor_window"),
				AgentID:       contextString(ctx, tool.ContextKeyAgentID),
				SessionID:     contextString(ctx, tool.ContextKeySessionID),
				WorkspaceRoot: contextString(ctx, tool.ContextKeyWorkspaceRoot),
			}
			if v, ok := boolParam(params, "include_tools"); ok {
				q.IncludeTools = &v
			}
			if v, ok := boolParam(params, "exclude_current"); ok {
				q.ExcludeCurrent = &v
			}
			if strings.TrimSpace(q.Query) == "" && q.Source != memory.SourceTranscript {
				return errorResult("empty_query_rejected"), nil
			}
			switch scope {
			case memory.ScopeUser:
				q.ScopeID = contextString(ctx, tool.ContextKeyUserID)
				if q.ScopeID == "" {
					return map[string]any{"hits": []map[string]any{}}, nil
				}
			case memory.ScopeSession:
				q.ScopeID = contextString(ctx, tool.ContextKeySessionID)
				if q.ScopeID == "" {
					return errorResult("session_id_missing"), nil
				}
			case memory.ScopeAgent:
				if q.WorkspaceRoot == "" {
					return errorResult("workspace_root_missing"), nil
				}
			}
			hits, err := store.Recall(ctx, q)
			if err != nil {
				return storeErrorResult(err), nil
			}
			if q.Source == memory.SourceTranscript {
				return transcriptRecallResult(hits, strings.TrimSpace(q.Query) == ""), nil
			}
			out := make([]map[string]any, 0, len(hits))
			for _, hit := range hits {
				out = append(out, memoryHitResult(hit))
			}
			return map[string]any{"hits": out}, nil
		},
	}
}

func transcriptRecallResult(hits []memory.MemoryHit, listRecent bool) map[string]any {
	if listRecent {
		sessions := make([]map[string]any, 0, len(hits))
		for _, hit := range hits {
			meta := hit.Metadata
			if meta == nil {
				meta = map[string]any{}
			}
			sessionID, _ := meta["session_id"].(string)
			if sessionID == "" {
				sessionID = hit.ID
			}
			entry := map[string]any{
				"session_id": sessionID,
				"title":      meta["title"],
				"preview":    hit.Content,
			}
			if root, ok := meta["root_session_id"]; ok {
				entry["root_session_id"] = root
			}
			if updated, ok := meta["updated_at"]; ok {
				entry["updated_at"] = updated
			}
			sessions = append(sessions, entry)
		}
		return map[string]any{"sessions": sessions, "count": len(sessions)}
	}

	out := make([]map[string]any, 0, len(hits))
	for _, hit := range hits {
		meta := hit.Metadata
		if meta == nil {
			meta = map[string]any{}
		}
		sessionID, _ := meta["session_id"].(string)
		if sessionID == "" {
			sessionID = hit.ID
		}
		entry := map[string]any{
			"session_id":    sessionID,
			"title":         meta["title"],
			"anchor":        meta["anchor"],
			"window":        meta["window"],
			"bookend_start": meta["bookend_start"],
			"bookend_end":   meta["bookend_end"],
		}
		if root, ok := meta["root_session_id"]; ok {
			entry["root_session_id"] = root
		}
		if hit.Score != 0 {
			entry["score"] = hit.Score
		}
		out = append(out, entry)
	}
	return map[string]any{"hits": out, "count": len(out)}
}

func boolParam(params map[string]any, key string) (bool, bool) {
	value, ok := params[key]
	if !ok || value == nil {
		return false, false
	}
	switch v := value.(type) {
	case bool:
		return v, true
	default:
		return false, false
	}
}

func memoryGetTool(store memory.MemoryStore, extraPaths []string) tool.Tool {
	return tool.Tool{
		Name:        "memory_get",
		Description: "Get a user or session memory unit or an allowed agent memory file.",
		Toolset:     tool.ToolsetMemory,
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"scope": enumSchema([]string{"user", "session", "agent"}),
				"id":    stringSchema("Session memory unit ID."),
				"path":  stringSchema("Allowed agent memory path."),
			},
			"required": []string{"scope"},
		},
		Execute: func(ctx context.Context, params map[string]any) (any, error) {
			scope := memory.Scope(stringParam(params, "scope"))
			if scope != memory.ScopeSession && scope != memory.ScopeAgent && scope != memory.ScopeUser {
				return errorResult("invalid_scope"), nil
			}
			ref := memory.GetRef{
				Scope:         scope,
				ID:            stringParam(params, "id"),
				Path:          stringParam(params, "path"),
				AgentID:       contextString(ctx, tool.ContextKeyAgentID),
				WorkspaceRoot: contextString(ctx, tool.ContextKeyWorkspaceRoot),
			}
			switch scope {
			case memory.ScopeUser:
				ref.ScopeID = contextString(ctx, tool.ContextKeyUserID)
				if ref.ScopeID == "" {
					return errorResult("not_found"), nil
				}
				if ref.ID == "" {
					return errorResult("id_required"), nil
				}
			case memory.ScopeSession:
				ref.ScopeID = contextString(ctx, tool.ContextKeySessionID)
				if ref.ScopeID == "" {
					return errorResult("session_id_missing"), nil
				}
				if ref.ID == "" {
					return errorResult("id_required"), nil
				}
			default:
				if ref.WorkspaceRoot == "" {
					return errorResult("workspace_root_missing"), nil
				}
				if !allowedMemoryPath(ref.WorkspaceRoot, ref.Path, extraPaths) {
					return errorResult("path_not_allowed"), nil
				}
			}
			hit, err := store.Get(ctx, ref)
			if err != nil {
				return storeErrorResult(err), nil
			}
			return memoryHitResult(hit), nil
		},
	}
}

func allowedMemoryPath(workspaceRoot, rel string, extraPaths []string) bool {
	if rel == "" || filepath.IsAbs(rel) {
		return false
	}
	if _, err := fwws.ResolveWorkspacePath(workspaceRoot, rel); err != nil {
		return false
	}
	clean := path.Clean(filepath.ToSlash(rel))
	if clean == "." || strings.HasPrefix(clean, "../") {
		return false
	}
	if clean == "MEMORY.md" || clean == "USER.md" || (strings.HasPrefix(clean, "memory/") && strings.HasSuffix(clean, ".md")) {
		return true
	}
	for _, allowed := range extraPaths {
		allowed = path.Clean(filepath.ToSlash(strings.TrimSpace(allowed)))
		if allowed == clean {
			return true
		}
		if matched, err := path.Match(allowed, clean); err == nil && matched {
			return true
		}
	}
	return false
}

func enumSchema(values []string) map[string]any {
	return map[string]any{"type": "string", "enum": values}
}

func stringSchema(description string) map[string]any {
	return map[string]any{"type": "string", "description": description}
}

func contextString(ctx context.Context, key string) string {
	value, _ := ctx.Value(key).(string)
	return strings.TrimSpace(value)
}

func stringParam(params map[string]any, key string) string {
	value, _ := params[key].(string)
	return strings.TrimSpace(value)
}

func intParam(params map[string]any, key string) int {
	switch value := params[key].(type) {
	case int:
		return value
	case float64:
		return int(value)
	default:
		return 0
	}
}

func floatParam(params map[string]any, key string) float64 {
	switch value := params[key].(type) {
	case float64:
		return value
	case float32:
		return float64(value)
	case int:
		return float64(value)
	default:
		return 0
	}
}

func scopeNotEnabledResult() map[string]any {
	return errorResultFor(memory.ErrScopeNotEnabled, "scope_not_enabled")
}

func skippedUserIDResult() map[string]any {
	return map[string]any{"skipped": true, "reason": "user_id_missing"}
}

func storeErrorResult(err error) map[string]any {
	if errors.Is(err, memory.ErrScopeNotEnabled) {
		return scopeNotEnabledResult()
	}
	if errors.Is(err, memory.ErrEmptyQueryRejected) {
		return errorResult("empty_query_rejected")
	}
	return errorResult(err.Error())
}

func errorResultFor(err error, code string) map[string]any {
	return map[string]any{"error": code, "cause": err.Error()}
}

func errorResult(code string) map[string]any {
	return map[string]any{"error": code}
}

func memoryHitResult(hit memory.MemoryHit) map[string]any {
	return map[string]any{
		"scope":    string(hit.Scope),
		"source":   string(hit.Source),
		"id":       hit.ID,
		"path":     hit.Path,
		"content":  hit.Content,
		"score":    hit.Score,
		"metadata": hit.Metadata,
	}
}
