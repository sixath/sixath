package cron

import (
	"context"

	"backend/api/common"
	cronv1 "backend/api/cron/v1"
	"backend/internal/biz"

	"github.com/go-kratos/kratos/v2/errors"
	"google.golang.org/protobuf/types/known/structpb"
)

func baseSuccess() *common.BaseResponse {
	return &common.BaseResponse{Code: 0, Message: "ok"}
}

// CronService implements cron.v1.CronHTTPServer
type CronService struct {
	uc   *biz.CronUsecase
	exec *Executor
}

// NewCronService creates a CronService
func NewCronService(uc *biz.CronUsecase, exec *Executor) *CronService {
	return &CronService{uc: uc, exec: exec}
}

func cronTaskMetaToReply(t *biz.CronTaskMeta) *cronv1.CronTaskReply {
	r := &cronv1.CronTaskReply{
		Ret:                baseSuccess(),
		Id:                 t.ID,
		Name:               t.Name,
		AgentId:            t.AgentID,
		ScheduleKind:       t.ScheduleKind,
		ScheduleExpr:       t.ScheduleExpr,
		Timezone:           t.Timezone,
		StaggerSec:         int32(t.StaggerSec),
		PayloadKind:        t.PayloadKind,
		PayloadContent:     t.PayloadContent,
		TimeoutSec:         int32(t.TimeoutSec),
		RetryCount:         int32(t.RetryCount),
		RetryIntervalSec:   int32(t.RetryIntervalSec),
		DeliveryMode:       t.DeliveryMode,
		DeliveryWebhookUrl: t.DeliveryWebhookURL,
		DeliverySessionId:  t.DeliverySessionID,
		DeliveryChannelId:  t.DeliveryChannelID,
		Enabled:            t.Enabled,
		CreatedAt:          t.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
		UpdatedAt:          t.UpdatedAt.Format("2006-01-02T15:04:05Z07:00"),
	}
	if t.NextRunAt != nil {
		r.NextRunAt = t.NextRunAt.Format("2006-01-02T15:04:05Z07:00")
	}
	return r
}

func cronRunMetaToReply(r *biz.CronRunMeta) *cronv1.CronRunReply {
	rep := &cronv1.CronRunReply{
		Id:            r.ID,
		TaskId:        r.TaskID,
		TriggeredAt:   r.TriggeredAt.Format("2006-01-02T15:04:05Z07:00"),
		StartedAt:     r.StartedAt.Format("2006-01-02T15:04:05Z07:00"),
		Status:        r.Status,
		OutputSummary: r.OutputSummary,
		Error:         r.Error,
		DeliveryOk:    r.DeliveryOK,
	}
	if r.FinishedAt != nil {
		rep.FinishedAt = r.FinishedAt.Format("2006-01-02T15:04:05Z07:00")
	}
	return rep
}

func structToMapCron(s *structpb.Struct) map[string]any {
	if s == nil || s.Fields == nil {
		return nil
	}
	m := make(map[string]any)
	for k, v := range s.Fields {
		m[k] = structValueToAny(v)
	}
	return m
}

func structValueToAny(v *structpb.Value) any {
	if v == nil {
		return nil
	}
	switch x := v.Kind.(type) {
	case *structpb.Value_StringValue:
		return x.StringValue
	case *structpb.Value_NumberValue:
		return x.NumberValue
	case *structpb.Value_BoolValue:
		return x.BoolValue
	case *structpb.Value_ListValue:
		if x.ListValue == nil {
			return nil
		}
		arr := make([]any, len(x.ListValue.Values))
		for i, item := range x.ListValue.Values {
			arr[i] = structValueToAny(item)
		}
		return arr
	case *structpb.Value_StructValue:
		if x.StructValue == nil {
			return nil
		}
		return structToMapCron(x.StructValue)
	default:
		return nil
	}
}

