package cron

import (
	"context"
	"errors"
	"reflect"
	"sync/atomic"
	"testing"
	"time"

	"backend/internal/biz"

	"github.com/go-kratos/kratos/v2/log"
)

type recordingHandbook struct {
	calls      []string
	rebuildErr error
}

func (r *recordingHandbook) RebuildStale(context.Context) (int, error) {
	r.calls = append(r.calls, "rebuild")
	return 1, r.rebuildErr
}

func (r *recordingHandbook) EnrichPending(context.Context) (int, error) {
	r.calls = append(r.calls, "enrich")
	return 0, nil
}

func TestRunHandbookRebuild_EnrichesAfterRebuild(t *testing.T) {
	for _, rebuildErr := range []error{nil, errors.New("list failed")} {
		hb := &recordingHandbook{rebuildErr: rebuildErr}
		s := NewScheduler(nil, nil, 0, log.DefaultLogger)
		s.handbookUC = hb
		s.runHandbookRebuild(context.Background())
		if want := []string{"rebuild", "enrich"}; !reflect.DeepEqual(hb.calls, want) {
			t.Fatalf("rebuild err %v: calls = %v, want %v", rebuildErr, hb.calls, want)
		}
	}
}

// blockingHandbook rebuilds until its context ends.
type blockingHandbook struct {
	started  chan struct{}
	finished atomic.Bool
}

func (b *blockingHandbook) RebuildStale(ctx context.Context) (int, error) {
	close(b.started)
	<-ctx.Done()
	time.Sleep(20 * time.Millisecond) // e.g. releasing leases
	b.finished.Store(true)
	return 0, ctx.Err()
}

func (b *blockingHandbook) EnrichPending(context.Context) (int, error) { return 0, nil }

func TestScheduler_WaitCoversHandbookJobs(t *testing.T) {
	hb := &blockingHandbook{started: make(chan struct{})}
	s := NewScheduler(nil, nil, time.Hour, log.DefaultLogger)
	s.handbookUC = hb
	ctx, cancel := context.WithCancel(context.Background())
	s.startAsync(ctx)
	s.spawn(func() { s.runHandbookRebuild(ctx) })
	<-hb.started
	short, stop := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer stop()
	if err := s.Wait(short); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Wait must block while the scheduler runs: %v", err)
	}
	cancel()
	wctx, wcancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer wcancel()
	if err := s.Wait(wctx); err != nil || !hb.finished.Load() {
		t.Fatalf("Wait returned before the handbook job finished: err=%v finished=%v", err, hb.finished.Load())
	}
}

func TestServer_StopCancelsAndWaits(t *testing.T) {
	s := NewScheduler(nil, nil, time.Hour, log.DefaultLogger)
	srv := NewServer(s)
	if err := srv.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.Stop(ctx); err != nil {
		t.Fatal(err)
	}
	if err := s.Wait(ctx); err != nil {
		t.Fatalf("the scheduler must have stopped: %v", err)
	}
}

func TestSetHandbook_NilDisables(t *testing.T) {
	s := NewScheduler(nil, nil, 0, log.DefaultLogger)
	s.SetHandbook((*biz.HandbookUsecase)(nil))
	if s.handbookUC != nil {
		t.Fatal("a nil usecase must leave handbook jobs disabled")
	}
}
