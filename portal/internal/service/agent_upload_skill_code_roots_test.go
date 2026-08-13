package service

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	agentv1 "backend/api/agent/v1"
	"backend/internal/biz"

	"github.com/go-kratos/kratos/v2/log"
)

func TestUploadSkillPackage_RejectsWorkspaceUnderCodeRoots(t *testing.T) {
	root := t.TempDir()
	ws := filepath.Join(root, "agent-ws")
	if err := os.MkdirAll(ws, 0o755); err != nil {
		t.Fatal(err)
	}

	const agentID = "agent-1"
	repo := &hybridAgentRepo{agent: &biz.AgentMeta{
		ID: agentID, Name: "a", Workspace: ws,
	}}
	res := &hybridResourceRepo{res: &biz.Resource{
		ID: "res-1", Type: biz.ResourceTypeAgent, PayloadRef: agentID,
		OwnerUserID: "owner", Visibility: biz.VisibilityPrivate,
	}}
	uc := biz.NewAgentUsecase(repo, res, biz.NewAccessChecker(res), "/tmp", log.NewStdLogger(nil))
	svc := NewAgentService(uc, nil, nil, nil, nil, []string{root}, log.NewStdLogger(nil))

	ctx := biz.WithCallerUserID(context.Background(), "owner")
	reply, err := svc.UploadSkillPackage(ctx, &agentv1.UploadSkillPackageRequest{
		Id:   agentID,
		File: []byte("not-a-real-zip"),
	})
	if err != nil {
		t.Fatalf("UploadSkillPackage: %v", err)
	}
	if reply.Success {
		t.Fatal("expected Success=false")
	}
	if reply.Ret == nil || reply.Ret.Code != 400 {
		t.Fatalf("Ret = %+v, want code 400", reply.Ret)
	}
	want := "workspace is under read-only code root; use subdirectory mode (workspace/code)"
	if reply.Message != want {
		t.Fatalf("Message = %q, want %q", reply.Message, want)
	}
	if reply.Ret.Message != want {
		t.Fatalf("Ret.Message = %q, want %q", reply.Ret.Message, want)
	}
}
