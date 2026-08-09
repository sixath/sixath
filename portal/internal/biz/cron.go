package biz

import (
	"context"
	"time"
)

// CronTaskCreate 创建定时任务参数
type CronTaskCreate struct {
	Name               string
	AgentID            string
	ScheduleKind       string // cron, every, at
	ScheduleExpr       string
	Timezone           string
	StaggerSec         int
	PayloadKind        string // agent_turn, skill_execute
	PayloadContent     string
	TimeoutSec         int
	RetryCount         int
	RetryIntervalSec   int
	DeliveryMode       string // none, webhook, session, channel
	DeliveryWebhookURL string
	DeliverySecret     string
	DeliveryBestEffort bool
	DeliverySessionID  string
	DeliveryChannelID  string // 投递到渠道（wxpusher 等）
	Enabled            bool
}

// CronTaskMeta 定时任务元数据
type CronTaskMeta struct {
	ID                 string
	Name               string
	AgentID            string
	ScheduleKind       string
	ScheduleExpr       string
	Timezone           string
	StaggerSec         int
	PayloadKind        string
	PayloadContent     string
	TimeoutSec         int
	RetryCount         int
	RetryIntervalSec   int
	DeliveryMode       string
	DeliveryWebhookURL string
	DeliverySecret     string
	DeliveryBestEffort bool
	DeliverySessionID  string
	DeliveryChannelID  string
	Enabled            bool
	NextRunAt          *time.Time
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

// CronRunMeta 执行历史元数据
type CronRunMeta struct {
	ID            string
	TaskID        string
	TriggeredAt   time.Time
	StartedAt     time.Time
	FinishedAt    *time.Time
	Status        string // success, failed, timeout, cancelled
	OutputSummary string
	Error         string
	DeliveryOK    *bool
}

// CronTaskRepo 定时任务存储接口
type CronTaskRepo interface {
	Create(ctx context.Context, t *CronTaskCreate) (*CronTaskMeta, error)
	GetByID(ctx context.Context, id string) (*CronTaskMeta, error)
	List(ctx context.Context, page, pageSize int32, agentID string, enabled *bool) ([]*CronTaskMeta, int, error)
	Update(ctx context.Context, id string, updates map[string]any) (*CronTaskMeta, error)
	Delete(ctx context.Context, id string) error
	ListDue(ctx context.Context, before time.Time) ([]*CronTaskMeta, error)
	UpdateNextRun(ctx context.Context, id string, nextRunAt time.Time) error
	// ListSkillExecuteByAgentIDs 返回指定 agent 的 skill_execute 任务（R2c cron 反写）。
	ListSkillExecuteByAgentIDs(ctx context.Context, agentIDs []string) ([]*CronTaskMeta, error)
}

// CronRunRepo 执行历史存储接口
type CronRunRepo interface {
	Create(ctx context.Context, run *CronRunMeta) error
	Update(ctx context.Context, id string, updates map[string]any) error
	ListByTask(ctx context.Context, taskID string, page, pageSize int32) ([]*CronRunMeta, int, error)
}
