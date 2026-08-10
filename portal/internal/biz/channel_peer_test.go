package biz

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	pkgErrors "backend/internal/pkg/errors"
)

type fakeChannelPeerSessionRepo struct {
	rows map[string]*ChannelPeerSession
}

func peerKey(channelID, peerID string) string {
	return channelID + "\x00" + peerID
}

func (f *fakeChannelPeerSessionRepo) Get(_ context.Context, channelID, peerID string) (*ChannelPeerSession, error) {
	row, ok := f.rows[peerKey(channelID, peerID)]
	if !ok {
		return nil, pkgErrors.ErrNotFound
	}
	cp := *row
	return &cp, nil
}

func (f *fakeChannelPeerSessionRepo) Create(_ context.Context, row *ChannelPeerSession) error {
	if f.rows == nil {
		f.rows = map[string]*ChannelPeerSession{}
	}
	key := peerKey(row.ChannelID, row.PeerID)
	if _, ok := f.rows[key]; ok {
		return pkgErrors.ErrConflict
	}
	cp := *row
	f.rows[key] = &cp
	return nil
}

func (f *fakeChannelPeerSessionRepo) Upsert(_ context.Context, row *ChannelPeerSession) error {
	if f.rows == nil {
		f.rows = map[string]*ChannelPeerSession{}
	}
	key := peerKey(row.ChannelID, row.PeerID)
	cp := *row
	if existing, ok := f.rows[key]; ok {
		cp.CreatedAt = existing.CreatedAt
	}
	f.rows[key] = &cp
	return nil
}

func (f *fakeChannelPeerSessionRepo) Delete(_ context.Context, channelID, peerID string) error {
	if f.rows == nil {
		return pkgErrors.ErrNotFound
	}
	key := peerKey(channelID, peerID)
	if _, ok := f.rows[key]; !ok {
		return pkgErrors.ErrNotFound
	}
	delete(f.rows, key)
	return nil
}

type fakeChatSessionRepoForPeer struct {
	sessions map[string]*ChatSession
	seq      int
}

func (f *fakeChatSessionRepoForPeer) Create(_ context.Context, userID, agentID, title, parentSessionID string) (*ChatSession, error) {
	if f.sessions == nil {
		f.sessions = map[string]*ChatSession{}
	}
	f.seq++
	id := fmt.Sprintf("session-%d", f.seq)
	s := &ChatSession{
		ID:              id,
		UserID:          userID,
		AgentID:         agentID,
		Title:           title,
		ParentSessionID: parentSessionID,
		CreatedAt:       time.Now(),
		UpdatedAt:       time.Now(),
	}
	f.sessions[id] = s
	return s, nil
}

func (f *fakeChatSessionRepoForPeer) GetByID(_ context.Context, id string) (*ChatSession, error) {
	s, ok := f.sessions[id]
	if !ok {
		return nil, pkgErrors.ErrNotFound
	}
	return s, nil
}

func (f *fakeChatSessionRepoForPeer) ListByAgent(context.Context, string, string, string, int32, int32, bool) ([]*ChatSession, int, error) {
	return nil, 0, nil
}
func (f *fakeChatSessionRepoForPeer) ListAll(context.Context, string, int32, int32, bool) ([]*ChatSession, int, error) {
	return nil, 0, nil
}
func (f *fakeChatSessionRepoForPeer) Update(context.Context, string, map[string]any) (*ChatSession, error) {
	return nil, pkgErrors.ErrNotFound
}
func (f *fakeChatSessionRepoForPeer) Delete(_ context.Context, id string) error {
	delete(f.sessions, id)
	return nil
}
func (f *fakeChatSessionRepoForPeer) Touch(context.Context, string) error  { return nil }
func (f *fakeChatSessionRepoForPeer) BumpRewindCount(context.Context, string) error {
	return nil
}
func (f *fakeChatSessionRepoForPeer) MarkReadonly(context.Context, string) error { return nil }

func TestChannelPeerResolve_SameKeySameSession(t *testing.T) {
	peerRepo := &fakeChannelPeerSessionRepo{rows: map[string]*ChannelPeerSession{}}
	sessionRepo := &fakeChatSessionRepoForPeer{}
	uc := NewChannelPeerUsecase(peerRepo, sessionRepo)

	r1, err := uc.Resolve(context.Background(), "ch-1", "peer-a", "agent-a")
	if err != nil {
		t.Fatalf("first Resolve: %v", err)
	}
	if !r1.Created {
		t.Fatalf("first Resolve: want created=true")
	}
	if r1.SessionID == "" {
		t.Fatalf("first Resolve: empty session_id")
	}
	if r1.AgentID != "agent-a" {
		t.Fatalf("first Resolve agent: got %q want agent-a", r1.AgentID)
	}

	r2, err := uc.Resolve(context.Background(), "ch-1", "peer-a", "agent-a")
	if err != nil {
		t.Fatalf("second Resolve: %v", err)
	}
	if r2.Created {
		t.Fatalf("second Resolve: want created=false")
	}
	if r2.SessionID != r1.SessionID {
		t.Fatalf("second Resolve session: got %q want %q", r2.SessionID, r1.SessionID)
	}

	r3, err := uc.Resolve(context.Background(), "ch-1", "peer-a", "agent-b")
	if err != nil {
		t.Fatalf("Resolve with new agent: %v", err)
	}
	if r3.SessionID != r1.SessionID {
		t.Fatalf("agent-b Resolve session: got %q want %q", r3.SessionID, r1.SessionID)
	}
	if r3.Created {
		t.Fatalf("agent-b Resolve: want created=false")
	}
	if r3.AgentID != "agent-a" {
		t.Fatalf("agent unchanged: got %q want agent-a", r3.AgentID)
	}
}

