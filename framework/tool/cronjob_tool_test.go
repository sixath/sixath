package tool

import (
	"context"
	"testing"
)

type mockCronClient struct {
	created []CronJobCreateInput
	list    []CronJobSummary
}

func (m *mockCronClient) Create(_ context.Context, agentID string, in CronJobCreateInput) (CronJobSummary, error) {
	m.created = append(m.created, in)
	return CronJobSummary{
		ID:             "task-1",
		Name:           in.Name,
		AgentID:        agentID,
		ScheduleKind:   in.ScheduleKind,
		ScheduleExpr:   in.ScheduleExpr,
		PayloadKind:    in.PayloadKind,
		PayloadContent: in.PayloadContent,
		Enabled:        in.Enabled,
	}, nil
}

func (m *mockCronClient) List(_ context.Context, _ string, _, _ int, _ *bool) ([]CronJobSummary, int, error) {
	return m.list, len(m.list), nil
}

func (m *mockCronClient) Update(_ context.Context, taskID string, updates map[string]any) (CronJobSummary, error) {
	enabled, _ := updates["enabled"].(bool)
	return CronJobSummary{ID: taskID, Enabled: enabled}, nil
}

func (m *mockCronClient) Delete(_ context.Context, taskID string) error {
	return nil
}

func (m *mockCronClient) RunAdHoc(_ context.Context, _ string) error {
	return nil
}

func TestCronCreateAllowed(t *testing.T) {
	if !CronCreateAllowed(context.Background()) {
		t.Fatal("expected create allowed in plain context")
	}
	ctx := context.WithValue(context.Background(), ContextKeyRunKind, "cron")
	if CronCreateAllowed(ctx) {
		t.Fatal("expected nested forbidden when run_kind=cron")
	}
	ctx = context.WithValue(context.Background(), ContextKeyAllowCronCreate, false)
	if CronCreateAllowed(ctx) {
		t.Fatal("expected forbidden when allow_cron_create=false")
	}
}

func TestCronjobToolNestedCreateForbidden(t *testing.T) {
	reg := NewRegistry()
	client := &mockCronClient{}
	if err := RegisterCronjobTool(reg, client, &CronjobToolConfig{Enabled: true}); err != nil {
		t.Fatal(err)
	}
	toolDef, ok := reg.Get("cronjob")
	if !ok {
		t.Fatal("cronjob not registered")
	}
	ctx := context.WithValue(context.Background(), ContextKeyAgentID, "agent-1")
	ctx = context.WithValue(ctx, ContextKeyRunKind, "cron")
	out, err := toolDef.Execute(ctx, map[string]any{
		"action":          "create",
		"name":            "daily",
		"schedule_kind":   "cron",
		"schedule_expr":   "0 9 * * *",
		"payload_kind":    "agent_turn",
		"payload_content": "summarize inbox",
	})
	if err != nil {
		t.Fatal(err)
	}
	m, ok := out.(map[string]any)
	if !ok || m["error"] != "cron_nested_forbidden" {
		t.Fatalf("expected cron_nested_forbidden, got %#v", out)
	}
	if len(client.created) != 0 {
		t.Fatal("client should not be called")
	}
}

func TestCronjobToolCreateAndList(t *testing.T) {
	reg := NewRegistry()
	client := &mockCronClient{list: []CronJobSummary{{ID: "task-1", Name: "daily"}}}
	if err := RegisterCronjobTool(reg, client, &CronjobToolConfig{Enabled: true}); err != nil {
		t.Fatal(err)
	}
	toolDef, _ := reg.Get("cronjob")

	ctx := context.WithValue(context.Background(), ContextKeyAgentID, "agent-1")
	createOut, err := toolDef.Execute(ctx, map[string]any{
		"action":          "create",
		"name":            "daily summary",
		"schedule_kind":   "cron",
		"schedule_expr":   "0 9 * * *",
		"payload_kind":    "agent_turn",
		"payload_content": "summarize inbox",
	})
	if err != nil {
		t.Fatal(err)
	}
	cm, ok := createOut.(map[string]any)
	if !ok || cm["ok"] != true {
		t.Fatalf("create failed: %#v", createOut)
	}
	if len(client.created) != 1 || client.created[0].Name != "daily summary" {
		t.Fatalf("unexpected create payload: %+v", client.created)
	}

	listOut, err := toolDef.Execute(ctx, map[string]any{"action": "list"})
	if err != nil {
		t.Fatal(err)
	}
	lm, ok := listOut.(map[string]any)
	if !ok || lm["total"] != 1 {
		t.Fatalf("list failed: %#v", listOut)
	}
}

func TestCronjobToolCheckFnDisabled(t *testing.T) {
	reg := NewRegistry()
	if err := RegisterCronjobTool(reg, &mockCronClient{}, &CronjobToolConfig{Enabled: false}); err != nil {
		t.Fatal(err)
	}
	toolDef, _ := reg.Get("cronjob")
	if toolDef.CheckFn == nil {
		t.Fatal("expected check fn")
	}
	if err := toolDef.CheckFn(context.Background()); err == nil {
		t.Fatal("expected disabled error")
	}
}
