package service

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"backend/internal/biz"
	"backend/internal/conf"

	"github.com/go-kratos/kratos/v2/log"
	"github.com/sixath/framework/growth"
	"github.com/sixath/framework/model"
	"github.com/sixath/framework/tool"
)

// fakeReviewModel 实现 agent.ToolCallingModel（= model.Model + ChatWithTools）。
// ChatWithTools 一步返回 final answer、不产生 tool_call，并记录收到的 ctx workspace。
type fakeReviewModel struct {
	gotWorkspace string
	gotCaller    string
	called       bool
}

func (f *fakeReviewModel) Generate(ctx context.Context, prompt string, opts ...model.Option) (*model.Generation, error) {
	return &model.Generation{Text: "ok"}, nil
}

func (f *fakeReviewModel) Chat(ctx context.Context, messages []model.Message, opts ...model.Option) (*model.Generation, error) {
	return &model.Generation{Text: "ok"}, nil
}

func (f *fakeReviewModel) Embed(ctx context.Context, texts []string, opts ...model.Option) ([]model.Embedding, error) {
	return nil, nil
}

func (f *fakeReviewModel) ChatWithTools(ctx context.Context, messages []model.Message, reg *tool.Registry, opts ...model.Option) (*model.Generation, error) {
	f.called = true
	if v, ok := ctx.Value(tool.ContextKeyWorkspaceRoot).(string); ok {
		f.gotWorkspace = v
	}
	if v, ok := biz.CallerUserID(ctx); ok {
		f.gotCaller = v
	}
	// 直接返回 final，不带 tool_call，使 ReAct 一步收敛。
	return &model.Generation{Text: "no changes needed"}, nil
}

// fakeGrowthRepoForService 实现 biz.GrowthRepo，让 ClearGrowthPending no-op 成功。
type fakeGrowthRepoForService struct {
	state *biz.ChatGrowthState
}

func (f *fakeGrowthRepoForService) GetState(ctx context.Context, sessionID string) (*biz.ChatGrowthState, error) {
	if f.state == nil || f.state.SessionID != sessionID {
		return &biz.ChatGrowthState{SessionID: sessionID}, nil
	}
	cp := *f.state
	return &cp, nil
}

func (f *fakeGrowthRepoForService) SaveState(ctx context.Context, st *biz.ChatGrowthState) error {
	if st == nil {
		return nil
	}
	cp := *st
	f.state = &cp
	return nil
}

func (f *fakeGrowthRepoForService) TryAcquireLease(ctx context.Context, workspaceKey, holderID string, ttl time.Duration) (bool, error) {
	return false, nil
}

func (f *fakeGrowthRepoForService) ReleaseLease(ctx context.Context, workspaceKey, holderID string) error {
	return nil
}

func (f *fakeGrowthRepoForService) ListPendingReviewSessions(ctx context.Context, limit int) ([]biz.GrowthPendingSession, error) {
	return nil, nil
}

func (f *fakeGrowthRepoForService) ListIdleSessions(ctx context.Context, idleInterval time.Duration, limit int) ([]biz.GrowthPendingSession, error) {
	return nil, nil
}