func TestChannelPeerResolve_DifferentPeerDifferentSession(t *testing.T) {
	peerRepo := &fakeChannelPeerSessionRepo{rows: map[string]*ChannelPeerSession{}}
	sessionRepo := &fakeChatSessionRepoForPeer{}
	uc := NewChannelPeerUsecase(peerRepo, sessionRepo)

	r1, err := uc.Resolve(context.Background(), "ch-1", "peer-1", "agent-a")
	if err != nil {
		t.Fatalf("peer-1 Resolve: %v", err)
	}
	r2, err := uc.Resolve(context.Background(), "ch-1", "peer-2", "agent-a")
	if err != nil {
		t.Fatalf("peer-2 Resolve: %v", err)
	}
	if r1.SessionID == r2.SessionID {
		t.Fatalf("different peers must get different sessions, both %q", r1.SessionID)
	}
}

func TestChannelPeerResolve_MappingCreateConflictReturnsExisting(t *testing.T) {
	winner := &ChannelPeerSession{
		ChannelID: "ch-1",
		PeerID:    "peer-a",
		SessionID: "session-winner",
		AgentID:   "agent-winner",
	}
	peerRepo := &conflictCreatePeerRepo{winner: winner}
	sessionRepo := &fakeChatSessionRepoForPeer{}
	uc := NewChannelPeerUsecase(peerRepo, sessionRepo)

	r, err := uc.Resolve(context.Background(), "ch-1", "peer-a", "agent-a")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if r.Created {
		t.Fatalf("want created=false after mapping conflict")
	}
	if r.SessionID != "session-winner" {
		t.Fatalf("session: got %q want session-winner", r.SessionID)
	}
	if r.AgentID != "agent-winner" {
		t.Fatalf("agent: got %q want agent-winner", r.AgentID)
	}
	// Orphan session from this Resolve should be best-effort deleted.
	if _, err := sessionRepo.GetByID(context.Background(), "session-1"); !errors.Is(err, pkgErrors.ErrNotFound) {
		t.Fatalf("orphan session should be deleted, GetByID err=%v", err)
	}
}

// conflictCreatePeerRepo simulates a concurrent writer winning the mapping insert.
type conflictCreatePeerRepo struct {
	winner *ChannelPeerSession
	rows   map[string]*ChannelPeerSession
}

func (f *conflictCreatePeerRepo) Get(_ context.Context, channelID, peerID string) (*ChannelPeerSession, error) {
	if f.rows == nil {
		return nil, pkgErrors.ErrNotFound
	}
	row, ok := f.rows[peerKey(channelID, peerID)]
	if !ok {
		return nil, pkgErrors.ErrNotFound
	}
	cp := *row
	return &cp, nil
}

func (f *conflictCreatePeerRepo) Create(_ context.Context, _ *ChannelPeerSession) error {
	if f.rows == nil {
		f.rows = map[string]*ChannelPeerSession{}
	}
	cp := *f.winner
	f.rows[peerKey(f.winner.ChannelID, f.winner.PeerID)] = &cp
	return pkgErrors.ErrConflict
}

func (f *conflictCreatePeerRepo) Upsert(_ context.Context, row *ChannelPeerSession) error {
	if f.rows == nil {
		f.rows = map[string]*ChannelPeerSession{}
	}
	cp := *row
	f.rows[peerKey(row.ChannelID, row.PeerID)] = &cp
	return nil
}

func (f *conflictCreatePeerRepo) Delete(_ context.Context, channelID, peerID string) error {
	if f.rows == nil {
		return pkgErrors.ErrNotFound
	}
	key := peerKey(channelID, peerID)
	if _, ok := f.rows[key]; !ok {
		return pkgErrors.ErrNotFound
	}
	delete(f.rows, key)
	return nil
}

func TestChannelPeerResolve_LongPeerUserIDFitsColumn(t *testing.T) {
	longPeer := strings.Repeat("x", 128)
	peerRepo := &fakeChannelPeerSessionRepo{rows: map[string]*ChannelPeerSession{}}
	sessionRepo := &fakeChatSessionRepoForPeer{}
	uc := NewChannelPeerUsecase(peerRepo, sessionRepo)

	r, err := uc.Resolve(context.Background(), "ch-1", longPeer, "agent-a")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	s, err := sessionRepo.GetByID(context.Background(), r.SessionID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if len(s.UserID) > 36 {
		t.Fatalf("user_id len=%d > 36: %q", len(s.UserID), s.UserID)
	}
	want := PeerUserID("ch-1", longPeer)
	if s.UserID != want {
		t.Fatalf("user_id: got %q want %q", s.UserID, want)
	}
	mapped, err := peerRepo.Get(context.Background(), "ch-1", longPeer)
	if err != nil {
		t.Fatalf("mapping Get: %v", err)
	}
	if mapped.PeerID != longPeer {
		t.Fatalf("mapping peer_id: got len=%d want 128", len(mapped.PeerID))
	}
}
