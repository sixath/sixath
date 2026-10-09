package cron

import (
	"context"
	"errors"
	"reflect"
	"testing"

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

func TestSetHandbook_NilDisables(t *testing.T) {
	s := NewScheduler(nil, nil, 0, log.DefaultLogger)
	s.SetHandbook((*biz.HandbookUsecase)(nil))
	if s.handbookUC != nil {
		t.Fatal("a nil usecase must leave handbook jobs disabled")
	}
}
