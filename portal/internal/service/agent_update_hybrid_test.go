package service

import (
	"context"
	"testing"

	agentv1 "backend/api/agent/v1"
	"backend/internal/biz"
	pkgErrors "backend/internal/pkg/errors"

	"github.com/go-kratos/kratos/v2/log"
)

// hybridAgentRepo is a minimal AgentRepo that applies runtime_tools updates in-place.
type hybridAgentRepo struct {
	agent *biz.AgentMeta
}

func (r *hybridAgentRepo) Create(context.Context, string, string, string, string, string, biz.ModelConfig, bool, string, biz.RuntimeToolsConfig, []string) (*biz.AgentMeta, error) {
	return nil, pkgErrors.ErrNotFound
}
func (r *hybridAgentRepo) CountByWecomChannelID(context.Context, string) (int, error) {
	return 0, nil
}
func (r *hybridAgentRepo) GetByID(_ context.Context, id string) (*biz.AgentMeta, error) {
	if r.agent == nil || r.agent.ID != id {
		return nil, pkgErrors.ErrNotFound
	}
	cp := *r.agent
	rt := r.agent.RuntimeTools
	if rt.HybridRecall != nil {
		v := *rt.HybridRecall
		rt.HybridRecall = &v
	}
	cp.RuntimeTools = rt
	return &cp, nil
}
func (r *hybridAgentRepo) GetByName(context.Context, string) (*biz.AgentMeta, error) {
	return nil, pkgErrors.ErrNotFound
}
func (r *hybridAgentRepo) List(context.Context, int32, int32) ([]*biz.AgentMeta, int, error) {
	return nil, 0, nil
}
func (r *hybridAgentRepo) ListByIDs(context.Context, []string, int32, int32) ([]*biz.AgentMeta, int, error) {
	return nil, 0, nil
}
func (r *hybridAgentRepo) Update(_ context.Context, id string, updates map[string]any) (*biz.AgentMeta, error) {
	if r.agent == nil || r.agent.ID != id {
		return nil, pkgErrors.ErrNotFound
	}
	if v, ok := updates["runtime_tools"].(biz.RuntimeToolsConfig); ok {
		r.agent.RuntimeTools = v
	}
	if v, ok := updates["name"].(string); ok {
		r.agent.Name = v
	}
	return r.GetByID(context.Background(), id)
}
func (r *hybridAgentRepo) Delete(context.Context, string) error { return nil }
func (r *hybridAgentRepo) BindTools(context.Context, string, []string) error {
	return nil
}
func (r *hybridAgentRepo) UnbindTools(context.Context, string, []string) error {
	return nil
}
func (r *hybridAgentRepo) ListDistinctWorkspaces(context.Context, int) ([]biz.CuratorWorkspace, error) {
	return nil, nil
}
func (r *hybridAgentRepo) ListAgentIDsByWorkspace(context.Context, string) ([]string, error) {
	return nil, nil
}

type hybridResourceRepo struct {
	res *biz.Resource
}

func (r *hybridResourceRepo) GetResource(_ context.Context, id string) (*biz.Resource, error) {
	if r.res == nil || r.res.ID != id {
		return nil, pkgErrors.ErrNotFound
	}
	return r.res, nil
}
func (r *hybridResourceRepo) ListGrants(context.Context, string) ([]biz.ResourceGrant, error) {
	return nil, nil
}
func (r *hybridResourceRepo) UserOrgIDs(context.Context, string) ([]string, error) {
	return nil, nil
}
func (r *hybridResourceRepo) CreateResource(context.Context, *biz.Resource) (*biz.Resource, error) {
	return nil, nil
}
func (r *hybridResourceRepo) UpdateResource(context.Context, *biz.Resource) error { return nil }
func (r *hybridResourceRepo) DeleteResource(context.Context, string) error         { return nil }
func (r *hybridResourceRepo) GetByPayload(_ context.Context, resourceType biz.ResourceType, payloadRef string) (*biz.Resource, error) {
	if r.res == nil || r.res.Type != resourceType || r.res.PayloadRef != payloadRef {
		return nil, pkgErrors.ErrNotFound
	}
	return r.res, nil
}
func (r *hybridResourceRepo) ListAllByType(context.Context, biz.ResourceType) ([]*biz.Resource, error) {
	return nil, nil
}
func (r *hybridResourceRepo) ListGrantsByResourceIDs(context.Context, []string) (map[string][]biz.ResourceGrant, error) {
	return map[string][]biz.ResourceGrant{}, nil
}
func (r *hybridResourceRepo) CreateGrant(context.Context, biz.ResourceGrant) error { return nil }

