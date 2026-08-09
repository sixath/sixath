package memory

import (
	"context"
	"testing"
)

func TestSessionMemory_UserScopeIsolatedFromSession(t *testing.T) {
	m := NewSessionMemory()
	ctx := context.Background()
	if _, err := m.Remember(ctx, RememberInput{
		Scope: ScopeUser, ScopeID: "u1", Action: ActionAdd, Content: "prefers dark mode",
	}); err != nil {
		t.Fatalf("Remember user: %v", err)
	}
	if _, err := m.Remember(ctx, RememberInput{
		Scope: ScopeSession, ScopeID: "s1", Action: ActionAdd, Content: "session only",
	}); err != nil {
		t.Fatalf("Remember session: %v", err)
	}
	hits, err := m.Recall(ctx, RecallQuery{Scope: ScopeUser, ScopeID: "u1", Query: "dark", Source: SourceUnits})
	if err != nil || len(hits) != 1 || hits[0].Scope != ScopeUser {
		t.Fatalf("user Recall = %+v err=%v", hits, err)
	}
	sess, err := m.Recall(ctx, RecallQuery{Scope: ScopeSession, ScopeID: "s1", Query: "session", Source: SourceUnits})
	if err != nil || len(sess) != 1 || sess[0].Scope != ScopeSession {
		t.Fatalf("session Recall = %+v err=%v", sess, err)
	}
}
