package cron

import (
	"context"
	"errors"
	"sort"
	"strings"
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

func (r *memCronRepo) GetByID(_ context.Context, id string) (*biz.CronTaskMeta, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	t := r.tasks[id]
	if t == nil {
		return nil, errors.New("not found")
	}
	c := *t
	return &c, nil
}

// memRunRepo keeps runs in memory; failUpdates makes that many Update calls fail first.
type memRunRepo struct {
	mu          sync.Mutex
	runs        map[string]*biz.CronRunMeta
	failUpdates int
}

func newMemRunRepo() *memRunRepo { return &memRunRepo{runs: map[string]*biz.CronRunMeta{}} }

func (r *memRunRepo) Create(_ context.Context, run *biz.CronRunMeta) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	c := *run
	r.runs[run.ID] = &c
	return nil
}

func (r *memRunRepo) Update(_ context.Context, id string, updates map[string]any) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.failUpdates > 0 {
		r.failUpdates--
		return errors.New("db write failed")
	}
	run := r.runs[id]
	if run == nil {
		return errors.New("not found")
	}
	for k, v := range updates {
		switch k {
		case "status":
			run.Status = v.(string)
		case "error":
			run.Error = v.(string)
		case "output_summary":
			run.OutputSummary = v.(string)
		case "finished_at":
			f := v.(time.Time)
			run.FinishedAt = &f
		case "delivery_ok":
			ok := v.(bool)
			run.DeliveryOK = &ok
		case "delivery_error":
			run.DeliveryError = v.(string)
		}
	}
	return nil
}

func (r *memRunRepo) ListByTask(_ context.Context, taskID string, _, _ int32) ([]*biz.CronRunMeta, int, error) {
	out := r.byTask(taskID)
	return out, len(out), nil
}

func (r *memRunRepo) ListRunning(_ context.Context, startedBefore time.Time) ([]*biz.CronRunMeta, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []*biz.CronRunMeta
	for _, run := range r.runs {
		if run.Status == biz.CronRunRunning && run.StartedAt.Before(startedBefore) {
			c := *run
			out = append(out, &c)
		}
	}
	return out, nil
}

// byTask returns copies of a task's runs ordered by planned time.
func (r *memRunRepo) byTask(taskID string) []*biz.CronRunMeta {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []*biz.CronRunMeta
	for _, run := range r.runs {
		if run.TaskID == taskID {
			c := *run
			out = append(out, &c)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].TriggeredAt.Before(out[j].TriggeredAt) })
	return out
}

type startedRun struct {
	id          string
	triggeredAt time.Time
}

// recordingScheduler returns a scheduler whose runTask records started runs.
func recordingScheduler(tasks *memCronRepo, runs *memRunRepo, interval time.Duration) (*Scheduler, func() []startedRun) {
	s := NewScheduler(biz.NewCronUsecase(tasks, runs, log.DefaultLogger), nil, interval, log.DefaultLogger)
	var mu sync.Mutex
	var started []startedRun
	s.runTask = func(_ context.Context, task *biz.CronTaskMeta, triggeredAt time.Time) {
		mu.Lock()
		started = append(started, startedRun{task.ID, triggeredAt})
		mu.Unlock()
	}
	return s, func() []startedRun {
		time.Sleep(50 * time.Millisecond)
		mu.Lock()
		defer mu.Unlock()
		return append([]startedRun(nil), started...)
	}
}

