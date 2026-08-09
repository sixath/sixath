package biz

import (
	"context"
	"testing"
)

type fakeChatSessionRepo struct {
	sessions map[string]*ChatSession
}

func (f *fakeChatSessionRepo) Create(_ context.Context, userID, agentID, title, parentSessionID string) (*ChatSession, error) {
	session := &ChatSession{ID: "new-session", UserID: userID, AgentID: agentID, Title: title, ParentSessionID: parentSessionID}
	f.sessions[session.ID] = session
	return session, nil
}

func (f *fakeChatSessionRepo) GetByID(_ context.Context, id string) (*ChatSession, error) {
	session, ok := f.sessions[id]
	if !ok {
		return nil, errNotFound
	}
	return session, nil
}

func (f *fakeChatSessionRepo) ListByAgent(_ context.Context, userID, agentID, _ string, _, _ int32, _ bool) ([]*ChatSession, int, error) {
	var sessions []*ChatSession
	for _, session := range f.sessions {
		if session.UserID == userID && session.AgentID == agentID {
			sessions = append(sessions, session)
		}
	}
	return sessions, len(sessions), nil
}

func (f *fakeChatSessionRepo) ListAll(_ context.Context, userID string, _, _ int32, _ bool) ([]*ChatSession, int, error) {
	sessions := make([]*ChatSession, 0, len(f.sessions))
	for _, session := range f.sessions {
		if session.UserID == userID {
			sessions = append(sessions, session)
		}
	}
	return sessions, len(sessions), nil
}

func (f *fakeChatSessionRepo) Update(_ context.Context, id string, _ map[string]any) (*ChatSession, error) {
	return f.GetByID(context.Background(), id)
}

func (f *fakeChatSessionRepo) Delete(_ context.Context, id string) error {
	if _, ok := f.sessions[id]; !ok {
		return errNotFound
	}
	delete(f.sessions, id)
	return nil
}

func (f *fakeChatSessionRepo) Touch(context.Context, string) error { return nil }

func (f *fakeChatSessionRepo) BumpRewindCount(_ context.Context, sessionID string) error {
	s, ok := f.sessions[sessionID]
	if !ok {
		return errNotFound
	}
	s.RewindCount++
	return nil
}

func (f *fakeChatSessionRepo) MarkReadonly(_ context.Context, sessionID string) error {
	s, ok := f.sessions[sessionID]
	if !ok {
		return errNotFound
	}
	s.Readonly = true
	return nil
}

func TestChatSessionUserIsolation(t *testing.T) {
	repo := &fakeChatSessionRepo{
		sessions: map[string]*ChatSession{
			"session-a": {ID: "session-a", UserID: "user-a", AgentID: "agent-1", Title: "user A session"},
		},
	}
	uc := NewChatUsecase(repo, nil, nil, nil, nil)

	if _, err := uc.GetSession(WithCallerUserID(context.Background(), "user-b"), "session-a"); !isReason(err, "SESSION_NOT_FOUND") {
		t.Fatalf("GetSession by user B error = %v, want SESSION_NOT_FOUND", err)
	}

	if _, err := uc.GetSession(context.Background(), "session-a"); !isReason(err, "UNAUTHORIZED") {
		t.Fatalf("GetSession without caller error = %v, want UNAUTHORIZED", err)
	}
}

func TestCreateSessionRequiresAgentUse(t *testing.T) {
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

	uc := NewChatUsecase(&fakeChatSessionRepo{sessions: map[string]*ChatSession{}}, nil, agents, resources, NewAccessChecker(resources))
	ctx := WithCallerUserID(context.Background(), "viewer")
	if _, err := uc.CreateSession(ctx, "agent-1", "", ""); !isReason(err, "FORBIDDEN_PERM") {
		t.Fatalf("CreateSession without agent use error = %v, want FORBIDDEN_PERM", err)
	}
}

func TestSearchSessionsWithAgentFilterRequiresAgentUse(t *testing.T) {
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

	uc := NewChatUsecase(&fakeChatSessionRepo{sessions: map[string]*ChatSession{}}, nil, agents, resources, NewAccessChecker(resources))
	if _, _, err := uc.SearchSessions(WithCallerUserID(context.Background(), "viewer"), "needle", "agent-1", 1); !isReason(err, "FORBIDDEN_PERM") {
		t.Fatalf("SearchSessions with view-only agent error = %v, want FORBIDDEN_PERM", err)
	}
}
