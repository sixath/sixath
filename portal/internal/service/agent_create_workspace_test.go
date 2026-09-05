package service

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	agentv1 "backend/api/agent/v1"
	"backend/internal/biz"

	kratosErrors "github.com/go-kratos/kratos/v2/errors"
	"github.com/go-kratos/kratos/v2/log"
)

func TestCreateAgent_RejectsWholeRepoWorkspace(t *testing.T) {
	root := t.TempDir()
	repoDir := filepath.Join(root, "repo")
	if err := os.Mkdir(repoDir, 0o755); err != nil {
		t.Fatal(err)
	}
	svc, _ := newHybridUpdateAgentService(t, biz.RuntimeToolsConfig{})
	svc.codeRoots = []string{root}
	ctx := biz.WithCallerUserID(context.Background(), "owner")
	_, err := svc.CreateAgent(ctx, &agentv1.CreateAgentRequest{
		Name:      "x",
		Workspace: repoDir,
		ModelConfig: &agentv1.ModelConfig{
			Provider: "openai",
			Model:    "gpt-4",
		},
	})
	if err == nil {
		t.Fatal("expected whole-repo create to fail")
	}
	if kratosErrors.FromError(err).Reason != "WORKSPACE_WHOLE_REPO_RETIRED" {
		t.Fatalf("reason = %q, err=%v", kratosErrors.FromError(err).Reason, err)
	}
}

func TestChat_RejectsEmptyWorkspace(t *testing.T) {
	const agentID = "agent-empty-ws"
	repo := &hybridAgentRepo{agent: &biz.AgentMeta{
		ID: agentID, Name: "a", Workspace: "  ",
		ModelConfig: biz.ModelConfig{Provider: "openai", Model: "gpt-4"},
	}}
	res := &hybridResourceRepo{res: &biz.Resource{
		ID: "res-1", Type: biz.ResourceTypeAgent, PayloadRef: agentID,
		OwnerUserID: "owner", Visibility: biz.VisibilityPrivate,
	}}
	uc := biz.NewAgentUsecase(repo, res, biz.NewAccessChecker(res), t.TempDir(), log.NewStdLogger(nil))
	svc := NewAgentService(uc, nil, nil, nil, nil, nil, log.NewStdLogger(nil))
	ctx := biz.WithCallerUserID(context.Background(), "owner")
	_, err := svc.Chat(ctx, &agentv1.ChatRequest{Id: agentID, Content: "hi"})
	if err == nil {
		t.Fatal("expected empty workspace to fail")
	}
	if kratosErrors.FromError(err).Reason != "WORKSPACE_REQUIRED" {
		t.Fatalf("reason = %q, err=%v", kratosErrors.FromError(err).Reason, err)
	}
}

func TestChat_RejectsWholeRepoWorkspace(t *testing.T) {
	root := t.TempDir()
	ws := filepath.Join(root, "repo")
	if err := os.Mkdir(ws, 0o755); err != nil {
		t.Fatal(err)
	}
	const agentID = "agent-whole-repo"
	repo := &hybridAgentRepo{agent: &biz.AgentMeta{
		ID: agentID, Name: "a", Workspace: ws,
		ModelConfig: biz.ModelConfig{Provider: "openai", Model: "gpt-4"},
	}}
	res := &hybridResourceRepo{res: &biz.Resource{
		ID: "res-1", Type: biz.ResourceTypeAgent, PayloadRef: agentID,
		OwnerUserID: "owner", Visibility: biz.VisibilityPrivate,
	}}
	uc := biz.NewAgentUsecase(repo, res, biz.NewAccessChecker(res), t.TempDir(), log.NewStdLogger(nil))
	svc := NewAgentService(uc, nil, nil, nil, nil, []string{root}, log.NewStdLogger(nil))
	ctx := biz.WithCallerUserID(context.Background(), "owner")
	_, err := svc.Chat(ctx, &agentv1.ChatRequest{Id: agentID, Content: "hi"})
	if err == nil {
		t.Fatal("expected whole-repo chat to fail")
	}
	if kratosErrors.FromError(err).Reason != "WORKSPACE_WHOLE_REPO_RETIRED" {
		t.Fatalf("reason = %q, err=%v", kratosErrors.FromError(err).Reason, err)
	}
}

func TestExecuteSkill_RejectsWholeRepoWorkspace(t *testing.T) {
	root := t.TempDir()
	ws := filepath.Join(root, "repo")
	if err := os.Mkdir(ws, 0o755); err != nil {
		t.Fatal(err)
	}
	const agentID = "agent-skill-whole-repo"
	repo := &hybridAgentRepo{agent: &biz.AgentMeta{
		ID: agentID, Name: "a", Workspace: ws,
	}}
	res := &hybridResourceRepo{res: &biz.Resource{
		ID: "res-1", Type: biz.ResourceTypeAgent, PayloadRef: agentID,
		OwnerUserID: "owner", Visibility: biz.VisibilityPrivate,
	}}
	uc := biz.NewAgentUsecase(repo, res, biz.NewAccessChecker(res), t.TempDir(), log.NewStdLogger(nil))
	svc := NewAgentService(uc, nil, nil, nil, nil, []string{root}, log.NewStdLogger(nil))
	ctx := biz.WithCallerUserID(context.Background(), "owner")
	_, err := svc.ExecuteSkill(ctx, &agentv1.ExecuteSkillRequest{
		Id:   agentID,
		Path: "demo/scripts/run.sh",
	})
	if err == nil {
		t.Fatal("expected whole-repo execute skill to fail")
	}
	if kratosErrors.FromError(err).Reason != "WORKSPACE_WHOLE_REPO_RETIRED" {
		t.Fatalf("reason = %q, err=%v", kratosErrors.FromError(err).Reason, err)
	}
}
