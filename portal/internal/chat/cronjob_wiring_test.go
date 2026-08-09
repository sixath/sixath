package chat

import (
	"context"
	"testing"
	"time"

	"backend/internal/biz"

	"github.com/sixath/framework/tool"
)

type stubCronRepo struct {
	tasks []*biz.CronTaskMeta
}

func (s *stubCronRepo) Create(_ context.Context, t *biz.CronTaskCreate) (*biz.CronTaskMeta, error) {
	meta := &biz.CronTaskMeta{
		ID:             "task-created",
		Name:           t.Name,
		AgentID:        t.AgentID,
		ScheduleKind:   t.ScheduleKind,
		ScheduleExpr:   t.ScheduleExpr,
		PayloadKind:    t.PayloadKind,
		PayloadContent: t.PayloadContent,
		Enabled:        t.Enabled,
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
	}
	s.tasks = append(s.tasks, meta)
	return meta, nil
}

func (s *stubCronRepo) GetByID(_ context.Context, id string) (*biz.CronTaskMeta, error) {
	for _, t := range s.tasks {
		if t.ID == id {
			return t, nil
		}
	}
	return nil, biz.ErrCronTaskNotFound
}

func (s *stubCronRepo) List(_ context.Context, _, _ int32, agentID string, _ *bool) ([]*biz.CronTaskMeta, int, error) {
	var out []*biz.CronTaskMeta
	for _, t := range s.tasks {
		if agentID == "" || t.AgentID == agentID {
			out = append(out, t)
		}
	}
	return out, len(out), nil
}

func (s *stubCronRepo) Update(_ context.Context, id string, updates map[string]any) (*biz.CronTaskMeta, error) {
	for _, t := range s.tasks {
		if t.ID == id {
			if v, ok := updates["enabled"].(bool); ok {
				t.Enabled = v
			}
			return t, nil
		}
	}
	return nil, biz.ErrCronTaskNotFound
}

func (s *stubCronRepo) Delete(_ context.Context, id string) error {
	for i, t := range s.tasks {
		if t.ID == id {
			s.tasks = append(s.tasks[:i], s.tasks[i+1:]...)
			return nil
		}
	}
	return biz.ErrCronTaskNotFound
}

func (s *stubCronRepo) ListDue(context.Context, time.Time) ([]*biz.CronTaskMeta, error) {
	return nil, nil
}

func (s *stubCronRepo) UpdateNextRun(context.Context, string, time.Time) error { return nil }

func (s *stubCronRepo) ListSkillExecuteByAgentIDs(context.Context, []string) ([]*biz.CronTaskMeta, error) {
	return nil, nil
}

type stubCronRunRepo struct{}

func (stubCronRunRepo) Create(context.Context, *biz.CronRunMeta) error { return nil }
func (stubCronRunRepo) Update(context.Context, string, map[string]any) error {
	return nil
}
func (stubCronRunRepo) ListByTask(context.Context, string, int32, int32) ([]*biz.CronRunMeta, int, error) {
	return nil, 0, nil
}

func TestPortalCronClientCreateAndList(t *testing.T) {
	repo := &stubCronRepo{}
	uc := biz.NewCronUsecase(repo, stubCronRunRepo{}, nil)
	client := NewPortalCronClient(uc, nil)

	created, err := client.Create(context.Background(), "agent-1", tool.CronJobCreateInput{
		Name:           "daily",
		ScheduleKind:   "cron",
		ScheduleExpr:   "0 9 * * *",
		PayloadKind:    "agent_turn",
		PayloadContent: "summarize",
		Enabled:        true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if created.ID != "task-created" {
		t.Fatalf("unexpected id: %s", created.ID)
	}

	items, total, err := client.List(context.Background(), "agent-1", 1, 20, nil)
	if err != nil {
		t.Fatal(err)
	}
	if total != 1 || len(items) != 1 || items[0].Name != "daily" {
		t.Fatalf("list mismatch: total=%d items=%+v", total, items)
	}
}

func TestRequestMetadataFromCronContext(t *testing.T) {
	ctx := WithCronSessionContext(context.Background())
	md := RequestMetadataFromContext(ctx)
	if md[MetaRunKind] != "cron" {
		t.Fatalf("run_kind: %#v", md)
	}
	if md[MetaAllowCronCreate] != false {
		t.Fatalf("allow_cron_create: %#v", md)
	}
	if md[MetaSkipMemory] != true || md[MetaSkipGrowthReview] != true {
		t.Fatalf("skip flags: %#v", md)
	}
}