func TestSpawnReviewAgent_InjectsWorkspaceAndSkillManageTool(t *testing.T) {
	// 需要真实存在的 skills 目录，否则 skills.NewIndex 的 WalkDir 会报错。
	ws := t.TempDir()
	if err := os.MkdirAll(filepath.Join(ws, "skills"), 0o755); err != nil {
		t.Fatalf("mkdir skills: %v", err)
	}

	repo := &fakeGrowthRepoForService{state: &biz.ChatGrowthState{SessionID: "s1"}}
	uc := biz.NewGrowthUsecase(repo)

	fm := &fakeReviewModel{}
	w := &GrowthWorker{
		log:                    log.NewHelper(log.DefaultLogger),
		growthUC:               uc,
		growthCfg:              &conf.Growth{},
		reviewModel:            fm,
		servicePrincipalUserID: "service-principal",
	}

	job := growth.ReviewJob{
		SessionID:     "s1",
		WorkspaceKey:  ws,
		WorkspaceRoot: ws,
		PendingSkill:  true,
	}

	if err := w.spawnReviewAgent(context.Background(), job, "transcript", "summary"); err != nil {
		t.Fatalf("spawnReviewAgent: %v", err)
	}

	if !fm.called {
		t.Fatal("expected model.ChatWithTools to be called")
	}
	if fm.gotWorkspace != ws {
		t.Fatalf("expected ctx workspace %q, got %q", ws, fm.gotWorkspace)
	}
	if fm.gotCaller != "service-principal" {
		t.Fatalf("expected caller user ID %q, got %q", "service-principal", fm.gotCaller)
	}

	// 默认 full_tools=false：工具集含 skill_manage，不含 shell/terminal 类工具。
	reg, err := w.buildReviewRegistry(ws)
	if err != nil {
		t.Fatalf("buildReviewRegistry: %v", err)
	}
	names := map[string]bool{}
	for _, n := range reg.Names() {
		names[n] = true
	}
	if !names["skill_manage"] {
		t.Fatalf("expected skill_manage in tools, got %v", reg.Names())
	}
	for _, banned := range []string{"shell", "terminal", "bash", "execute_command"} {
		if names[banned] {
			t.Fatalf("did not expect %q in default review tools, got %v", banned, reg.Names())
		}
	}
}

// renameOnCallModel 在首次 ChatWithTools 时把 workspace 内技能 frontmatter name 从 old→new。
type renameOnCallModel struct {
	fakeReviewModel
	skillPath string
	newBody   string
}

func (f *renameOnCallModel) ChatWithTools(ctx context.Context, messages []model.Message, reg *tool.Registry, opts ...model.Option) (*model.Generation, error) {
	if err := os.WriteFile(f.skillPath, []byte(f.newBody), 0o644); err != nil {
		return nil, err
	}
	return f.fakeReviewModel.ChatWithTools(ctx, messages, reg, opts...)
}

func TestSpawnReviewAgent_RewritesCronOnOneToOneRename(t *testing.T) {
	ws := t.TempDir()
	skillDir := filepath.Join(ws, "skills", "daily-report")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	skillPath := filepath.Join(skillDir, "SKILL.md")
	oldBody := "---\nname: daily-report\ndescription: old\n---\n# Daily\n"
	newBody := "---\nname: daily-report-v2\ndescription: new\n---\n# Daily\n"
	if err := os.WriteFile(skillPath, []byte(oldBody), 0o644); err != nil {
		t.Fatalf("write skill: %v", err)
	}

	repo := &fakeGrowthRepoForService{state: &biz.ChatGrowthState{SessionID: "s-rename"}}
	uc := biz.NewGrowthUsecase(repo)

	var gotWorkspace string
	var gotRenames map[string]string
	rewriteCalls := 0

	fm := &renameOnCallModel{
		skillPath: skillPath,
		newBody:   newBody,
	}
	w := &GrowthWorker{
		log:         log.NewHelper(log.DefaultLogger),
		growthUC:    uc,
		growthCfg:   &conf.Growth{},
		reviewModel: fm,
		cronRewrite: func(ctx context.Context, workspaceKey string, renames map[string]string) error {
			rewriteCalls++
			gotWorkspace = workspaceKey
			gotRenames = renames
			return nil
		},
	}

	job := growth.ReviewJob{
		SessionID:     "s-rename",
		WorkspaceKey:  ws,
		WorkspaceRoot: ws,
		PendingSkill:  true,
	}
	if err := w.spawnReviewAgent(context.Background(), job, "transcript", "summary"); err != nil {
		t.Fatalf("spawnReviewAgent: %v", err)
	}
	if rewriteCalls != 1 {
		t.Fatalf("cronRewrite calls = %d, want 1", rewriteCalls)
	}
	if gotWorkspace != ws {
		t.Fatalf("workspace = %q, want %q", gotWorkspace, ws)
	}
	if gotRenames["daily-report"] != "daily-report-v2" {
		t.Fatalf("renames = %#v", gotRenames)
	}
}

