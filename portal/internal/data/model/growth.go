package model

import "time"

// ChatGrowthState 会话级成长游标（与 spec：计数、pending 标志、最近错误）。
// 一行对应一个 chat_sessions.id。
type ChatGrowthState struct {
	SessionID              string    `gorm:"column:session_id;primaryKey;size:36"`
	ToolItersSinceReview   int       `gorm:"column:tool_iters_since_review;not null;default:0"`
	TurnsSinceMemoryReview int       `gorm:"column:turns_since_memory_review;not null;default:0"`
	PendingSkillReview     bool      `gorm:"column:pending_skill_review;not null;default:0"`
	PendingMemoryReview    bool      `gorm:"column:pending_memory_review;not null;default:0"`
	LastSkillError         string     `gorm:"column:last_skill_error;type:text"`
	LastMemoryError        string     `gorm:"column:last_memory_error;type:text"`
	ReviewFailedAt         *time.Time `gorm:"column:review_failed_at"`
	// ReviewRetryCount 复盘连续失败次数（spec phase2 A5）；成功后清零，达 max_retry 后 worker 清 pending。
	ReviewRetryCount       int        `gorm:"column:review_retry_count;not null;default:0"`
	LastIdleCheckAt        *time.Time `gorm:"column:last_idle_check_at"`
	// LastBackgroundReviewAt 最近一次 C3 即时 BackgroundReview 成功完成时间（与 async Worker 去重）。
	LastBackgroundReviewAt *time.Time `gorm:"column:last_background_review_at"`
	// LastReviewRequestID 最近一次已复盘的 chat request_id（SessionEnd / Worker 去重）。
	LastReviewRequestID string `gorm:"column:last_review_request_id;size:128;not null;default:''"`
	// BgReviewInFlight 本进程 C3 fork 进行中；Worker 认领前需检查（含 TTL 陈旧清理）。
	BgReviewInFlight bool `gorm:"column:bg_review_in_flight;not null;default:0"`
	// BgReviewInFlightSince in_flight 置位时间；超时后 Worker 强制清标志。
	BgReviewInFlightSince *time.Time `gorm:"column:bg_review_in_flight_since"`
	CreatedAt             time.Time  `gorm:"column:created_at;not null"`
	UpdatedAt             time.Time  `gorm:"column:updated_at;not null"`
}

func (ChatGrowthState) TableName() string {
	return "chat_growth_states"
}

// GrowthWorkspaceLease workspace 复盘租约（多 portal 单写者；spec §6）。
type GrowthWorkspaceLease struct {
	WorkspaceKey string    `gorm:"column:workspace_key;primaryKey;size:384"`
	HolderID     string    `gorm:"column:holder_id;size:128;not null"`
	ExpiresAt    time.Time `gorm:"column:expires_at;not null;index"`
	UpdatedAt    time.Time `gorm:"column:updated_at;not null"`
}

func (GrowthWorkspaceLease) TableName() string {
	return "growth_workspace_leases"
}
