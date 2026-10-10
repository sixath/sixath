package cron

import (
	"context"
	"strings"
	"testing"
	"time"

	"backend/internal/biz"

	"github.com/go-kratos/kratos/v2/log"
)

func runRecordingExecutor(runs *memRunRepo) *Executor {
	return &Executor{
		cronUC: biz.NewCronUsecase(&memCronRepo{}, runs, log.DefaultLogger),
		log:    log.NewHelper(log.DefaultLogger),
	}
}

func onlyRun(t *testing.T, runs *memRunRepo, taskID string) *biz.CronRunMeta {
	t.Helper()
	recs := runs.byTask(taskID)
	if len(recs) != 1 {
		t.Fatalf("runs for %s = %d, want 1", taskID, len(recs))
	}
	return recs[0]
}

func TestExecute_RecordsManualFailureAndDeliveryError(t *testing.T) {
	runs := newMemRunRepo()
	e := runRecordingExecutor(runs)
	task := &biz.CronTaskMeta{ID: "t1", Name: "n", PayloadKind: "bogus", TimeoutSec: 5,
		DeliveryMode: "webhook", DeliveryBestEffort: true}

	e.Execute(context.Background(), task)

	r := onlyRun(t, runs, "t1")
	if r.Trigger != biz.CronTriggerManual || r.Status != biz.CronRunFailed || !strings.Contains(r.Error, "unknown payload_kind") || r.FinishedAt == nil {
		t.Fatalf("run = %+v, want a finished manual failure with its error", r)
	}
	if r.DeliveryOK == nil || *r.DeliveryOK || !strings.Contains(r.DeliveryError, "未配置 webhook") {
		t.Fatalf("delivery ok=%v error=%q, want the delivery error recorded", r.DeliveryOK, r.DeliveryError)
	}
}

func TestExecuteScheduled_RecordsPanicAsFailure(t *testing.T) {
	runs := newMemRunRepo()
	e := runRecordingExecutor(runs)
	e.payloadRunner = func(context.Context, *biz.CronTaskMeta) (string, error) { panic("boom") }
	due := time.Now().Add(-time.Second).Truncate(time.Second)
	task := &biz.CronTaskMeta{ID: "t2", PayloadKind: "agent_turn", TimeoutSec: 5}

	e.executeScheduled(context.Background(), task, due)

	r := onlyRun(t, runs, "t2")
	if r.Trigger != biz.CronTriggerSchedule || !r.TriggeredAt.Equal(due) {
		t.Fatalf("run trigger=%q at %v, want schedule at %v", r.Trigger, r.TriggeredAt, due)
	}
	if r.Status != biz.CronRunFailed || !strings.Contains(r.Error, "panic") || !strings.Contains(r.Error, "boom") {
		t.Fatalf("run = %+v, want a failure naming the panic", r)
	}
}

func TestExecute_FallsBackWhenResultWriteFails(t *testing.T) {
	old := saveResultBackoff
	saveResultBackoff = 0
	defer func() { saveResultBackoff = old }()
	runs := newMemRunRepo()
	runs.failUpdates = saveResultAttempts
	e := runRecordingExecutor(runs)

	e.Execute(context.Background(), &biz.CronTaskMeta{ID: "t3", PayloadKind: "bogus", TimeoutSec: 5})

	r := onlyRun(t, runs, "t3")
	if r.Status != biz.CronRunFailed || r.FinishedAt == nil ||
		!strings.Contains(r.Error, "保存执行结果失败") || !strings.Contains(r.Error, "unknown payload_kind") {
		t.Fatalf("run = %+v, want the fallback status and error", r)
	}
}
