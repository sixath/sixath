package service

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"backend/internal/chat"

	"github.com/sixath/framework/growth"
	"github.com/sixath/framework/skills"
	"github.com/sixath/framework/tool"
)

var errNoReviewModel = errors.New("growth: review model not configured")

const agentReviewSystemPrompt = `你是一名技能策展员（Skill Curator），运行在后台复盘环节。
你的目标：根据本次会话 transcript 与现有技能库，用工具把可复用的经验沉淀为 SKILL.md。
优先级链路：patch 现有技能 → 为其加 reference/template → 仅当确有新类别时才新建 umbrella 技能。
约束：
- 禁止把 PR 编号、错误字符串、一次性产物变成技能名。
- 禁止对已被 cron 定时任务引用的技能改名（改名会断链）。
- 若确无可沉淀内容，直接结束，不要强行制造变更。
可用工具：skill_view / skills_list 浏览现有技能；read_skill_file 读子文件；skill_manage 创建/修改/删除 SKILL.md。`

// spawnReviewAgent 实现 growth.RunnerDeps.SpawnReviewAgent（fork-agent 复盘路径）。
// 构造一个瘦身 ReActAgent，让它用 skillops 工具自主演化技能库；成功收尾清除 pending_skill。
func (w *GrowthWorker) spawnReviewAgent(ctx context.Context, job growth.ReviewJob, transcript, summary string) error {
	if err := w.runForkReviewAgent(ctx, job, nil, transcript, summary); err != nil {
		return err
	}
	return w.growthUC.ClearGrowthPending(w.internalContext(context.Background()), job.SessionID, true, false)
}

func (w *GrowthWorker) rewriteCronAfterForkReview(ctx context.Context, workspace string, beforeNames []string) error {
	afterNames, err := listWorkspaceSkillNames(workspace)
	if err != nil {
		return fmt.Errorf("growth: list skills after fork-agent review: %w", err)
	}
	renames, ok := growth.DetectOneToOneRename(beforeNames, afterNames)
	if ok {
		if w.cronRewrite == nil {
			return nil
		}
		return w.cronRewrite(ctx, workspace, renames)
	}
	if skillNameSetsDiffer(beforeNames, afterNames) && w.log != nil {
		w.log.Warnf("growth: fork-agent skill name changes are not 1:1; skipping cron skill ref rewrite")
	}
	return nil
}

func listWorkspaceSkillNames(workspace string) ([]string, error) {
	skillsDir := filepath.Join(workspace, "skills")
	if st, err := os.Stat(skillsDir); err != nil || !st.IsDir() {
		return nil, nil
	}
	idx, err := skills.NewIndex([]string{skillsDir}, nil, nil)
	if err != nil {
		return nil, err
	}
	all := idx.All()
	names := make([]string, 0, len(all))
	seen := make(map[string]struct{}, len(all))
	for _, m := range all {
		if m.Name == "" {
			continue
		}
		if _, ok := seen[m.Name]; ok {
			continue
		}
		seen[m.Name] = struct{}{}
		names = append(names, m.Name)
	}
	return names, nil
}

func skillNameSetsDiffer(before, after []string) bool {
	b := make(map[string]struct{}, len(before))
	for _, n := range before {
		if n != "" {
			b[n] = struct{}{}
		}
	}
	a := make(map[string]struct{}, len(after))
	for _, n := range after {
		if n != "" {
			a[n] = struct{}{}
		}
	}
	if len(a) != len(b) {
		return true
	}
	for n := range b {
		if _, hit := a[n]; !hit {
			return true
		}
	}
	return false
}

// buildReviewRegistry 组装复盘 agent 的瘦身工具集：
// 默认（full_tools=false）仅暴露 skillops 只读浏览 + skill_manage 写工具，
// 不含 shell/terminal 等通用执行类工具，最小化后台复盘的破坏面。
func (w *GrowthWorker) buildReviewRegistry(workspace string) (*tool.Registry, error) {
	skillsDir := workspace + "/skills"
	idx, err := skills.NewIndex([]string{skillsDir}, nil, nil)
	if err != nil {
		return nil, err
	}
	reg := tool.NewRegistry()
	// core：load_skill / read_skill_file（禁脚本执行：后台复盘不需要跑技能脚本）。
	if err := chat.RegisterSkillTools(reg, idx, nil, false); err != nil {
		return nil, err
	}
	// runtime（Hermes）：skills_list / skill_view / skill_manage —— 策展员的读写主力（patch 直写，无 UI confirm）。
	if err := chat.RegisterSkillRuntimeToolsWithManage(reg, idx, nil, chat.SkillManageToolConfigForGrowthReview(idx)); err != nil {
		return nil, err
	}
	// full-tools 追加通用工具集：portal 当前没有单一 "注册通用 agent 工具" 入口，
	// 默认路径不依赖它；此分支留待 phase-2 接入，避免在此发明新逻辑。
	// TODO(phase-2): full-tools 通用工具集入口待接入。
	return reg, nil
}