// CreateCronTask implements cron.v1.CronHTTPServer
func (s *CronService) CreateCronTask(ctx context.Context, req *cronv1.CreateCronTaskRequest) (*cronv1.CronTaskReply, error) {
	if req.GetName() == "" || req.GetAgentId() == "" || req.GetScheduleKind() == "" || req.GetScheduleExpr() == "" || req.GetPayloadKind() == "" || req.GetPayloadContent() == "" {
		return nil, errors.BadRequest("INVALID", "name, agent_id, schedule_kind, schedule_expr, payload_kind, payload_content required")
	}
	timezone := req.GetTimezone()
	if timezone == "" {
		timezone = "UTC"
	}
	timeoutSec := req.GetTimeoutSec()
	if timeoutSec <= 0 {
		timeoutSec = 300
	}
	deliveryMode := req.GetDeliveryMode()
	if deliveryMode == "" {
		deliveryMode = "none"
	}
	t, err := s.uc.Create(ctx, &biz.CronTaskCreate{
		Name:               req.GetName(),
		AgentID:            req.GetAgentId(),
		ScheduleKind:       req.GetScheduleKind(),
		ScheduleExpr:       req.GetScheduleExpr(),
		Timezone:           timezone,
		StaggerSec:         int(req.GetStaggerSec()),
		PayloadKind:        req.GetPayloadKind(),
		PayloadContent:     req.GetPayloadContent(),
		TimeoutSec:         int(timeoutSec),
		RetryCount:         int(req.GetRetryCount()),
		RetryIntervalSec:   int(req.GetRetryIntervalSec()),
		DeliveryMode:       deliveryMode,
		DeliveryWebhookURL: req.GetDeliveryWebhookUrl(),
		DeliverySecret:     req.GetDeliverySecret(),
		DeliveryBestEffort: req.GetDeliveryBestEffort(),
		DeliverySessionID:  req.GetDeliverySessionId(),
		DeliveryChannelID:  req.GetDeliveryChannelId(),
		Enabled:            req.GetEnabled(),
	})
	if err != nil {
		return nil, err
	}
	return cronTaskMetaToReply(t), nil
}

// ListCronTasks implements cron.v1.CronHTTPServer
func (s *CronService) ListCronTasks(ctx context.Context, req *cronv1.ListCronTasksRequest) (*cronv1.ListCronTasksReply, error) {
	page, pageSize := req.GetPage(), req.GetPageSize()
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 20
	}
	var enabled *bool
	if req.Enabled != nil {
		enabled = req.Enabled
	}
	list, total, err := s.uc.List(ctx, page, pageSize, req.GetAgentId(), enabled)
	if err != nil {
		return nil, err
	}
	items := make([]*cronv1.CronTaskReply, len(list))
	for i, t := range list {
		items[i] = cronTaskMetaToReply(t)
	}
	return &cronv1.ListCronTasksReply{
		Ret:   baseSuccess(),
		Items: items,
		Total: int32(total),
	}, nil
}

// GetCronTask implements cron.v1.CronHTTPServer
func (s *CronService) GetCronTask(ctx context.Context, req *cronv1.GetCronTaskRequest) (*cronv1.CronTaskReply, error) {
	t, err := s.uc.Get(ctx, req.GetId())
	if err != nil {
		return nil, err
	}
	return cronTaskMetaToReply(t), nil
}

// UpdateCronTask implements cron.v1.CronHTTPServer
func (s *CronService) UpdateCronTask(ctx context.Context, req *cronv1.UpdateCronTaskRequest) (*cronv1.CronTaskReply, error) {
	updates := structToMapCron(req.GetUpdates())
	if len(updates) == 0 {
		return s.GetCronTask(ctx, &cronv1.GetCronTaskRequest{Id: req.GetId()})
	}
	t, err := s.uc.Update(ctx, req.GetId(), updates)
	if err != nil {
		return nil, err
	}
	return cronTaskMetaToReply(t), nil
}

// DeleteCronTask implements cron.v1.CronHTTPServer
func (s *CronService) DeleteCronTask(ctx context.Context, req *cronv1.DeleteCronTaskRequest) (*cronv1.DeleteCronTaskReply, error) {
	if err := s.uc.Delete(ctx, req.GetId()); err != nil {
		return nil, err
	}
	return &cronv1.DeleteCronTaskReply{Ret: baseSuccess()}, nil
}

// RunCronTask implements cron.v1.CronHTTPServer
func (s *CronService) RunCronTask(ctx context.Context, req *cronv1.RunCronTaskRequest) (*cronv1.RunCronTaskReply, error) {
	task, err := s.uc.Get(ctx, req.GetId())
	if err != nil {
		return nil, err
	}
	go s.exec.Execute(context.Background(), task)
	return &cronv1.RunCronTaskReply{Ret: baseSuccess()}, nil
}

// ListCronRuns implements cron.v1.CronHTTPServer
func (s *CronService) ListCronRuns(ctx context.Context, req *cronv1.ListCronRunsRequest) (*cronv1.ListCronRunsReply, error) {
	page, pageSize := req.GetPage(), req.GetPageSize()
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 20
	}
	list, total, err := s.uc.ListRuns(ctx, req.GetId(), page, pageSize)
	if err != nil {
		return nil, err
	}
	items := make([]*cronv1.CronRunReply, len(list))
	for i, r := range list {
		items[i] = cronRunMetaToReply(r)
	}
	return &cronv1.ListCronRunsReply{
		Ret:   baseSuccess(),
		Items: items,
		Total: int32(total),
	}, nil
}
