package biz

import (
	"context"
	"strconv"
	"time"

	"github.com/robfig/cron/v3"
)

// CronNextRun is the first run of a schedule after after: the next match of a five-field cron
// expression, after plus the interval in seconds for "every" (default 3600), and the RFC 3339
// time itself for "at". Times are interpreted in timezone (UTC when empty or unknown).
func CronNextRun(kind, expr, timezone string, after time.Time) (*time.Time, error) {
	loc := time.UTC
	if timezone != "" {
		if l, err := time.LoadLocation(timezone); err == nil {
			loc = l
		}
	}
	now := after.In(loc)
	switch kind {
	case "cron":
		if expr == "" {
			return nil, nil
		}
		sched, err := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow).Parse(expr)
		if err != nil {
			return nil, err
		}
		next := sched.Next(now)
		return &next, nil
	case "every":
		sec := 3600
		if n, err := strconv.Atoi(expr); err == nil && n > 0 {
			sec = n
		}
		next := now.Add(time.Duration(sec) * time.Second)
		return &next, nil
	case "at":
		t, err := time.ParseInLocation(time.RFC3339, expr, loc)
		if err != nil {
			return nil, err
		}
		return &t, nil
	}
	return nil, nil
}

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

// Cron run statuses.
const (
	CronRunRunning = "running"
	CronRunSuccess = "success"
	CronRunFailed  = "failed"
	// CronRunMissed records a scheduled time that passed while the backend was not running.
	CronRunMissed = "missed"
	// CronRunInterrupted records a run that never reported a result, e.g. the backend
	// restarted mid-run.
	CronRunInterrupted = "interrupted"
)

// Cron run triggers.
const (
	CronTriggerSchedule = "schedule"
	CronTriggerManual   = "manual"
)

// CronRunMeta 执行历史元数据
type CronRunMeta struct {
	ID            string
	TaskID        string
	Trigger       string // schedule, manual; "" for runs recorded before the field existed
	TriggeredAt   time.Time
	StartedAt     time.Time
	FinishedAt    *time.Time
	Status        string // running, success, failed, missed, interrupted
	OutputSummary string
	Error         string
	DeliveryOK    *bool
	DeliveryError string
}

// CronTaskRepo 定时任务存储接口
type CronTaskRepo interface {
	Create(ctx context.Context, t *CronTaskCreate) (*CronTaskMeta, error)
	GetByID(ctx context.Context, id string) (*CronTaskMeta, error)
	List(ctx context.Context, page, pageSize int32, agentID string, enabled *bool) ([]*CronTaskMeta, int, error)
	Update(ctx context.Context, id string, updates map[string]any) (*CronTaskMeta, error)
	Delete(ctx context.Context, id string) error
	ListDue(ctx context.Context, before time.Time) ([]*CronTaskMeta, error)
	// ClaimDue moves next_run_at of an enabled task that is due at now to next (nil clears it)
	// and reports whether this call did so; only the claiming caller may run the slot.
	ClaimDue(ctx context.Context, id string, now time.Time, next *time.Time) (bool, error)
	// ListSkillExecuteByAgentIDs 返回指定 agent 的 skill_execute 任务（R2c cron 反写）。
	ListSkillExecuteByAgentIDs(ctx context.Context, agentIDs []string) ([]*CronTaskMeta, error)
}

// CronRunRepo 执行历史存储接口
type CronRunRepo interface {
	Create(ctx context.Context, run *CronRunMeta) error
	Update(ctx context.Context, id string, updates map[string]any) error
	ListByTask(ctx context.Context, taskID string, page, pageSize int32) ([]*CronRunMeta, int, error)
	// ListRunning returns runs still marked running that started before startedBefore.
	ListRunning(ctx context.Context, startedBefore time.Time) ([]*CronRunMeta, error)
}
