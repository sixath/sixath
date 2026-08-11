package biz

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	pkgErrors "backend/internal/pkg/errors"

	kratosErrors "github.com/go-kratos/kratos/v2/errors"
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
func (f *fakeChatSessionRepoForPeer) Touch(context.Context, string) error          { return nil }
func (f *fakeChatSessionRepoForPeer) BumpRewindCount(context.Context, string) error { return nil }
func (f *fakeChatSessionRepoForPeer) MarkReadonly(context.Context, string) error   { return nil }

func seedPeerChannel(t *testing.T, channelRepo *fakeChannelRepo, channelID, defaultAgent string, allowed []string) {
	t.Helper()
	_, err := channelRepo.Create(context.Background(), &ChannelCreate{
		ChannelID:     channelID,
		Type:          "webhook",
		DefaultAgent:  defaultAgent,
		AllowedAgents: allowed,
		Enabled:       true,
	})
	if err != nil {
		t.Fatalf("seed channel: %v", err)
	}
}

func newPeerUsecase(t *testing.T, channelID, defaultAgent string, allowed []string) (*ChannelPeerUsecase, *fakeChannelPeerSessionRepo, *fakeChatSessionRepoForPeer) {
	t.Helper()
	channelRepo := &fakeChannelRepo{}
	if channelID != "" {
		seedPeerChannel(t, channelRepo, channelID, defaultAgent, allowed)
	}
	peerRepo := &fakeChannelPeerSessionRepo{rows: map[string]*ChannelPeerSession{}}
	sessionRepo := &fakeChatSessionRepoForPeer{}
	return NewChannelPeerUsecase(peerRepo, sessionRepo, channelRepo), peerRepo, sessionRepo
}

func resolveIn(channelID, peerID, agentID string, forceNew bool) ChannelPeerResolveInput {
	return ChannelPeerResolveInput{
		ChannelID: channelID,
		PeerID:    peerID,
		AgentID:   agentID,
		ForceNew:  forceNew,
	}
}

func TestResolve_NoMappingCreates(t *testing.T) {
	uc, peerRepo, _ := newPeerUsecase(t, "ch-1", "agent-a", nil)

	r, err := uc.Resolve(context.Background(), resolveIn("ch-1", "peer-a", "agent-a", false))
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if !r.Created {
		t.Fatalf("want created=true")
	}
	if r.SessionID == "" || r.AgentID != "agent-a" {
		t.Fatalf("got %+v", r)
	}
	mapped, err := peerRepo.Get(context.Background(), "ch-1", "peer-a")
	if err != nil {
		t.Fatalf("mapping: %v", err)
	}
	if mapped.SessionID != r.SessionID || mapped.AgentID != "agent-a" {
		t.Fatalf("mapping=%+v", mapped)
	}
}

func TestResolve_SameAgentContinues(t *testing.T) {
	uc, _, _ := newPeerUsecase(t, "ch-1", "agent-a", []string{"agent-a", "agent-b"})

	r1, err := uc.Resolve(context.Background(), resolveIn("ch-1", "peer-a", "agent-a", false))
	if err != nil {
		t.Fatalf("first: %v", err)
	}
	r2, err := uc.Resolve(context.Background(), resolveIn("ch-1", "peer-a", "agent-a", false))
	if err != nil {
		t.Fatalf("second: %v", err)
	}
	if r2.Created {
		t.Fatalf("want created=false")
	}
	if r2.SessionID != r1.SessionID || r2.AgentID != "agent-a" {
		t.Fatalf("got %+v want session=%s agent-a", r2, r1.SessionID)
	}
}

func TestResolve_DifferentAgentWithoutForceNew_AgentBound(t *testing.T) {
	uc, _, _ := newPeerUsecase(t, "ch-1", "agent-a", []string{"agent-a", "agent-b"})

	if _, err := uc.Resolve(context.Background(), resolveIn("ch-1", "peer-a", "agent-a", false)); err != nil {
		t.Fatalf("seed resolve: %v", err)
	}
	_, err := uc.Resolve(context.Background(), resolveIn("ch-1", "peer-a", "agent-b", false))
	if !isReason(err, "AGENT_BOUND") {
		t.Fatalf("err=%v want AGENT_BOUND", err)
	}
	if code := kratosErrors.FromError(err).Code; code != 409 {
		t.Fatalf("code=%d want 409", code)
	}
}

