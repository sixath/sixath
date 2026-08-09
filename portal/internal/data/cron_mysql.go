package data

import (
	"context"
	"strconv"
	"time"

	"backend/internal/biz"
	"backend/internal/data/model"

	"github.com/go-kratos/kratos/v2/log"
	"github.com/google/uuid"
	"github.com/robfig/cron/v3"
	"gorm.io/gorm"
)

var _ biz.CronTaskRepo = (*cronTaskRepo)(nil)
var _ biz.CronRunRepo = (*cronRunRepo)(nil)

type cronTaskRepo struct {
	db  *gorm.DB
	log *log.Helper
}

type cronRunRepo struct {
	db  *gorm.DB
	log *log.Helper
}

func NewCronTaskRepo(data *Data, logger log.Logger) biz.CronTaskRepo {
	if data == nil || data.db == nil {
		panic("NewCronTaskRepo: Data.db is nil")
	}
	return &cronTaskRepo{db: data.db, log: log.NewHelper(logger)}
}

func NewCronRunRepo(data *Data, logger log.Logger) biz.CronRunRepo {
	if data == nil || data.db == nil {
		panic("NewCronRunRepo: Data.db is nil")
	}
	return &cronRunRepo{db: data.db, log: log.NewHelper(logger)}
}

// CronTaskRepo impl
func (r *cronTaskRepo) Create(ctx context.Context, t *biz.CronTaskCreate) (*biz.CronTaskMeta, error) {
	id := uuid.New().String()
	m := &model.CronTask{
		ID:                 id,
		Name:               t.Name,
		AgentID:            t.AgentID,
		ScheduleKind:       t.ScheduleKind,
		ScheduleExpr:       t.ScheduleExpr,
		Timezone:           t.Timezone,
		StaggerSec:         t.StaggerSec,
		PayloadKind:        t.PayloadKind,
		PayloadContent:     t.PayloadContent,
		TimeoutSec:         t.TimeoutSec,
		RetryCount:         t.RetryCount,
		RetryIntervalSec:   t.RetryIntervalSec,
		DeliveryMode:       t.DeliveryMode,
		DeliveryWebhookURL: t.DeliveryWebhookURL,
		DeliverySecret:     t.DeliverySecret,
		DeliveryBestEffort: t.DeliveryBestEffort,
		DeliverySessionID:  t.DeliverySessionID,
		DeliveryChannelID:  t.DeliveryChannelID,
		Enabled:            t.Enabled,
	}
	if err := r.db.WithContext(ctx).Create(m).Error; err != nil {
		return nil, err
	}
	next, _ := computeNextRun(m)
	if next != nil {
		r.db.WithContext(ctx).Model(m).Update("next_run_at", next)
		m.NextRunAt = next
	}
	return cronTaskModelToBiz(m), nil
}

func (r *cronTaskRepo) GetByID(ctx context.Context, id string) (*biz.CronTaskMeta, error) {
	var m model.CronTask
	if err := r.db.WithContext(ctx).Where("id = ?", id).First(&m).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return cronTaskModelToBiz(&m), nil
}

func (r *cronTaskRepo) List(ctx context.Context, page, pageSize int32, agentID string, enabled *bool) ([]*biz.CronTaskMeta, int, error) {
	q := r.db.WithContext(ctx).Model(&model.CronTask{})
	if agentID != "" {
		q = q.Where("agent_id = ?", agentID)
	}
	if enabled != nil {
		q = q.Where("enabled = ?", *enabled)
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}
	offset := (page - 1) * pageSize
	var list []model.CronTask
	if err := q.Order("created_at DESC").Offset(int(offset)).Limit(int(pageSize)).Find(&list).Error; err != nil {
		return nil, 0, err
	}
	out := make([]*biz.CronTaskMeta, len(list))
	for i := range list {
		out[i] = cronTaskModelToBiz(&list[i])
	}
	return out, int(total), nil
}

func (r *cronTaskRepo) Update(ctx context.Context, id string, updates map[string]any) (*biz.CronTaskMeta, error) {
	var m model.CronTask
	if err := r.db.WithContext(ctx).Where("id = ?", id).First(&m).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if err := r.db.WithContext(ctx).Model(&m).Updates(updates).Error; err != nil {
		return nil, err
	}
	var updated model.CronTask
	if err := r.db.WithContext(ctx).Where("id = ?", id).First(&updated).Error; err != nil {
		return nil, err
	}
	next, _ := computeNextRun(&updated)
	if next != nil {
		r.db.WithContext(ctx).Model(&updated).Update("next_run_at", next)
		updated.NextRunAt = next
	}
	return cronTaskModelToBiz(&updated), nil
}

func (r *cronTaskRepo) Delete(ctx context.Context, id string) error {
	res := r.db.WithContext(ctx).Where("id = ?", id).Delete(&model.CronTask{})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *cronTaskRepo) ListDue(ctx context.Context, before time.Time) ([]*biz.CronTaskMeta, error) {
	var list []model.CronTask
	if err := r.db.WithContext(ctx).Where("enabled = ? AND next_run_at IS NOT NULL AND next_run_at <= ?", true, before).
		Order("next_run_at ASC").Find(&list).Error; err != nil {
		return nil, err
	}
	out := make([]*biz.CronTaskMeta, len(list))
	for i := range list {
		out[i] = cronTaskModelToBiz(&list[i])
	}
	return out, nil
}

