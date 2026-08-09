package tool

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
)

// CronJobCreateInput is the payload for creating a scheduled task.
type CronJobCreateInput struct {
	Name               string
	ScheduleKind       string
	ScheduleExpr       string
	Timezone           string
	PayloadKind        string
	PayloadContent     string
	TimeoutSec         int
	DeliveryMode       string
	DeliveryWebhookURL string
	DeliverySessionID  string
	DeliveryChannelID  string
	Enabled            bool
}

// CronJobSummary is a lightweight cron task view for the agent tool.
type CronJobSummary struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	AgentID        string `json:"agent_id"`
	ScheduleKind   string `json:"schedule_kind"`
	ScheduleExpr   string `json:"schedule_expr"`
	Timezone       string `json:"timezone,omitempty"`
	PayloadKind    string `json:"payload_kind"`
	PayloadContent string `json:"payload_content"`
	DeliveryMode   string `json:"delivery_mode,omitempty"`
	Enabled        bool   `json:"enabled"`
	NextRunAt      string `json:"next_run_at,omitempty"`
}

// CronClient abstracts Portal cron persistence and ad-hoc execution.
type CronClient interface {
	Create(ctx context.Context, agentID string, in CronJobCreateInput) (CronJobSummary, error)
	List(ctx context.Context, agentID string, page, pageSize int, enabled *bool) ([]CronJobSummary, int, error)
	Update(ctx context.Context, taskID string, updates map[string]any) (CronJobSummary, error)
	Delete(ctx context.Context, taskID string) error
	RunAdHoc(ctx context.Context, taskID string) error
}

// CronjobToolConfig configures cronjob tool registration.
type CronjobToolConfig struct {
	Enabled bool
}

// CronjobToolEnabled is the process-wide default for cronjob tool (override via env in Portal).
var CronjobToolEnabled = false

// CronjobEnabledFromEnv reports whether CRONJOB_TOOL_ENABLED is truthy.
func CronjobEnabledFromEnv() bool {
	v := strings.ToLower(strings.TrimSpace(os.Getenv("CRONJOB_TOOL_ENABLED")))
	return v == "1" || v == "true" || v == "yes"
}

// CronCreateAllowed returns false when nested cron creation is forbidden (spec §5.7.1).
func CronCreateAllowed(ctx context.Context) bool {
	if rk, ok := ctx.Value(ContextKeyRunKind).(string); ok && strings.EqualFold(strings.TrimSpace(rk), "cron") {
		return false
	}
	return contextBoolDefaultTrue(ctx, ContextKeyAllowCronCreate)
}

func contextBoolDefaultTrue(ctx context.Context, key string) bool {
	v := ctx.Value(key)
	if v == nil {
		return true
	}
	switch x := v.(type) {
	case bool:
		return x
	case string:
		s := strings.ToLower(strings.TrimSpace(x))
		return s != "0" && s != "false" && s != "no"
	case float64:
		return x != 0
	case int:
		return x != 0
	case int64:
		return x != 0
	default:
		return true
	}
}