func TestSchedulerTick_ClaimsEachDueSlotOnce(t *testing.T) {
	now := time.Now()
	due := now.Add(-time.Minute).Truncate(time.Second)
	repo := &memCronRepo{tasks: map[string]*biz.CronTaskMeta{
		"daily": {ID: "daily", Enabled: true, ScheduleKind: "cron", ScheduleExpr: "36 14 * * *", Timezone: "Asia/Shanghai", NextRunAt: &due},
		"once":  {ID: "once", Enabled: true, ScheduleKind: "at", ScheduleExpr: due.Format(time.RFC3339), NextRunAt: &due},
	}}
	s, started := recordingScheduler(repo, newMemRunRepo(), time.Hour)

	s.tick(context.Background())
	s.tick(context.Background())

	got := started()
	if len(got) != 2 {
		t.Fatalf("started %v, want each due task exactly once", got)
	}
	for _, r := range got {
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
}

func TestSchedulerTick_InvalidScheduleRecordsFailureWithoutRunning(t *testing.T) {
	due := time.Now().Add(-time.Minute).Truncate(time.Second)
	repo := &memCronRepo{tasks: map[string]*biz.CronTaskMeta{
		"bad":   {ID: "bad", Enabled: true, ScheduleKind: "cron", ScheduleExpr: "not a cron", NextRunAt: &due},
		"empty": {ID: "empty", Enabled: true, ScheduleKind: "cron", ScheduleExpr: "", NextRunAt: &due},
	}}
	runs := newMemRunRepo()
	s, started := recordingScheduler(repo, runs, time.Hour)

	s.tick(context.Background())

	if got := started(); len(got) != 0 {
		t.Fatalf("started %v, a task without a valid schedule must not run", got)
	}
	for _, id := range []string{"bad", "empty"} {
		if repo.tasks[id].NextRunAt != nil {
			t.Fatalf("%s must stop, next run %v", id, repo.tasks[id].NextRunAt)
		}
		recs := runs.byTask(id)
		if len(recs) != 1 {
			t.Fatalf("%s runs = %d, want one failure record", id, len(recs))
		}
		r := recs[0]
		if r.Status != biz.CronRunFailed || r.Trigger != biz.CronTriggerSchedule || !r.TriggeredAt.Equal(due) ||
			r.FinishedAt == nil || !strings.Contains(r.Error, "计划表达式无法解析") {
			t.Fatalf("%s run = %+v, want a finished failed schedule run explaining the schedule", id, r)
		}
	}
}

func TestSchedulerTick_RecordsMissedSlots(t *testing.T) {
	now := time.Now()
	// The first */10 slot more than an hour ago: six or seven later slots passed meanwhile.
	due, _ := biz.CronNextRun("cron", "*/10 * * * *", "UTC", now.Add(-75*time.Minute))
	repo := &memCronRepo{tasks: map[string]*biz.CronTaskMeta{
		"tenmin": {ID: "tenmin", Enabled: true, ScheduleKind: "cron", ScheduleExpr: "*/10 * * * *", Timezone: "UTC", NextRunAt: due},
	}}
	runs := newMemRunRepo()
	s, started := recordingScheduler(repo, runs, 30*time.Second)

	s.tick(context.Background())

	got := started()
	if len(got) != 1 || !got[0].triggeredAt.Equal(*due) {
		t.Fatalf("started %v, want only the earliest due slot %v", got, *due)
	}
	recs := runs.byTask("tenmin")
	if len(recs) < 6 {
		t.Fatalf("missed records = %d, want every slot after %v that passed", len(recs), *due)
	}
	prev := *due
	for _, r := range recs {
		if r.Status != biz.CronRunMissed || r.Trigger != biz.CronTriggerSchedule || r.FinishedAt == nil || !strings.Contains(r.Error, "后端未运行") {
			t.Fatalf("missed run = %+v", r)
		}
		if r.TriggeredAt.Sub(prev) != 10*time.Minute || !r.TriggeredAt.Before(now.Add(-30*time.Second)) {
			t.Fatalf("missed slot %v after %v, want consecutive past slots", r.TriggeredAt, prev)
		}
		prev = r.TriggeredAt
	}
}

func TestSchedulerTick_CapsMissedRecords(t *testing.T) {
	due := time.Now().Add(-2 * time.Hour)
	repo := &memCronRepo{tasks: map[string]*biz.CronTaskMeta{
		"minutely": {ID: "minutely", Enabled: true, ScheduleKind: "every", ScheduleExpr: "60", NextRunAt: &due},
	}}
	runs := newMemRunRepo()
	s, _ := recordingScheduler(repo, runs, 30*time.Second)

	s.tick(context.Background())

	recs := runs.byTask("minutely")
	if len(recs) != maxMissedRecords {
		t.Fatalf("missed records = %d, want the cap %d", len(recs), maxMissedRecords)
	}
	if !strings.Contains(recs[len(recs)-1].Error, "更多错过的执行") {
		t.Fatalf("last missed record must say more slots were missed, got %q", recs[len(recs)-1].Error)
	}
}

func TestSchedulerTick_DoesNotRecordOnTimeSlots(t *testing.T) {
	due := time.Now().Add(-10 * time.Second)
	repo := &memCronRepo{tasks: map[string]*biz.CronTaskMeta{
		"fast": {ID: "fast", Enabled: true, ScheduleKind: "every", ScheduleExpr: "5", NextRunAt: &due},
	}}
	runs := newMemRunRepo()
	s, _ := recordingScheduler(repo, runs, 30*time.Second)

	s.tick(context.Background())

	if recs := runs.byTask("fast"); len(recs) != 0 {
		t.Fatalf("slots within one tick of now are not missed, got %d records", len(recs))
	}
}

func TestSchedulerTick_MarksStaleRunsInterrupted(t *testing.T) {
	now := time.Now()
	repo := &memCronRepo{tasks: map[string]*biz.CronTaskMeta{
		"slow": {ID: "slow", Enabled: true, ScheduleKind: "cron", ScheduleExpr: "0 0 * * *", TimeoutSec: 600},
	}}
	runs := newMemRunRepo()
	_ = runs.Create(context.Background(), &biz.CronRunMeta{ID: "dead", TaskID: "slow", StartedAt: now.Add(-time.Hour), Status: biz.CronRunRunning})
	_ = runs.Create(context.Background(), &biz.CronRunMeta{ID: "alive", TaskID: "slow", StartedAt: now.Add(-8 * time.Minute), Status: biz.CronRunRunning})
	s, _ := recordingScheduler(repo, runs, 30*time.Second)

	s.tick(context.Background())

	dead, alive := runs.runs["dead"], runs.runs["alive"]
	if dead.Status != biz.CronRunInterrupted || dead.FinishedAt == nil || !strings.Contains(dead.Error, "执行中断") {
		t.Fatalf("dead run = %+v, want interrupted with an explanation", dead)
	}
	if alive.Status != biz.CronRunRunning {
		t.Fatalf("a run still within timeout+grace must stay running, got %+v", alive)
	}
}
