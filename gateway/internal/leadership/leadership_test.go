package leadership

import (
	"context"
	"testing"
)

type neverLeader struct{}

func (neverLeader) IsLeader(context.Context) bool { return false }

func TestAlwaysLeader(t *testing.T) {
	var l Leader = AlwaysLeader{}
	if !l.IsLeader(context.Background()) {
		t.Fatal("AlwaysLeader should always report leader")
	}
}

func TestCustomLeaderDropIn(t *testing.T) {
	// 多副本预留：非领导实例返回 false（Redis/Portal 租约实现可 drop-in）。
	var l Leader = neverLeader{}
	if l.IsLeader(context.Background()) {
		t.Fatal("neverLeader should report not leader")
	}
}