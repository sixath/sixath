package biz

import (
	"context"
	"testing"
	"time"
)

type stubCronRefCronRepo struct {
	tasks []*CronTaskMeta
}

func (s *stubCronRefCronRepo) Create(ctx context.Context, t *CronTaskCreate) (*CronTaskMeta, error) {
	return nil, nil
}
func (s *stubCronRefCronRepo) GetByID(ctx context.Context, id string) (*CronTaskMeta, error) {
	return nil, nil
}
func (s *stubCronRefCronRepo) List(ctx context.Context, page, pageSize int32, agentID string, enabled *bool) ([]*CronTaskMeta, int, error) {
	return nil, 0, nil
}
func (s *stubCronRefCronRepo) Update(ctx context.Context, id string, updates map[string]any) (*CronTaskMeta, error) {
	for i := range s.tasks {
		if s.tasks[i].ID == id {
			if v, ok := updates["payload_content"].(string); ok {
				s.tasks[i].PayloadContent = v
			}
			return s.tasks[i], nil
		}
	}
	return nil, nil
}
func (s *stubCronRefCronRepo) Delete(ctx context.Context, id string) error { return nil }
func (s *stubCronRefCronRepo) ListDue(ctx context.Context, before time.Time) ([]*CronTaskMeta, error) {
	return nil, nil
}
func (s *stubCronRefCronRepo) UpdateNextRun(ctx context.Context, id string, nextRunAt time.Time) error {
	return nil
}
func (s *stubCronRefCronRepo) ListSkillExecuteByAgentIDs(ctx context.Context, agentIDs []string) ([]*CronTaskMeta, error) {
	set := make(map[string]struct{}, len(agentIDs))
	for _, id := range agentIDs {
		set[id] = struct{}{}
	}
	var out []*CronTaskMeta
	for _, t := range s.tasks {
		if _, ok := set[t.AgentID]; ok && t.PayloadKind == "skill_execute" {
			out = append(out, t)
		}
	}
	return out, nil
}

type stubCronRefAgentRepo struct {
	ids []string
}

func (s *stubCronRefAgentRepo) Create(ctx context.Context, id, name, description, systemPrompt, workspace string, modelConfig ModelConfig, debugRun bool, wecomChannelID string, runtimeTools RuntimeToolsConfig, toolIDs []string) (*AgentMeta, error) {
	return nil, nil
}
func (s *stubCronRefAgentRepo) GetByID(ctx context.Context, id string) (*AgentMeta, error) {
	return nil, nil
}
func (s *stubCronRefAgentRepo) GetByName(ctx context.Context, name string) (*AgentMeta, error) {
	return nil, nil
}
func (s *stubCronRefAgentRepo) List(ctx context.Context, page, pageSize int32) ([]*AgentMeta, int, error) {
	return nil, 0, nil
}
func (s *stubCronRefAgentRepo) ListByIDs(ctx context.Context, ids []string, page, pageSize int32) ([]*AgentMeta, int, error) {
	return nil, 0, nil
}
func (s *stubCronRefAgentRepo) Update(ctx context.Context, id string, updates map[string]any) (*AgentMeta, error) {
	return nil, nil
}
func (s *stubCronRefAgentRepo) Delete(ctx context.Context, id string) error { return nil }
func (s *stubCronRefAgentRepo) BindTools(ctx context.Context, agentID string, toolIDs []string) error {
	return nil
}
func (s *stubCronRefAgentRepo) UnbindTools(ctx context.Context, agentID string, toolIDs []string) error {
	return nil
}
func (s *stubCronRefAgentRepo) ListDistinctWorkspaces(ctx context.Context, limit int) ([]CuratorWorkspace, error) {
	return nil, nil
}
func (s *stubCronRefAgentRepo) CountByWecomChannelID(ctx context.Context, channelID string) (int, error) {
	return 0, nil
}

func (s *stubCronRefAgentRepo) ListAgentIDsByWorkspace(ctx context.Context, workspace string) ([]string, error) {
	if workspace == "ws1" {
		return s.ids, nil
	}
	return nil, nil
}

func TestCronRefRewriteUsecase_RewriteForWorkspace(t *testing.T) {
	cronRepo := &stubCronRefCronRepo{tasks: []*CronTaskMeta{{
		ID:             "t1",
		AgentID:        "a1",
		PayloadKind:    "skill_execute",
		PayloadContent: "daily-report/scripts/run.sh",
	}}}
	uc := NewCronRefRewriteUsecase(cronRepo, &stubCronRefAgentRepo{ids: []string{"a1"}})
	n, err := uc.RewriteForWorkspace(context.Background(), "ws1", map[string]string{"daily-report": "daily-report-v2"})
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("updated=%d", n)
	}
	if cronRepo.tasks[0].PayloadContent != "daily-report-v2/scripts/run.sh" {
		t.Fatalf("payload=%q", cronRepo.tasks[0].PayloadContent)
	}
}
