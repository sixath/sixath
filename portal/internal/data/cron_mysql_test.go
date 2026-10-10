package data

import (
	"context"
	"testing"
	"time"

	"backend/internal/biz"
	"backend/internal/data/model"

	"github.com/go-kratos/kratos/v2/log"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func newCronTaskRepoForTest(t *testing.T) (biz.CronTaskRepo, *gorm.DB) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := db.AutoMigrate(&model.CronTask{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return NewCronTaskRepo(&Data{db: db}, log.DefaultLogger), db
}

func TestCronTaskRepo_ClaimDue(t *testing.T) {
	ctx := context.Background()
	repo, db := newCronTaskRepoForTest(t)
	task, err := repo.Create(ctx, &biz.CronTaskCreate{
		Name: "noon", AgentID: "a1", ScheduleKind: "cron", ScheduleExpr: "36 14 * * *", Timezone: "Asia/Shanghai",
		PayloadKind: "agent_turn", PayloadContent: "hi", TimeoutSec: 60, DeliveryMode: "none", Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().Truncate(time.Second)
	due := now.Add(-time.Minute)
	if err := db.Model(&model.CronTask{}).Where("id = ?", task.ID).Update("next_run_at", due).Error; err != nil {
		t.Fatal(err)
	}
	next := now.Add(24 * time.Hour)

	ok, err := repo.ClaimDue(ctx, task.ID, now, &next)
	if err != nil || !ok {
		t.Fatalf("first claim: ok=%v err=%v", ok, err)
	}
	if ok, err := repo.ClaimDue(ctx, task.ID, now, &next); err != nil || ok {
		t.Fatalf("a claimed slot must not be claimed again: ok=%v err=%v", ok, err)
	}
	got, err := repo.GetByID(ctx, task.ID)
	if err != nil || got.NextRunAt == nil || !got.NextRunAt.Equal(next) {
		t.Fatalf("next_run_at = %v err=%v, want %v", got.NextRunAt, err, next)
	}
	if due, err := repo.ListDue(ctx, now); err != nil || len(due) != 0 {
		t.Fatalf("claimed task still due: %v err=%v", due, err)
	}

	if err := db.Model(&model.CronTask{}).Where("id = ?", task.ID).Updates(map[string]any{"next_run_at": due, "enabled": false}).Error; err != nil {
		t.Fatal(err)
	}
	if ok, err := repo.ClaimDue(ctx, task.ID, now, &next); err != nil || ok {
		t.Fatalf("a disabled task must not be claimed: ok=%v err=%v", ok, err)
	}

	if err := db.Model(&model.CronTask{}).Where("id = ?", task.ID).Update("enabled", true).Error; err != nil {
		t.Fatal(err)
	}
	if ok, err := repo.ClaimDue(ctx, task.ID, now, nil); err != nil || !ok {
		t.Fatalf("claim without a next run: ok=%v err=%v", ok, err)
	}
	if got, _ := repo.GetByID(ctx, task.ID); got.NextRunAt != nil {
		t.Fatalf("a task without a next run must clear next_run_at, got %v", got.NextRunAt)
	}
	if ok, err := repo.ClaimDue(ctx, "missing", now, &next); err != nil || ok {
		t.Fatalf("unknown task: ok=%v err=%v", ok, err)
	}
}

func TestCronRunRepo_TriggerDeliveryErrorAndListRunning(t *testing.T) {
	ctx := context.Background()
	_, db := newCronTaskRepoForTest(t)
	if err := db.AutoMigrate(&model.CronRun{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	repo := NewCronRunRepo(&Data{db: db}, log.DefaultLogger)
	now := time.Now().Truncate(time.Second)
	for _, r := range []*biz.CronRunMeta{
		{ID: "old", TaskID: "t1", Trigger: biz.CronTriggerSchedule, TriggeredAt: now.Add(-time.Hour), StartedAt: now.Add(-time.Hour), Status: biz.CronRunRunning},
		{ID: "new", TaskID: "t1", Trigger: biz.CronTriggerManual, TriggeredAt: now, StartedAt: now, Status: biz.CronRunRunning},
		{ID: "done", TaskID: "t1", Trigger: biz.CronTriggerSchedule, TriggeredAt: now.Add(-2 * time.Hour), StartedAt: now.Add(-2 * time.Hour), Status: biz.CronRunSuccess},
	} {
		if err := repo.Create(ctx, r); err != nil {
			t.Fatal(err)
		}
	}
	if err := repo.Update(ctx, "old", map[string]any{"delivery_ok": false, "delivery_error": "webhook 返回 HTTP 500"}); err != nil {
		t.Fatal(err)
	}

	running, err := repo.ListRunning(ctx, now.Add(-time.Minute))
	if err != nil || len(running) != 1 || running[0].ID != "old" {
		t.Fatalf("ListRunning = %v err=%v, want only the old running run", running, err)
	}
	runs, _, err := repo.ListByTask(ctx, "t1", 1, 10)
	if err != nil {
		t.Fatal(err)
	}
	byID := map[string]*biz.CronRunMeta{}
	for _, r := range runs {
		byID[r.ID] = r
	}
	if byID["new"].Trigger != biz.CronTriggerManual || byID["old"].Trigger != biz.CronTriggerSchedule {
		t.Fatalf("triggers = %q/%q", byID["new"].Trigger, byID["old"].Trigger)
	}
	if byID["old"].DeliveryError != "webhook 返回 HTTP 500" {
		t.Fatalf("delivery error = %q", byID["old"].DeliveryError)
	}
}
