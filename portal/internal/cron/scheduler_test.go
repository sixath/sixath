package cron

import (
	"context"
	"sync"
	"testing"
	"time"

	"backend/internal/biz"

	"github.com/go-kratos/kratos/v2/log"
)

// memCronRepo keeps tasks in memory with ClaimDue semantics of the MySQL repo.
type memCronRepo struct {
	biz.CronTaskRepo
	mu    sync.Mutex
	tasks map[string]*biz.CronTaskMeta
}

func (r *memCronRepo) ListDue(_ context.Context, before time.Time) ([]*biz.CronTaskMeta, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []*biz.CronTaskMeta
	for _, t := range r.tasks {
		if t.Enabled && t.NextRunAt != nil && !t.NextRunAt.After(before) {
			c := *t
			out = append(out, &c)
		}
	}
	return out, nil
}

func (r *memCronRepo) ClaimDue(_ context.Context, id string, now time.Time, next *time.Time) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	t := r.tasks[id]
	if t == nil || !t.Enabled || t.NextRunAt == nil || t.NextRunAt.After(now) {
		return false, nil
	}
	t.NextRunAt = next
	return true, nil
}

type startedRun struct {
	id          string
	triggeredAt time.Time
}

func TestSchedulerTick_ClaimsEachDueSlotOnce(t *testing.T) {
	now := time.Now()
	due := now.Add(-time.Minute).Truncate(time.Second)
	repo := &memCronRepo{tasks: map[string]*biz.CronTaskMeta{
		"daily": {ID: "daily", Enabled: true, ScheduleKind: "cron", ScheduleExpr: "36 14 * * *", Timezone: "Asia/Shanghai", NextRunAt: &due},
		"once":  {ID: "once", Enabled: true, ScheduleKind: "at", ScheduleExpr: due.Format(time.RFC3339), NextRunAt: &due},
		"bad":   {ID: "bad", Enabled: true, ScheduleKind: "cron", ScheduleExpr: "not a cron", NextRunAt: &due},
	}}
	s := NewScheduler(biz.NewCronUsecase(repo, nil, log.DefaultLogger), nil, time.Hour, log.DefaultLogger)
	var (
		mu      sync.Mutex
		started []startedRun
		wg      sync.WaitGroup
	)
	wg.Add(3)
	s.runTask = func(_ context.Context, task *biz.CronTaskMeta, triggeredAt time.Time) {
		mu.Lock()
		started = append(started, startedRun{task.ID, triggeredAt})
		mu.Unlock()
		wg.Done()
	}

	s.tick(context.Background())
	s.tick(context.Background())
	wg.Wait()
	time.Sleep(50 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	if len(started) != 3 {
		t.Fatalf("started %v, want each due task exactly once", started)
	}
	for _, r := range started {
		if !r.triggeredAt.Equal(due) {
			t.Fatalf("%s triggered at %v, want the due slot %v", r.id, r.triggeredAt, due)
		}
	}
	shanghai, _ := time.LoadLocation("Asia/Shanghai")
	next := repo.tasks["daily"].NextRunAt
	if next == nil || !next.After(now) || next.In(shanghai).Hour() != 14 || next.In(shanghai).Minute() != 36 {
		t.Fatalf("daily next run = %v, want the next 14:36 after now", next)
	}
	if repo.tasks["once"].NextRunAt != nil {
		t.Fatalf("a fired one-shot task must have no next run, got %v", repo.tasks["once"].NextRunAt)
	}
	if repo.tasks["bad"].NextRunAt != nil {
		t.Fatalf("a task whose schedule no longer parses must stop, got %v", repo.tasks["bad"].NextRunAt)
	}
}