func (r *cronTaskRepo) UpdateNextRun(ctx context.Context, id string, nextRunAt time.Time) error {
	return r.db.WithContext(ctx).Model(&model.CronTask{}).Where("id = ?", id).Update("next_run_at", nextRunAt).Error
}

func (r *cronTaskRepo) ListSkillExecuteByAgentIDs(ctx context.Context, agentIDs []string) ([]*biz.CronTaskMeta, error) {
	if len(agentIDs) == 0 {
		return nil, nil
	}
	var list []model.CronTask
	if err := r.db.WithContext(ctx).
		Where("agent_id IN ? AND payload_kind = ?", agentIDs, "skill_execute").
		Find(&list).Error; err != nil {
		return nil, err
	}
	out := make([]*biz.CronTaskMeta, len(list))
	for i := range list {
		out[i] = cronTaskModelToBiz(&list[i])
	}
	return out, nil
}

// CronRunRepo impl
func (r *cronRunRepo) Create(ctx context.Context, run *biz.CronRunMeta) error {
	m := &model.CronRun{
		ID:            run.ID,
		TaskID:        run.TaskID,
		TriggeredAt:   run.TriggeredAt,
		StartedAt:     run.StartedAt,
		FinishedAt:    run.FinishedAt,
		Status:        run.Status,
		OutputSummary: run.OutputSummary,
		Error:         run.Error,
		DeliveryOK:    run.DeliveryOK,
	}
	return r.db.WithContext(ctx).Create(m).Error
}

func (r *cronRunRepo) Update(ctx context.Context, id string, updates map[string]any) error {
	return r.db.WithContext(ctx).Model(&model.CronRun{}).Where("id = ?", id).Updates(updates).Error
}

func (r *cronRunRepo) ListByTask(ctx context.Context, taskID string, page, pageSize int32) ([]*biz.CronRunMeta, int, error) {
	q := r.db.WithContext(ctx).Model(&model.CronRun{}).Where("task_id = ?", taskID)
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}
	offset := (page - 1) * pageSize
	var list []model.CronRun
	if err := q.Order("triggered_at DESC").Offset(int(offset)).Limit(int(pageSize)).Find(&list).Error; err != nil {
		return nil, 0, err
	}
	out := make([]*biz.CronRunMeta, len(list))
	for i := range list {
		out[i] = cronRunModelToBiz(&list[i])
	}
	return out, int(total), nil
}

func cronTaskModelToBiz(m *model.CronTask) *biz.CronTaskMeta {
	return &biz.CronTaskMeta{
		ID:                 m.ID,
		Name:               m.Name,
		AgentID:            m.AgentID,
		ScheduleKind:       m.ScheduleKind,
		ScheduleExpr:       m.ScheduleExpr,
		Timezone:           m.Timezone,
		StaggerSec:         m.StaggerSec,
		PayloadKind:        m.PayloadKind,
		PayloadContent:     m.PayloadContent,
		TimeoutSec:         m.TimeoutSec,
		RetryCount:         m.RetryCount,
		RetryIntervalSec:   m.RetryIntervalSec,
		DeliveryMode:       m.DeliveryMode,
		DeliveryWebhookURL: m.DeliveryWebhookURL,
		DeliverySecret:     m.DeliverySecret,
		DeliveryBestEffort: m.DeliveryBestEffort,
		DeliverySessionID:  m.DeliverySessionID,
		DeliveryChannelID:  m.DeliveryChannelID,
		Enabled:            m.Enabled,
		NextRunAt:          m.NextRunAt,
		CreatedAt:          m.CreatedAt,
		UpdatedAt:          m.UpdatedAt,
	}
}

func cronRunModelToBiz(m *model.CronRun) *biz.CronRunMeta {
	return &biz.CronRunMeta{
		ID:            m.ID,
		TaskID:        m.TaskID,
		TriggeredAt:   m.TriggeredAt,
		StartedAt:     m.StartedAt,
		FinishedAt:    m.FinishedAt,
		Status:        m.Status,
		OutputSummary: m.OutputSummary,
		Error:         m.Error,
		DeliveryOK:    m.DeliveryOK,
	}
}

// computeNextRun 计算下次执行时间
func computeNextRun(m *model.CronTask) (*time.Time, error) {
	loc := time.UTC
	if m.Timezone != "" {
		if l, err := time.LoadLocation(m.Timezone); err == nil {
			loc = l
		}
	}
	now := time.Now().In(loc)

	switch m.ScheduleKind {
	case "cron":
		if m.ScheduleExpr == "" {
			return nil, nil
		}
		parser := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)
		sched, err := parser.Parse(m.ScheduleExpr)
		if err != nil {
			return nil, err
		}
		next := sched.Next(now)
		return &next, nil
	case "every":
		sec := 3600
		if n, err := strconv.Atoi(m.ScheduleExpr); err == nil && n > 0 {
			sec = n
		}
		next := now.Add(time.Duration(sec) * time.Second)
		return &next, nil
	case "at":
		t, err := time.ParseInLocation(time.RFC3339, m.ScheduleExpr, loc)
		if err != nil {
			return nil, err
		}
		return &t, nil
	}
	return nil, nil
}
