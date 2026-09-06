package workspace

// 约定目录（契约，不是新进程）。Portal 仍负责创建 {data_root}/agents/{id}/。
const (
	SkillsDir    = "skills"
	HooksFileRel = "harness/hooks.yaml"
	MemoryFile   = "MEMORY.md"
	UserFile     = "USER.md"
	CodeDir      = "code"
)
