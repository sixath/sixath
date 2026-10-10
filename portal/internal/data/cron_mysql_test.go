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