// RegisterCronjobTool registers the Hermes-aligned cronjob management tool.
func RegisterCronjobTool(reg *Registry, client CronClient, cfg *CronjobToolConfig) error {
	if reg == nil {
		return errors.New("cronjob: registry is nil")
	}
	enabled := cronjobConfigOrDefault(cfg).Enabled
	checkFn := func(ctx context.Context) error {
		if !enabled {
			return errors.New("cronjob tool is disabled (set CRONJOB_TOOL_ENABLED=true)")
		}
		if client == nil {
			return errors.New("cronjob client not configured")
		}
		return nil
	}
	return reg.Register(Tool{
		Name: "cronjob",
		Description: "Manage scheduled tasks (create, list, update, pause, resume, remove, run). " +
			"IMPORTANT: always call action=list before remove to confirm the task id. " +
			"Schedule kinds: cron (cron expr), every (seconds), at (RFC3339 one-shot). " +
			"Payload kinds: agent_turn (prompt text), skill_execute (skill script path).",
		Toolset: ToolsetCronjob,
		CheckFn: checkFn,
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"action": map[string]any{
					"type":        "string",
					"enum":        []string{"create", "list", "update", "pause", "resume", "remove", "run"},
					"description": "Operation to perform. Use list before remove.",
				},
				"task_id": map[string]any{
					"type":        "string",
					"description": "Task id (required for update, pause, resume, remove, run).",
				},
				"name": map[string]any{
					"type":        "string",
					"description": "Human-readable task name (create).",
				},
				"schedule_kind": map[string]any{
					"type":        "string",
					"enum":        []string{"cron", "every", "at"},
					"description": "Schedule type (create).",
				},
				"schedule_expr": map[string]any{
					"type":        "string",
					"description": "Cron expression, interval seconds, or RFC3339 timestamp (create).",
				},
				"timezone": map[string]any{
					"type":        "string",
					"description": "IANA timezone (create/update, default UTC).",
				},
				"payload_kind": map[string]any{
					"type":        "string",
					"enum":        []string{"agent_turn", "skill_execute"},
					"description": "What to run (create).",
				},
				"payload_content": map[string]any{
					"type":        "string",
					"description": "Agent prompt or skill script path (create).",
				},
				"delivery_mode": map[string]any{
					"type":        "string",
					"enum":        []string{"none", "webhook", "session", "channel"},
					"description": "Where to deliver results (create/update).",
				},
				"delivery_session_id": map[string]any{
					"type":        "string",
					"description": "Session id when delivery_mode=session.",
				},
				"delivery_channel_id": map[string]any{
					"type":        "string",
					"description": "Channel id when delivery_mode=channel.",
				},
				"enabled": map[string]any{
					"type":        "boolean",
					"description": "Whether the task is active (create/update).",
				},
				"page": map[string]any{
					"type":        "integer",
					"description": "List page (default 1).",
				},
				"page_size": map[string]any{
					"type":        "integer",
					"description": "List page size (default 20).",
				},
				"updates": map[string]any{
					"type":        "object",
					"description": "Partial fields for update action.",
				},
			},
			"required": []string{"action"},
		},
		Execute: func(ctx context.Context, params map[string]any) (any, error) {
			action := strings.ToLower(strings.TrimSpace(stringParam(params, "action")))
			if action == "" {
				return map[string]any{"error": "action is required"}, nil
			}
			agentID, _ := ctx.Value(ContextKeyAgentID).(string)
			if agentID == "" {
				return map[string]any{"error": "agent_id missing from context"}, nil
			}
			switch action {
			case "create":
				return cronjobCreate(ctx, client, agentID, params)
			case "list":
				return cronjobList(ctx, client, agentID, params)
			case "update":
				return cronjobUpdate(ctx, client, params)
			case "pause":
				return cronjobSetEnabled(ctx, client, params, false)
			case "resume":
				return cronjobSetEnabled(ctx, client, params, true)
			case "remove":
				return cronjobRemove(ctx, client, params)
			case "run":
				return cronjobRun(ctx, client, params)
			default:
				return map[string]any{"error": fmt.Sprintf("unknown action: %s", action)}, nil
			}
		},
	})
}

func cronjobConfigOrDefault(cfg *CronjobToolConfig) CronjobToolConfig {
	if cfg == nil {
		return CronjobToolConfig{Enabled: CronjobToolEnabled || CronjobEnabledFromEnv()}
	}
	return *cfg
}