func newHybridUpdateAgentService(t *testing.T, stored biz.RuntimeToolsConfig) (*AgentService, *hybridAgentRepo) {
	t.Helper()
	const agentID = "agent-1"
	repo := &hybridAgentRepo{agent: &biz.AgentMeta{
		ID: agentID, Name: "a", RuntimeTools: stored,
	}}
	res := &hybridResourceRepo{res: &biz.Resource{
		ID: "res-1", Type: biz.ResourceTypeAgent, PayloadRef: agentID,
		OwnerUserID: "owner", Visibility: biz.VisibilityPrivate,
	}}
	uc := biz.NewAgentUsecase(repo, res, biz.NewAccessChecker(res), "/tmp", log.NewStdLogger(nil))
	return NewAgentService(uc, nil, nil, nil, nil, nil, log.NewStdLogger(nil)), repo
}

func TestUpdateAgent_OmitsHybridRecall_PreservesStored(t *testing.T) {
	f := false
	svc, repo := newHybridUpdateAgentService(t, biz.RuntimeToolsConfig{
		TodoEnabled:  true,
		HybridRecall: &f,
	})
	ctx := biz.WithCallerUserID(context.Background(), "owner")

	reply, err := svc.UpdateAgent(ctx, &agentv1.UpdateAgentRequest{
		Id: "agent-1",
		RuntimeTools: &agentv1.RuntimeToolsConfig{
			TodoEnabled: false, // whole-replace other bools; hybrid_recall intentionally unset
		},
	})
	if err != nil {
		t.Fatalf("UpdateAgent: %v", err)
	}
	if repo.agent.RuntimeTools.TodoEnabled {
		t.Fatal("other runtime_tools bools must still be whole-replaced")
	}
	if repo.agent.RuntimeTools.HybridRecall == nil || *repo.agent.RuntimeTools.HybridRecall {
		t.Fatalf("omit hybrid_recall must preserve stored false, got %+v", repo.agent.RuntimeTools.HybridRecall)
	}
	if reply.GetRuntimeTools().HybridRecall == nil || *reply.GetRuntimeTools().HybridRecall {
		t.Fatalf("reply must keep hybrid_recall=false, got %+v", reply.GetRuntimeTools().HybridRecall)
	}

	tr := true
	reply, err = svc.UpdateAgent(ctx, &agentv1.UpdateAgentRequest{
		Id: "agent-1",
		RuntimeTools: &agentv1.RuntimeToolsConfig{
			HybridRecall: &tr,
		},
	})
	if err != nil {
		t.Fatalf("UpdateAgent explicit true: %v", err)
	}
	if repo.agent.RuntimeTools.HybridRecall == nil || !*repo.agent.RuntimeTools.HybridRecall {
		t.Fatalf("explicit true must set hybrid_recall=true, got %+v", repo.agent.RuntimeTools.HybridRecall)
	}
	if reply.GetRuntimeTools().HybridRecall == nil || !*reply.GetRuntimeTools().HybridRecall {
		t.Fatalf("reply must show hybrid_recall=true, got %+v", reply.GetRuntimeTools().HybridRecall)
	}
}
