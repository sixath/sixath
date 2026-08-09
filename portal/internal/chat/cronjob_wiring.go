package chat

import (
	"context"

	"backend/internal/biz"

	"github.com/sixath/framework/tool"
)

var cronClient tool.CronClient

// SetCronClient injects the Portal CronClient used by RegisterCronjobTools.
func SetCronClient(c tool.CronClient) {
	cronClient = c
}

// RegisterCronjobTools registers the cronjob agent tool when enabled.
func RegisterCronjobTools(reg *tool.Registry) error {
	return RegisterCronjobToolsWithEnabled(reg, tool.CronjobToolEnabled || tool.CronjobEnabledFromEnv())
}

// RegisterCronjobToolsWithEnabled registers cronjob with an explicit enabled flag.
func RegisterCronjobToolsWithEnabled(reg *tool.Registry, enabled bool) error {
	if reg == nil || !enabled {
		return nil
	}
	return tool.RegisterCronjobTool(reg, cronClient, &tool.CronjobToolConfig{Enabled: true})
}

// portalCronClient implements tool.CronClient against biz + optional run trigger.
type portalCronClient struct {
	uc      *biz.CronUsecase
	runTask func(context.Context, *biz.CronTaskMeta)
}

// NewPortalCronClient creates a CronClient backed by Portal cron usecase.
func NewPortalCronClient(uc *biz.CronUsecase, runTask func(context.Context, *biz.CronTaskMeta)) tool.CronClient {
	return &portalCronClient{uc: uc, runTask: runTask}
}

func (c *portalCronClient) Create(ctx context.Context, agentID string, in tool.CronJobCreateInput) (tool.CronJobSummary, error) {
	timeoutSec := in.TimeoutSec
	if timeoutSec <= 0 {
		timeoutSec = 300
	}
	deliveryMode := in.DeliveryMode
	if deliveryMode == "" {
		deliveryMode = "none"
	}
	tz := in.Timezone
	if tz == "" {
		tz = "UTC"
	}
	meta, err := c.uc.Create(ctx, &biz.CronTaskCreate{
		Name:               in.Name,
		AgentID:            agentID,
		ScheduleKind:       in.ScheduleKind,
		ScheduleExpr:       in.ScheduleExpr,
		Timezone:           tz,
		PayloadKind:        in.PayloadKind,
		PayloadContent:     in.PayloadContent,
		TimeoutSec:         timeoutSec,
		DeliveryMode:       deliveryMode,
		DeliveryWebhookURL: in.DeliveryWebhookURL,
		DeliverySessionID:  in.DeliverySessionID,
		DeliveryChannelID:  in.DeliveryChannelID,
		Enabled:            in.Enabled,
	})
	if err != nil {
		return tool.CronJobSummary{}, err
	}
	return cronMetaToSummary(meta), nil
}

func (c *portalCronClient) List(ctx context.Context, agentID string, page, pageSize int, enabled *bool) ([]tool.CronJobSummary, int, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 20
	}
	list, total, err := c.uc.List(ctx, int32(page), int32(pageSize), agentID, enabled)
	if err != nil {
		return nil, 0, err
	}
	out := make([]tool.CronJobSummary, len(list))
	for i, t := range list {
		out[i] = cronMetaToSummary(t)
	}
	return out, total, nil
}

func (c *portalCronClient) Update(ctx context.Context, taskID string, updates map[string]any) (tool.CronJobSummary, error) {
	meta, err := c.uc.Update(ctx, taskID, updates)
	if err != nil {
		return tool.CronJobSummary{}, err
	}
	return cronMetaToSummary(meta), nil
}

func (c *portalCronClient) Delete(ctx context.Context, taskID string) error {
	return c.uc.Delete(ctx, taskID)
}

func (c *portalCronClient) RunAdHoc(ctx context.Context, taskID string) error {
	task, err := c.uc.Get(ctx, taskID)
	if err != nil {
		return err
	}
	if c.runTask == nil {
		return nil
	}
	go c.runTask(context.Background(), task)
	return nil
}

func cronMetaToSummary(t *biz.CronTaskMeta) tool.CronJobSummary {
	if t == nil {
		return tool.CronJobSummary{}
	}
	s := tool.CronJobSummary{
		ID:             t.ID,
		Name:           t.Name,
		AgentID:        t.AgentID,
		ScheduleKind:   t.ScheduleKind,
		ScheduleExpr:   t.ScheduleExpr,
		Timezone:       t.Timezone,
		PayloadKind:    t.PayloadKind,
		PayloadContent: t.PayloadContent,
		DeliveryMode:   t.DeliveryMode,
		Enabled:        t.Enabled,
	}
	if t.NextRunAt != nil {
		s.NextRunAt = t.NextRunAt.Format("2006-01-02T15:04:05Z07:00")
	}
	return s
}

// WithCronSessionContext attaches cron execution session flags to context (spec §5.7.1).
func WithCronSessionContext(ctx context.Context) context.Context {
	ctx = context.WithValue(ctx, tool.ContextKeyRunKind, "cron")
	ctx = context.WithValue(ctx, tool.ContextKeyAllowCronCreate, false)
	ctx = context.WithValue(ctx, tool.ContextKeySkipMemory, true)
	ctx = context.WithValue(ctx, tool.ContextKeySkipGrowthReview, true)
	return ctx
}

// CronSessionMetadata returns agent.Request metadata for cron execution sessions.
func CronSessionMetadata(agentID, workspace string) map[string]any {
	m := map[string]any{
		MetaRunKind:          "cron",
		MetaAllowCronCreate:  false,
		MetaSkipMemory:       true,
		MetaSkipGrowthReview: true,
		"agent_id":           agentID,
	}
	if workspace != "" {
		m["workspace_root"] = workspace
	}
	return m
}

// RequestMetadataFromContext builds agent.Request metadata from context session flags.
func RequestMetadataFromContext(ctx context.Context) map[string]any {
	if ctx == nil {
		return nil
	}
	var md map[string]any
	if rk, ok := ctx.Value(tool.ContextKeyRunKind).(string); ok && rk != "" {
		md = ensureMetadata(md)
		md[MetaRunKind] = rk
	}
	if v := ctx.Value(tool.ContextKeyAllowCronCreate); v != nil {
		md = ensureMetadata(md)
		md[MetaAllowCronCreate] = v
	}
	if v := ctx.Value(tool.ContextKeySkipMemory); v != nil {
		md = ensureMetadata(md)
		md[MetaSkipMemory] = v
	}
	if v := ctx.Value(tool.ContextKeySkipGrowthReview); v != nil {
		md = ensureMetadata(md)
		md[MetaSkipGrowthReview] = v
	}
	if agentID, ok := ctx.Value(tool.ContextKeyAgentID).(string); ok && agentID != "" {
		md = ensureMetadata(md)
		md["agent_id"] = agentID
	}
	if ws, ok := ctx.Value(tool.ContextKeyWorkspaceRoot).(string); ok && ws != "" {
		md = ensureMetadata(md)
		md["workspace_root"] = ws
	}
	return md
}

func ensureMetadata(md map[string]any) map[string]any {
	if md != nil {
		return md
	}
	return make(map[string]any)
}

// Metadata keys re-exported for portal service layers.
const (
	MetaRunKind          = "run_kind"
	MetaAllowCronCreate  = "allow_cron_create"
	MetaSkipMemory       = "skip_memory"
	MetaSkipGrowthReview = "skip_growth_review"
)
