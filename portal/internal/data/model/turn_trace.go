package model

import "time"

type TurnTraceRow struct {
	ID        string    `gorm:"column:id;primaryKey;size:36"`
	SessionID string    `gorm:"column:session_id;size:36;uniqueIndex:uk_session_request;index"`
	AgentID   string    `gorm:"column:agent_id;size:128;index"`
	RequestID string    `gorm:"column:request_id;size:64;uniqueIndex:uk_session_request"`
	TurnSeq   int       `gorm:"column:turn_seq;not null"`
	Payload   string    `gorm:"column:payload_json;type:longtext"`
	Active    bool      `gorm:"column:active;not null;default:1"`
	CreatedAt time.Time `gorm:"column:created_at;not null"`
}

func (TurnTraceRow) TableName() string { return "turn_traces" }
