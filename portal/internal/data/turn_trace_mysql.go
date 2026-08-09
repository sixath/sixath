package data

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"backend/internal/data/model"

	"github.com/google/uuid"
	"github.com/sixath/framework/agent"
	"github.com/sixath/framework/turntrace"
	"gorm.io/gorm"
)

var _ turntrace.Store = (*TurnTraceStore)(nil)

// TurnTraceStore persists agent.TurnTrace rows in MySQL (SQLite-compatible for tests).
type TurnTraceStore struct {
	db *gorm.DB
}

// NewTurnTraceStore creates a TurnTraceStore backed by db.
func NewTurnTraceStore(db *gorm.DB) *TurnTraceStore {
	return &TurnTraceStore{db: db}
}

// NewTurnTraceStoreFromData returns a turntrace.Store for wire injection.
func NewTurnTraceStoreFromData(data *Data) turntrace.Store {
	if data == nil || data.db == nil {
		return nil
	}
	return NewTurnTraceStore(data.db)
}

const turnTraceUpsertMaxAttempts = 5

func (s *TurnTraceStore) Upsert(ctx context.Context, t *agent.TurnTrace) error {
	if t == nil {
		return fmt.Errorf("turntrace: nil trace")
	}
	if t.SessionID == "" || t.RequestID == "" {
		return fmt.Errorf("turntrace: session_id and request_id required")
	}
	payload, err := json.Marshal(t)
	if err != nil {
		return fmt.Errorf("turntrace: marshal: %w", err)
	}
	payloadStr := string(payload)

	for attempt := 0; attempt < turnTraceUpsertMaxAttempts; attempt++ {
		updated, seq, err := s.updatePayloadIfExists(ctx, t.SessionID, t.RequestID, payloadStr)
		if err != nil {
			return err
		}
		if updated {
			t.TurnSeq = seq
			return nil
		}

		err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			var maxSeq int
			if err := tx.Model(&model.TurnTraceRow{}).
				Where("session_id = ?", t.SessionID).
				Select("COALESCE(MAX(turn_seq), 0)").
				Scan(&maxSeq).Error; err != nil {
				return err
			}
			createdAt := t.CreatedAt
			if createdAt.IsZero() {
				createdAt = time.Now().UTC()
			}
			row := model.TurnTraceRow{
				ID:        uuid.New().String(),
				SessionID: t.SessionID,
				AgentID:   t.AgentID,
				RequestID: t.RequestID,
				TurnSeq:   maxSeq + 1,
				Payload:   payloadStr,
				Active:    true,
				CreatedAt: createdAt,
			}
			if err := tx.Create(&row).Error; err != nil {
				return err
			}
			t.TurnSeq = row.TurnSeq
			return nil
		})
		if err == nil {
			return nil
		}
		if isUniqueConflict(err) {
			continue
		}
		return err
	}
	return fmt.Errorf("turntrace: upsert retries exhausted for session=%s request=%s", t.SessionID, t.RequestID)
}

func (s *TurnTraceStore) updatePayloadIfExists(ctx context.Context, sessionID, requestID, payload string) (bool, int, error) {
	var row model.TurnTraceRow
	err := s.db.WithContext(ctx).
		Where("session_id = ? AND request_id = ?", sessionID, requestID).
		First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return false, 0, nil
	}
	if err != nil {
		return false, 0, err
	}
	if err := s.db.WithContext(ctx).Model(&row).Update("payload_json", payload).Error; err != nil {
		return false, 0, err
	}
	return true, row.TurnSeq, nil
}

func (s *TurnTraceStore) GetByRequest(ctx context.Context, sessionID, requestID string) (*agent.TurnTrace, error) {
	var row model.TurnTraceRow
	err := s.db.WithContext(ctx).
		Where("session_id = ? AND request_id = ?", sessionID, requestID).
		First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return rowToTurnTrace(&row)
}

func (s *TurnTraceStore) ListBySession(ctx context.Context, sessionID string, limit int) ([]agent.TurnTrace, error) {
	q := s.db.WithContext(ctx).
		Where("session_id = ? AND active = ?", sessionID, true).
		Order("turn_seq DESC")
	if limit > 0 {
		q = q.Limit(limit)
	}
	var rows []model.TurnTraceRow
	if err := q.Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]agent.TurnTrace, 0, len(rows))
	for i := range rows {
		tr, err := rowToTurnTrace(&rows[i])
		if err != nil {
			return nil, err
		}
		out = append(out, *tr)
	}
	return out, nil
}

// DeactivateAfter sets active=false for traces in session with created_at >= at.
// Returns the request_ids that were deactivated (for FTS projection cleanup).
func (s *TurnTraceStore) DeactivateAfter(ctx context.Context, sessionID string, at time.Time) ([]string, error) {
	if sessionID == "" {
		return nil, nil
	}
	var rows []model.TurnTraceRow
	if err := s.db.WithContext(ctx).
		Where("session_id = ? AND active = ? AND created_at >= ?", sessionID, true, at).
		Find(&rows).Error; err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}
	ids := make([]string, 0, len(rows))
	requestIDs := make([]string, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.ID)
		requestIDs = append(requestIDs, row.RequestID)
	}
	if err := s.db.WithContext(ctx).Model(&model.TurnTraceRow{}).
		Where("id IN ?", ids).
		Update("active", false).Error; err != nil {
		return nil, err
	}
	return requestIDs, nil
}

// ListByAgent returns active traces for agent_id with created_at in [from, to] (inclusive), newest first.
func (s *TurnTraceStore) ListByAgent(ctx context.Context, agentID string, from, to time.Time, limit int) ([]agent.TurnTrace, error) {
	if agentID == "" {
		return nil, nil
	}
	if limit <= 0 {
		limit = 5000
	}
	q := s.db.WithContext(ctx).
		Where("agent_id = ? AND active = ?", agentID, true)
	if !from.IsZero() {
		q = q.Where("created_at >= ?", from)
	}
	if !to.IsZero() {
		q = q.Where("created_at <= ?", to)
	}
	var rows []model.TurnTraceRow
	if err := q.Order("created_at DESC").Limit(limit).Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]agent.TurnTrace, 0, len(rows))
	for i := range rows {
		tr, err := rowToTurnTrace(&rows[i])
		if err != nil {
			return nil, err
		}
		out = append(out, *tr)
	}
	return out, nil
}

func rowToTurnTrace(row *model.TurnTraceRow) (*agent.TurnTrace, error) {
	var tr agent.TurnTrace
	if row.Payload != "" {
		if err := json.Unmarshal([]byte(row.Payload), &tr); err != nil {
			return nil, fmt.Errorf("turntrace: unmarshal: %w", err)
		}
	}
	tr.TurnSeq = row.TurnSeq
	if tr.SessionID == "" {
		tr.SessionID = row.SessionID
	}
	if tr.AgentID == "" {
		tr.AgentID = row.AgentID
	}
	if tr.RequestID == "" {
		tr.RequestID = row.RequestID
	}
	if tr.CreatedAt.IsZero() {
		tr.CreatedAt = row.CreatedAt
	}
	return &tr, nil
}

func isUniqueConflict(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, gorm.ErrDuplicatedKey) {
		return true
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "unique constraint") ||
		strings.Contains(msg, "duplicate entry") ||
		strings.Contains(msg, "duplicated key")
}
