package model

import (
	"time"
)

// CronTask 定时任务表
type CronTask struct {
	ID                 string     `gorm:"column:id;primaryKey;size:36"`
	Name               string     `gorm:"column:name;size:128;not null"`
	AgentID            string     `gorm:"column:agent_id;size:36;not null;index"`
	ScheduleKind       string     `gorm:"column:schedule_kind;size:16;not null"` // cron, every, at
	ScheduleExpr       string     `gorm:"column:schedule_expr;size:256;not null"`
	Timezone           string     `gorm:"column:timezone;size:64;not null;default:UTC"`
	StaggerSec         int        `gorm:"column:stagger_sec;not null;default:0"`
	PayloadKind        string     `gorm:"column:payload_kind;size:16;not null"` // agent_turn, skill_execute
	PayloadContent     string     `gorm:"column:payload_content;type:text;not null"`
	TimeoutSec         int        `gorm:"column:timeout_sec;not null;default:300"`
	RetryCount         int        `gorm:"column:retry_count;not null;default:0"`
	RetryIntervalSec   int        `gorm:"column:retry_interval_sec;not null;default:60"`
	DeliveryMode       string     `gorm:"column:delivery_mode;size:16;not null;default:none"`
	DeliveryWebhookURL string     `gorm:"column:delivery_webhook_url;size:512"`
	DeliverySecret     string     `gorm:"column:delivery_secret;size:256"`
	DeliveryBestEffort bool       `gorm:"column:delivery_best_effort;not null;default:false"`
	DeliverySessionID  string     `gorm:"column:delivery_session_id;size:36"`
	DeliveryChannelID  string     `gorm:"column:delivery_channel_id;size:36"`
	Enabled            bool       `gorm:"column:enabled;not null;default:true"`
	NextRunAt          *time.Time `gorm:"column:next_run_at"`
	CreatedAt          time.Time  `gorm:"column:created_at;not null"`
	UpdatedAt          time.Time  `gorm:"column:updated_at;not null"`
}

func (CronTask) TableName() string {
	return "cron_tasks"
}

// CronRun 定时任务执行历史表
type CronRun struct {
	ID            string     `gorm:"column:id;primaryKey;size:36"`
	TaskID        string     `gorm:"column:task_id;size:36;not null;index"`
	TriggeredAt   time.Time  `gorm:"column:triggered_at;not null"`
	StartedAt     time.Time  `gorm:"column:started_at;not null"`
	FinishedAt    *time.Time `gorm:"column:finished_at"`
	Status        string     `gorm:"column:status;size:16;not null"` // success, failed, timeout, cancelled
	OutputSummary string     `gorm:"column:output_summary;type:text"`
	Error         string     `gorm:"column:error;type:text"`
	DeliveryOK    *bool      `gorm:"column:delivery_ok"`
}

func (CronRun) TableName() string {
	return "cron_run_history"
}