func TestResolve_ForceNewRebindsMapping(t *testing.T) {
	uc, peerRepo, sessionRepo := newPeerUsecase(t, "ch-1", "agent-a", []string{"agent-a", "agent-b"})

	r1, err := uc.Resolve(context.Background(), resolveIn("ch-1", "peer-a", "agent-a", false))
	if err != nil {
		t.Fatalf("first: %v", err)
	}
	oldSessionID := r1.SessionID

	r2, err := uc.Resolve(context.Background(), resolveIn("ch-1", "peer-a", "agent-b", true))
	if err != nil {
		t.Fatalf("force_new: %v", err)
	}
	if !r2.Created {
		t.Fatalf("want created=true")
	}
	if r2.SessionID == oldSessionID {
		t.Fatalf("want new session, still %q", r2.SessionID)
	}
	if r2.AgentID != "agent-b" {
		t.Fatalf("agent=%q want agent-b", r2.AgentID)
	}

	mapped, err := peerRepo.Get(context.Background(), "ch-1", "peer-a")
	if err != nil {
		t.Fatalf("mapping: %v", err)
	}
	if mapped.SessionID != r2.SessionID || mapped.AgentID != "agent-b" {
		t.Fatalf("mapping=%+v", mapped)
	}
	if _, err := sessionRepo.GetByID(context.Background(), oldSessionID); err != nil {
		t.Fatalf("old session should be retained: %v", err)
	}
}

func TestResolve_AgentNotAllowed(t *testing.T) {
	uc, _, _ := newPeerUsecase(t, "ch-1", "agent-a", []string{"agent-a"})

	_, err := uc.Resolve(context.Background(), resolveIn("ch-1", "peer-a", "agent-b", false))
	if !isReason(err, "AGENT_NOT_ALLOWED") {
		t.Fatalf("err=%v want AGENT_NOT_ALLOWED", err)
	}
	if code := kratosErrors.FromError(err).Code; code != 403 {
		t.Fatalf("code=%d want 403", code)
	}
}

func TestResolve_EmptyAllowed_OnlyDefault(t *testing.T) {
	uc, _, _ := newPeerUsecase(t, "ch-1", "agent-a", nil)

	r, err := uc.Resolve(context.Background(), resolveIn("ch-1", "peer-a", "agent-a", false))
	if err != nil {
		t.Fatalf("default allowed: %v", err)
	}
	if r.AgentID != "agent-a" {
		t.Fatalf("agent=%q", r.AgentID)
	}
	_, err = uc.Resolve(context.Background(), resolveIn("ch-1", "peer-b", "agent-b", false))
	if !isReason(err, "AGENT_NOT_ALLOWED") {
		t.Fatalf("err=%v want AGENT_NOT_ALLOWED", err)
	}
}

func TestResolve_ChannelNotFound(t *testing.T) {
	uc, _, _ := newPeerUsecase(t, "", "", nil)

	_, err := uc.Resolve(context.Background(), resolveIn("missing", "peer-a", "agent-a", false))
	if !isReason(err, "CHANNEL_NOT_FOUND") {
		t.Fatalf("err=%v want CHANNEL_NOT_FOUND", err)
	}
	if code := kratosErrors.FromError(err).Code; code != 404 {
		t.Fatalf("code=%d want 404", code)
	}
}

func TestResolve_OmitsAgentUsesDefault(t *testing.T) {
	uc, _, _ := newPeerUsecase(t, "ch-1", "agent-default", []string{"agent-default", "agent-b"})

	r, err := uc.Resolve(context.Background(), resolveIn("ch-1", "peer-a", "", false))
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if r.AgentID != "agent-default" {
		t.Fatalf("agent=%q want agent-default", r.AgentID)
	}
}

