package cron

import (
	"context"
	"testing"

	cronv1 "backend/api/cron/v1"
	"backend/internal/biz"

	"github.com/go-kratos/kratos/v2/log"
)

type captureCronRepo struct {
	biz.CronTaskRepo
	last *biz.CronTaskCreate
}

func (r *captureCronRepo) Create(_ context.Context, t *biz.CronTaskCreate) (*biz.CronTaskMeta, error) {
	cp := *t
	r.last = &cp
	return &biz.CronTaskMeta{ID: "t1", Name: t.Name, DeliveryMode: t.DeliveryMode, DeliveryChannelID: t.DeliveryChannelID, DeliveryPeerID: t.DeliveryPeerID}, nil
}

func TestCronService_CreateMapsDeliveryPeerID(t *testing.T) {
	repo := &captureCronRepo{}
	s := NewCronService(biz.NewCronUsecase(repo, newMemRunRepo(), log.DefaultLogger), nil)

	reply, err := s.CreateCronTask(context.Background(), &cronv1.CreateCronTaskRequest{
		Name: "wx", AgentId: "a1", ScheduleKind: "cron", ScheduleExpr: "0 9 * * *",
		PayloadKind: "agent_turn", PayloadContent: "hi",
		DeliveryMode: "channel", DeliveryChannelId: "uuid-1", DeliveryPeerId: "p1@im.wechat",
	})
	if err != nil {
		t.Fatalf("CreateCronTask: %v", err)
	}
	if repo.last.DeliveryPeerID != "p1@im.wechat" {
		t.Fatalf("create input peer = %q", repo.last.DeliveryPeerID)
	}
	if reply.GetDeliveryPeerId() != "p1@im.wechat" {
		t.Fatalf("reply peer = %q", reply.GetDeliveryPeerId())
	}
}