func TestSpawnReviewAgent_SkipsCronRewriteWithoutOneToOne(t *testing.T) {
	ws := t.TempDir()
	if err := os.MkdirAll(filepath.Join(ws, "skills"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	repo := &fakeGrowthRepoForService{state: &biz.ChatGrowthState{SessionID: "s-skip"}}
	uc := biz.NewGrowthUsecase(repo)

	rewriteCalls := 0
	w := &GrowthWorker{
		log:         log.NewHelper(log.DefaultLogger),
		growthUC:    uc,
		growthCfg:   &conf.Growth{},
		reviewModel: &fakeReviewModel{},
		cronRewrite: func(ctx context.Context, workspaceKey string, renames map[string]string) error {
			rewriteCalls++
			return nil
		},
	}
	job := growth.ReviewJob{
		SessionID:     "s-skip",
		WorkspaceKey:  ws,
		WorkspaceRoot: ws,
		PendingSkill:  true,
	}
	if err := w.spawnReviewAgent(context.Background(), job, "transcript", "summary"); err != nil {
		t.Fatalf("spawnReviewAgent: %v", err)
	}
	if rewriteCalls != 0 {
		t.Fatalf("cronRewrite calls = %d, want 0", rewriteCalls)
	}
}

// corruptAfterCallModel 在复盘完成后破坏 SKILL.md，使事后 listWorkspaceSkillNames 失败。
type corruptAfterCallModel struct {
	fakeReviewModel
	skillPath string
}

func (f *corruptAfterCallModel) ChatWithTools(ctx context.Context, messages []model.Message, reg *tool.Registry, opts ...model.Option) (*model.Generation, error) {
	if err := os.WriteFile(f.skillPath, []byte("---\nonly one delimiter\n"), 0o644); err != nil {
		return nil, err
	}
	return f.fakeReviewModel.ChatWithTools(ctx, messages, reg, opts...)
}

func TestSpawnReviewAgent_FailsWhenAfterListSkillNamesFails(t *testing.T) {
	ws := t.TempDir()
	skillDir := filepath.Join(ws, "skills", "daily-report")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	skillPath := filepath.Join(skillDir, "SKILL.md")
	validBody := "---\nname: daily-report\ndescription: ok\n---\n# Daily\n"
	if err := os.WriteFile(skillPath, []byte(validBody), 0o644); err != nil {
		t.Fatalf("write skill: %v", err)
	}

	repo := &fakeGrowthRepoForService{state: &biz.ChatGrowthState{
		SessionID:          "s-after-fail",
		PendingSkillReview: true,
	}}
	uc := biz.NewGrowthUsecase(repo)

	rewriteCalls := 0
	w := &GrowthWorker{
		log:         log.NewHelper(log.DefaultLogger),
		growthUC:    uc,
		growthCfg:   &conf.Growth{},
		reviewModel: &corruptAfterCallModel{skillPath: skillPath},
		cronRewrite: func(ctx context.Context, workspaceKey string, renames map[string]string) error {
			rewriteCalls++
			return nil
		},
	}
	job := growth.ReviewJob{
		SessionID:     "s-after-fail",
		WorkspaceKey:  ws,
		WorkspaceRoot: ws,
		PendingSkill:  true,
	}
	err := w.spawnReviewAgent(context.Background(), job, "transcript", "summary")
	if err == nil {
		t.Fatal("expected error when after-list fails")
	}
	if !strings.Contains(err.Error(), "list skills after fork-agent review") {
		t.Fatalf("unexpected error: %v", err)
	}
	if rewriteCalls != 0 {
		t.Fatalf("cronRewrite calls = %d, want 0", rewriteCalls)
	}
	if !repo.state.PendingSkillReview {
		t.Fatal("pending skill should remain when after-list fails")
	}
}