func TestChannelPeerResolve_SameKeySameSession(t *testing.T) {
	uc, _, _ := newPeerUsecase(t, "ch-1", "agent-a", []string{"agent-a", "agent-b"})

	r1, err := uc.Resolve(context.Background(), resolveIn("ch-1", "peer-a", "agent-a", false))
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

	r2, err := uc.Resolve(context.Background(), resolveIn("ch-1", "peer-a", "agent-a", false))
	if err != nil {
		t.Fatalf("second Resolve: %v", err)
	}
	if r2.Created {
		t.Fatalf("second Resolve: want created=false")
	}
	if r2.SessionID != r1.SessionID {
		t.Fatalf("second Resolve session: got %q want %q", r2.SessionID, r1.SessionID)
	}

	_, err = uc.Resolve(context.Background(), resolveIn("ch-1", "peer-a", "agent-b", false))
	if !isReason(err, "AGENT_BOUND") {
		t.Fatalf("agent-b Resolve: err=%v want AGENT_BOUND", err)
	}
}

func TestChannelPeerResolve_DifferentPeerDifferentSession(t *testing.T) {
	uc, _, _ := newPeerUsecase(t, "ch-1", "agent-a", nil)

	r1, err := uc.Resolve(context.Background(), resolveIn("ch-1", "peer-1", "agent-a", false))
	if err != nil {
		t.Fatalf("peer-1 Resolve: %v", err)
	}
	r2, err := uc.Resolve(context.Background(), resolveIn("ch-1", "peer-2", "agent-a", false))
	if err != nil {
		t.Fatalf("peer-2 Resolve: %v", err)
	}
	if r1.SessionID == r2.SessionID {
		t.Fatalf("different peers must get different sessions, both %q", r1.SessionID)
	}
}

func TestChannelPeerResolve_MappingCreateConflictReturnsExisting(t *testing.T) {
	channelRepo := &fakeChannelRepo{}
	seedPeerChannel(t, channelRepo, "ch-1", "agent-a", nil)
	winner := &ChannelPeerSession{
		ChannelID: "ch-1",
		PeerID:    "peer-a",
		SessionID: "session-winner",
		AgentID:   "agent-winner",
	}
	peerRepo := &conflictCreatePeerRepo{winner: winner}
	sessionRepo := &fakeChatSessionRepoForPeer{}
	uc := NewChannelPeerUsecase(peerRepo, sessionRepo, channelRepo)

	r, err := uc.Resolve(context.Background(), resolveIn("ch-1", "peer-a", "agent-a", false))
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
	uc, peerRepo, sessionRepo := newPeerUsecase(t, "ch-1", "agent-a", nil)

	r, err := uc.Resolve(context.Background(), resolveIn("ch-1", longPeer, "agent-a", false))
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

func TestGetBinding_ReturnsRow(t *testing.T) {
	uc, _, _ := newPeerUsecase(t, "ch-1", "agent-a", nil)
	r, err := uc.Resolve(context.Background(), resolveIn("ch-1", "peer-a", "agent-a", false))
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	row, err := uc.GetBinding(context.Background(), "ch-1", "peer-a")
	if err != nil {
		t.Fatalf("GetBinding: %v", err)
	}
	if row.SessionID != r.SessionID || row.AgentID != "agent-a" {
		t.Fatalf("unexpected row: %+v want session_id=%q agent_id=agent-a", row, r.SessionID)
	}
}

func TestGetBinding_NotFound(t *testing.T) {
	uc, _, _ := newPeerUsecase(t, "ch-1", "agent-a", nil)
	_, err := uc.GetBinding(context.Background(), "ch-1", "missing-peer")
	if !errors.Is(err, pkgErrors.ErrNotFound) {
		t.Fatalf("err=%v want ErrNotFound", err)
	}
}

func TestChannelPeerDeleteBinding(t *testing.T) {
	uc, peerRepo, _ := newPeerUsecase(t, "ch-1", "agent-a", nil)
	if _, err := uc.Resolve(context.Background(), resolveIn("ch-1", "peer-a", "agent-a", false)); err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if err := uc.DeleteBinding(context.Background(), "ch-1", "peer-a"); err != nil {
		t.Fatalf("DeleteBinding: %v", err)
	}
	if _, err := peerRepo.Get(context.Background(), "ch-1", "peer-a"); !errors.Is(err, pkgErrors.ErrNotFound) {
		t.Fatalf("mapping should be gone, err=%v", err)
	}
}
