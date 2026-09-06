package chat

import (
	"os"
	"strings"

	"github.com/sixath/framework/skills"
	"github.com/sixath/framework/tool"
	toolskill "github.com/sixath/framework/tool/skillops"
)

// SkillManageConfirmCreateDelete 控制 create/delete 是否走两阶段确认（默认 true）。
var SkillManageConfirmCreateDelete = true

// SkillManageConfirmPatch 控制 patch/edit/write_file/remove_file 是否走两阶段确认（默认 true，G4 主对话人审）。
// Growth fork registry 应显式设 RequirePatchConfirm=false。
var SkillManageConfirmPatch = true

// SetSkillManageConfirmCreateDelete 由 main 根据 config / 环境变量设置。
func SetSkillManageConfirmCreateDelete(v bool) {
	SkillManageConfirmCreateDelete = v
}

// SetSkillManageConfirmPatch 由 env / flags 设置。
func SetSkillManageConfirmPatch(v bool) {
	SkillManageConfirmPatch = v
}

var defaultSkillManagePending = toolskill.NewInMemorySkillManagePendingStore()

// SkillManagePendingStore 返回进程内共享 pending store。
func SkillManagePendingStore() toolskill.SkillManagePendingStore {
	return defaultSkillManagePending
}

// SkillManageToolConfig 构造 RegisterSkillManageTool 所需配置（主对话：create/delete + patch confirm）。
func SkillManageToolConfig(idx *skills.Index) *toolskill.SkillManageConfig {
	return &toolskill.SkillManageConfig{
		Index:                      idx,
		PendingStore:               defaultSkillManagePending,
		TokenGen:                   tool.RandomTokenGenerator{},
		RequireCreateDeleteConfirm: SkillManageConfirmCreateDelete,
		RequirePatchConfirm:        SkillManageConfirmPatch,
		RequireUIConfirm:           true,
		ConfirmTTLSeconds:          300,
	}
}

// SkillManageToolConfigForGrowthReview 复盘 fork 用：create/delete 仍可确认策略，但 patch 直写（无 SSE UI）。
func SkillManageToolConfigForGrowthReview(idx *skills.Index) *toolskill.SkillManageConfig {
	cfg := SkillManageToolConfig(idx)
	cfg.RequireCreateDeleteConfirm = false
	cfg.RequirePatchConfirm = false
	return cfg
}

// EnrichSkillManageConfirmFromEnv reads SATH_SKILL_MANAGE_CONFIRM_PATCH (1/true/yes).
func EnrichSkillManageConfirmFromEnv() {
	if v := strings.TrimSpace(os.Getenv("SATH_SKILL_MANAGE_CONFIRM_PATCH")); v != "" {
		lv := strings.ToLower(v)
		SkillManageConfirmPatch = lv == "1" || lv == "true" || lv == "yes"
	}
}
