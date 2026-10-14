package vault

// Status 笔记的生命周期状态。
type Status string

const (
	StatusPending  Status = "pending"  // 隔离区，未 review，不进检索
	StatusPromoted Status = "promoted" // 正式知识，进检索
)

// Zone 笔记所在目录分区。
type Zone int

const (
	zonePending  Zone = iota // _agent_notes/ 隔离区
	zonePromoted              // ops/ 正式经验
	zoneMine                  // mine/ 个人第二大脑
	zoneShared                // shared/ 组织共享
	zoneUnknown
)

// Note 一条经验笔记。
type Note struct {
	Path    string   // 文件绝对路径
	Zone    Zone     // 目录分区
	Type    string   // experience | case | tool-usage | workflow | note
	Status  Status   // pending | promoted
	Tags    []string
	Source  string   // 溯源，如 session:<uuid>
	Created string
	Body    string   // frontmatter 之后的正文
}
