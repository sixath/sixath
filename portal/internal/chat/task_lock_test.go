package chat

import (
	"strings"
	"testing"

	"github.com/sixath/framework/model"
)

const bf26Q = "需要看看access-service有没有收到游戏启动成功事件的时间和vm-manager有没有startGame成功"

func TestBuildTurnTaskLock_bf26values(t *testing.T) {
	prior := "流水 4_a8uva8m5tpsl 正常。uid=104551174 ugid=796"
	lock := BuildTurnTaskLock(bf26Q, []model.Message{
		{Role: "user", Content: "这条流水4_a8uva8m5tpsl 正常吗"},
		{Role: "assistant", Content: prior},
		{Role: "user", Content: bf26Q},
	})
	if lock.Q != bf26Q {
		t.Fatalf("Q=%q", lock.Q)
	}
	if !lock.HasPriorAssistant {
		t.Fatal("want prior assistant")
	}
	joined := strings.Join(lock.KnownValues, " ")
	for _, v := range []string{"4_a8uva8m5tpsl", "104551174", "796"} {
		if !strings.Contains(joined, v) {
			t.Fatalf("KnownValues missing %s: %v", v, lock.KnownValues)
		}
	}
	for _, v := range lock.KnownValues {
		if v == "access" {
			t.Fatalf("access must not enter KnownValues: %v", lock.KnownValues)
		}
	}
	block := lock.Format()
	if !strings.Contains(block, "【本轮任务锁】") || !strings.Contains(block, bf26Q) {
		t.Fatalf("format=%s", block)
	}
	if !strings.Contains(block, "不可改写") {
		t.Fatalf("format missing 不可改写: %s", block)
	}
}

func TestBuildTurnTaskLock_noIDFollowup(t *testing.T) {
	lock := BuildTurnTaskLock("那错误码是什么", []model.Message{
		{Role: "assistant", Content: "超时由网关重试导致"},
		{Role: "user", Content: "那错误码是什么"},
	})
	if !lock.HasPriorAssistant || lock.Q != "那错误码是什么" {
		t.Fatalf("%+v", lock)
	}
}

func TestQLooksLikeIntake(t *testing.T) {
	if qLooksLikeIntake("帮我查一个单") {
		t.Fatal("first-turn must not look like intake")
	}
	if !qLooksLikeIntake("请提供 flow_id 或 uuid") {
		t.Fatal("want intake")
	}
}

func TestAppendTaskLock_afterCatalog(t *testing.T) {
	lock := TurnTaskLock{Q: "查 access-service"}
	got := AppendTaskLock("【可用 Skills】catalog", lock)
	cat := strings.Index(got, "catalog")
	lk := strings.Index(got, "【本轮任务锁】")
	if cat < 0 || lk < 0 || lk < cat {
		t.Fatalf("lock must follow catalog, got %s", got)
	}
}

func TestBuildTurnTaskLock_bf26QUnchanged(t *testing.T) {
	lock := BuildTurnTaskLock(bf26Q, []model.Message{
		{Role: "user", Content: "这条流水4_a8uva8m5tpsl 正常吗"},
		{Role: "assistant", Content: "流水 4_a8uva8m5tpsl 正常。uid=104551174 ugid=796"},
		{Role: "user", Content: bf26Q},
	})
	if lock.Q != bf26Q || lock.Delivery != "" {
		t.Fatalf("%+v", lock)
	}
}

func TestBuildTurnTaskLock_inheritDeliveryChain(t *testing.T) {
	hist := []model.Message{
		{Role: "user", Content: "GetGameInfo 失败的原因是啥"},
		{Role: "assistant", Content: "Redis key 不存在"},
		{Role: "user", Content: "把相应的代码和日志都打印出来更加直观"},
		{Role: "assistant", Content: "我去贴"},
		{Role: "user", Content: "没有打印出来呀"},
	}
	lock := BuildTurnTaskLock("没有打印出来呀", hist)
	if lock.Q != "GetGameInfo 失败的原因是啥" {
		t.Fatalf("G=%q", lock.Q)
	}
	if lock.Delivery != "没有打印出来呀" {
		t.Fatalf("D=%q", lock.Delivery)
	}
	block := lock.Format()
	if !strings.Contains(block, "GetGameInfo 失败的原因是啥") {
		t.Fatalf("format missing G: %s", block)
	}
	if !strings.Contains(block, "本轮交付") || !strings.Contains(block, "没有打印出来呀") {
		t.Fatalf("format missing D: %s", block)
	}
	gIdx := strings.Index(block, "用户问题（不可改写）：")
	dIdx := strings.Index(block, "本轮交付")
	if gIdx < 0 || dIdx < gIdx {
		t.Fatalf("G must be the locked question before D: %s", block)
	}
}

func TestBuildTurnTaskLock_newFlowDoesNotInherit(t *testing.T) {
	hist := []model.Message{
		{Role: "user", Content: "GetGameInfo 失败的原因是啥"},
		{Role: "assistant", Content: "Redis nil"},
		{Role: "user", Content: "看另一条流水 4103_E1JAObeKMdw2"},
	}
	q := "看另一条流水 4103_E1JAObeKMdw2"
	lock := BuildTurnTaskLock(q, hist)
	if lock.Q != q || lock.Delivery != "" {
		t.Fatalf("%+v", lock)
	}
}

func TestBuildTurnTaskLock_newOpaqueDoesNotInherit(t *testing.T) {
	hist := []model.Message{
		{Role: "user", Content: "GetGameInfo 失败的原因是啥"},
		{Role: "assistant", Content: "Redis nil"},
		{Role: "user", Content: "再查 4103_E1JAObeKMdw2"},
	}
	q := "再查 4103_E1JAObeKMdw2"
	lock := BuildTurnTaskLock(q, hist)
	if lock.Q != q || lock.Delivery != "" {
		t.Fatalf("new opaque must be new G, got %+v", lock)
	}
}

func TestIsDeliveryUtterance(t *testing.T) {
	for _, s := range []string{"没有打印出来呀", "现在补查", "vm-manager的索引是vm_manager吧", "继续"} {
		if !isDeliveryUtterance(s) {
			t.Fatalf("want delivery: %q", s)
		}
	}
	for _, s := range []string{bf26Q, "先放放，改查预启动", "看另一条流水"} {
		if isDeliveryUtterance(s) {
			t.Fatalf("not delivery: %q", s)
		}
	}
}