func cronjobCreate(ctx context.Context, client CronClient, agentID string, params map[string]any) (any, error) {
	if !CronCreateAllowed(ctx) {
		return map[string]any{"error": "cron_nested_forbidden"}, nil
	}
	name := strings.TrimSpace(stringParam(params, "name"))
	scheduleKind := strings.TrimSpace(stringParam(params, "schedule_kind"))
	scheduleExpr := strings.TrimSpace(stringParam(params, "schedule_expr"))
	payloadKind := strings.TrimSpace(stringParam(params, "payload_kind"))
	payloadContent := strings.TrimSpace(stringParam(params, "payload_content"))
	if name == "" || scheduleKind == "" || scheduleExpr == "" || payloadKind == "" || payloadContent == "" {
		return map[string]any{"error": "name, schedule_kind, schedule_expr, payload_kind, payload_content are required for create"}, nil
	}
	tz := strings.TrimSpace(stringParam(params, "timezone"))
	if tz == "" {
		tz = "UTC"
	}
	deliveryMode := strings.TrimSpace(stringParam(params, "delivery_mode"))
	if deliveryMode == "" {
		deliveryMode = "none"
	}
	enabled := true
	if v, ok := params["enabled"].(bool); ok {
		enabled = v
	}
	task, err := client.Create(ctx, agentID, CronJobCreateInput{
		Name:               name,
		ScheduleKind:       scheduleKind,
		ScheduleExpr:       scheduleExpr,
		Timezone:           tz,
		PayloadKind:        payloadKind,
		PayloadContent:     payloadContent,
		DeliveryMode:       deliveryMode,
		DeliveryWebhookURL: stringParam(params, "delivery_webhook_url"),
		DeliverySessionID:  stringParam(params, "delivery_session_id"),
		DeliveryChannelID:  stringParam(params, "delivery_channel_id"),
		Enabled:            enabled,
	})
	if err != nil {
		return map[string]any{"error": err.Error()}, nil
	}
	return map[string]any{"ok": true, "task": task}, nil
}

func cronjobList(ctx context.Context, client CronClient, agentID string, params map[string]any) (any, error) {
	page := intParam(params, "page", 1)
	pageSize := intParam(params, "page_size", 20)
	var enabled *bool
	if v, ok := params["enabled"].(bool); ok {
		enabled = &v
	}
	items, total, err := client.List(ctx, agentID, page, pageSize, enabled)
	if err != nil {
		return map[string]any{"error": err.Error()}, nil
	}
	return map[string]any{"tasks": items, "total": total, "page": page, "page_size": pageSize}, nil
}

func cronjobUpdate(ctx context.Context, client CronClient, params map[string]any) (any, error) {
	taskID := strings.TrimSpace(stringParam(params, "task_id"))
	if taskID == "" {
		return map[string]any{"error": "task_id is required for update"}, nil
	}
	updates, _ := params["updates"].(map[string]any)
	if len(updates) == 0 {
		return map[string]any{"error": "updates object is required for update"}, nil
	}
	task, err := client.Update(ctx, taskID, updates)
	if err != nil {
		return map[string]any{"error": err.Error()}, nil
	}
	return map[string]any{"ok": true, "task": task}, nil
}

func cronjobSetEnabled(ctx context.Context, client CronClient, params map[string]any, enabled bool) (any, error) {
	taskID := strings.TrimSpace(stringParam(params, "task_id"))
	if taskID == "" {
		return map[string]any{"error": "task_id is required"}, nil
	}
	task, err := client.Update(ctx, taskID, map[string]any{"enabled": enabled})
	if err != nil {
		return map[string]any{"error": err.Error()}, nil
	}
	return map[string]any{"ok": true, "task": task}, nil
}

func cronjobRemove(ctx context.Context, client CronClient, params map[string]any) (any, error) {
	taskID := strings.TrimSpace(stringParam(params, "task_id"))
	if taskID == "" {
		return map[string]any{"error": "task_id is required for remove; list tasks first"}, nil
	}
	if err := client.Delete(ctx, taskID); err != nil {
		return map[string]any{"error": err.Error()}, nil
	}
	return map[string]any{"ok": true, "removed": taskID}, nil
}

func cronjobRun(ctx context.Context, client CronClient, params map[string]any) (any, error) {
	taskID := strings.TrimSpace(stringParam(params, "task_id"))
	if taskID == "" {
		return map[string]any{"error": "task_id is required for run"}, nil
	}
	if err := client.RunAdHoc(ctx, taskID); err != nil {
		return map[string]any{"error": err.Error()}, nil
	}
	return map[string]any{"ok": true, "triggered": taskID}, nil
}

func stringParam(params map[string]any, key string) string {
	v, _ := params[key].(string)
	return v
}

func intParam(params map[string]any, key string, def int) int {
	switch v := params[key].(type) {
	case float64:
		if int(v) > 0 {
			return int(v)
		}
	case int:
		if v > 0 {
			return v
		}
	case int64:
		if v > 0 {
			return int(v)
		}
	}
	return def
}
