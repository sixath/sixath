package leadership

import "context"

// Leader 判定当前实例是否持有领导权（如 wecom_bot 的 WSS 订阅租约）。
//
// 单副本部署用 AlwaysLeader（恒为 leader）；未来多副本时，用分布式租约实现
// （Portal lease / Redis），未持租约的实例返回 false，从而不订阅 WSS、避免重复收消息。
type Leader interface {
	IsLeader(ctx context.Context) bool
}

// AlwaysLeader 单副本部署的领导权实现：恒为 leader。
type AlwaysLeader struct{}

func (AlwaysLeader) IsLeader(context.Context) bool { return true }

var _ Leader = AlwaysLeader{}