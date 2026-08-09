package biz

import (
	"context"
	"testing"
)

type fakeAnchoredBackend struct {
	hits []AnchoredHit
	err  error
	opts TranscriptSearchOpts
	n    int
}

func (f *fakeAnchoredBackend) SearchSessions(context.Context, []string, string, int) []SessionSearchCandidate {
	return nil
}

func (f *fakeAnchoredBackend) SearchAnchored(_ context.Context, opts TranscriptSearchOpts) ([]AnchoredHit, error) {
	f.n++
	f.opts = opts
	return f.hits, f.err
}

func TestSearchTranscript_RequiresAgentView(t *testing.T) {
	agents := &fakeAgentACLRepo{agents: map[string]*AgentMeta{
		"agent-1": {ID: "agent-1"},
	}}
	resources := &fakeAgentResourceRepo{
		fakeResourceReader: fakeResourceReader{
			resources: map[string]*Resource{},
			grants:    map[string][]ResourceGrant{},
			userOrgs:  map[string][]string{},
		},
		byPayload: map[string]*Resource{},
	}
	resource := &Resource{ID: "agent-resource", Type: ResourceTypeAgent, PayloadRef: "agent-1", OwnerUserID: "owner", Visibility: VisibilityPrivate}
	resources.resources[resource.ID] = resource
	resources.byPayload["agent:agent-1"] = resource
	resources.grants[resource.ID] = []ResourceGrant{{ResourceID: resource.ID, GranteeType: "user", GranteeID: "viewer", Perm: PermView}}

	backend := &fakeAnchoredBackend{hits: []AnchoredHit{{SessionID: "s1", Title: "t"}}}
	uc := NewChatUsecase(&fakeChatSessionRepo{sessions: map[string]*ChatSession{}}, nil, agents, resources, NewAccessChecker(resources))
	uc.SetSessionSearchBackend(backend)

	// View-only caller is allowed (unlike SearchSessions which needs use).
	out, err := uc.SearchTranscript(WithCallerUserID(context.Background(), "viewer"), TranscriptSearchOpts{
		AgentID:      "agent-1",
		Query:        "needle",
		IncludeTools: true,
		Window:       5,
	})
	if err != nil {
		t.Fatalf("SearchTranscript view-only: %v", err)
	}
	if out.Count != 1 || len(out.Hits) != 1 {
		t.Fatalf("result=%+v", out)
	}
	if backend.n != 1 || backend.opts.Query != "needle" {
		t.Fatalf("backend opts=%+v calls=%d", backend.opts, backend.n)
	}

	if _, err := uc.SearchTranscript(WithCallerUserID(context.Background(), "stranger"), TranscriptSearchOpts{
		AgentID: "agent-1",
		Query:   "needle",
	}); !isReason(err, "AGENT_NOT_FOUND") {
		t.Fatalf("stranger error=%v want AGENT_NOT_FOUND", err)
	}
}

func TestSearchTranscript_EmptyQuery(t *testing.T) {
	agents := &fakeAgentACLRepo{agents: map[string]*AgentMeta{
		"agent-1": {ID: "agent-1"},
	}}
	resources := &fakeAgentResourceRepo{
		fakeResourceReader: fakeResourceReader{
			resources: map[string]*Resource{},
			grants:    map[string][]ResourceGrant{},
			userOrgs:  map[string][]string{},
		},
		byPayload: map[string]*Resource{},
	}
	resource := &Resource{ID: "agent-resource", Type: ResourceTypeAgent, PayloadRef: "agent-1", OwnerUserID: "owner", Visibility: VisibilityPrivate}
	resources.resources[resource.ID] = resource
	resources.byPayload["agent:agent-1"] = resource
	resources.grants[resource.ID] = []ResourceGrant{{ResourceID: resource.ID, GranteeType: "user", GranteeID: "owner", Perm: PermUse}}

	backend := &fakeAnchoredBackend{hits: []AnchoredHit{{SessionID: "s1"}}}
	uc := NewChatUsecase(&fakeChatSessionRepo{sessions: map[string]*ChatSession{}}, nil, agents, resources, NewAccessChecker(resources))
	uc.SetSessionSearchBackend(backend)

	out, err := uc.SearchTranscript(WithCallerUserID(context.Background(), "owner"), TranscriptSearchOpts{
		AgentID: "agent-1",
		Query:   "  ",
	})
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if out.Count != 0 || len(out.Hits) != 0 {
		t.Fatalf("empty query should short-circuit: %+v", out)
	}
	if backend.n != 0 {
		t.Fatalf("backend should not be called for empty query, calls=%d", backend.n)
	}
}
