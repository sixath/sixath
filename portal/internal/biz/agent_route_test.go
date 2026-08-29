package biz

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	pkgErrors "backend/internal/pkg/errors"
)

type fakeRouteChannelRepo struct {
	byID map[string]*ChannelMeta
	err  error
}

func (f *fakeRouteChannelRepo) Create(context.Context, *ChannelCreate) (*ChannelMeta, error) {
	return nil, nil
}
func (f *fakeRouteChannelRepo) GetByID(context.Context, string) (*ChannelMeta, error) {
	return nil, pkgErrors.ErrNotFound
}
func (f *fakeRouteChannelRepo) GetByChannelID(_ context.Context, channelID string) (*ChannelMeta, error) {
	if f.err != nil {
		return nil, f.err
	}
	ch, ok := f.byID[channelID]
	if !ok {
		return nil, pkgErrors.ErrNotFound
	}
	cp := *ch
	return &cp, nil
}
func (f *fakeRouteChannelRepo) GetWecomByDefaultAgent(context.Context, string) (*ChannelMeta, error) {
	return nil, pkgErrors.ErrNotFound
}
func (f *fakeRouteChannelRepo) List(context.Context, int32, int32, string, *bool) ([]*ChannelMeta, int, error) {
	return nil, 0, nil
}
func (f *fakeRouteChannelRepo) ListGatewayChannels(context.Context) ([]*ChannelMeta, error) {
	return nil, nil
}
func (f *fakeRouteChannelRepo) Update(context.Context, string, map[string]any) (*ChannelMeta, error) {
	return nil, pkgErrors.ErrNotFound
}
func (f *fakeRouteChannelRepo) Delete(context.Context, string) error {
	return pkgErrors.ErrNotFound
}

type fakeRoutePeerRepo struct {
	byKey map[string]*ChannelPeerSession
}

func (f *fakeRoutePeerRepo) Get(_ context.Context, channelID, peerID string) (*ChannelPeerSession, error) {
	row, ok := f.byKey[channelID+"\x00"+peerID]
	if !ok {
		return nil, pkgErrors.ErrNotFound
	}
	cp := *row
	return &cp, nil
}
func (f *fakeRoutePeerRepo) Create(context.Context, *ChannelPeerSession) error { return nil }
func (f *fakeRoutePeerRepo) Upsert(context.Context, *ChannelPeerSession) error { return nil }
func (f *fakeRoutePeerRepo) Delete(context.Context, string, string) error      { return nil }

type fakeRouteAgentReader struct {
	byID map[string]*AgentMeta
}

func (f *fakeRouteAgentReader) GetForSession(_ context.Context, id string) (*AgentMeta, error) {
	m, ok := f.byID[id]
	if !ok {
		return nil, ErrAgentNotFound
	}
	cp := *m
	return &cp, nil
}

type fakeCompleter struct {
	fn      func(ctx context.Context, prompt string) (string, error)
	calls   int
	lastPrompt string
}

func (f *fakeCompleter) Complete(ctx context.Context, prompt string) (string, error) {
	f.calls++
	f.lastPrompt = prompt
	if f.fn != nil {
		return f.fn(ctx, prompt)
	}
	return "", errors.New("no completer fn")
}

func newRouteUC(ch *fakeRouteChannelRepo, peers *fakeRoutePeerRepo, agents agentRouteAgentReader, c RouteCompleter) *AgentRouteUsecase {
	return NewAgentRouteUsecase(ch, peers, agents, c, 50*time.Millisecond)
}

func TestRoute_SingleCandidate_NoLLM(t *testing.T) {
	ch := &fakeRouteChannelRepo{byID: map[string]*ChannelMeta{
		"ch1": {ChannelID: "ch1", DefaultAgent: "a1"},
	}}
	c := &fakeCompleter{fn: func(context.Context, string) (string, error) {
		t.Fatal("Completer must not be called")
		return "", nil
	}}
	uc := newRouteUC(ch, &fakeRoutePeerRepo{}, &fakeRouteAgentReader{byID: map[string]*AgentMeta{
		"a1": {ID: "a1", Name: "One"},
	}}, c)

	out, err := uc.Route(context.Background(), AgentRouteInput{ChannelID: "ch1", Text: "hi"})
	if err != nil {
		t.Fatalf("Route: %v", err)
	}
	if out.AgentID != "a1" || out.Confidence != RouteConfidenceHigh || out.Source != RouteSourceDefault || out.Reason != "single_candidate" {
		t.Fatalf("out=%+v", out)
	}
	if c.calls != 0 {
		t.Fatalf("calls=%d", c.calls)
	}
}

func TestRoute_ClassifierHigh_InAllowlist(t *testing.T) {
	ch := &fakeRouteChannelRepo{byID: map[string]*ChannelMeta{
		"ch1": {
			ChannelID:     "ch1",
			DefaultAgent:  "a1",
			AllowedAgents: []string{"a1", "b"},
		},
	}}
	c := &fakeCompleter{fn: func(context.Context, string) (string, error) {
		return `{"agent_id":"b","confidence":"high"}`, nil
	}}
	uc := newRouteUC(ch, &fakeRoutePeerRepo{}, &fakeRouteAgentReader{byID: map[string]*AgentMeta{
		"a1": {ID: "a1", Name: "A"},
		"b":  {ID: "b", Name: "B", Description: "beta"},
	}}, c)

	out, err := uc.Route(context.Background(), AgentRouteInput{ChannelID: "ch1", Text: "need B"})
	if err != nil {
		t.Fatalf("Route: %v", err)
	}
	if out.AgentID != "b" || out.Confidence != RouteConfidenceHigh || out.Source != RouteSourceClassifier {
		t.Fatalf("out=%+v", out)
	}
	if c.calls != 1 || !strings.Contains(c.lastPrompt, "need B") {
		t.Fatalf("prompt calls=%d prompt=%q", c.calls, c.lastPrompt)
	}
}

func TestRoute_ClassifierLow_FailOpenCurrent(t *testing.T) {
	ch := &fakeRouteChannelRepo{byID: map[string]*ChannelMeta{
		"ch1": {
			ChannelID:     "ch1",
			DefaultAgent:  "a1",
			AllowedAgents: []string{"a1", "b"},
		},
	}}
	peers := &fakeRoutePeerRepo{byKey: map[string]*ChannelPeerSession{
		"ch1\x00peer1": {ChannelID: "ch1", PeerID: "peer1", AgentID: "a1", SessionID: "s1"},
	}}
	c := &fakeCompleter{fn: func(context.Context, string) (string, error) {
		return `{"agent_id":"b","confidence":"low"}`, nil
	}}
	uc := newRouteUC(ch, peers, &fakeRouteAgentReader{byID: map[string]*AgentMeta{
		"a1": {ID: "a1", Name: "A"},
		"b":  {ID: "b", Name: "B"},
	}}, c)

	out, err := uc.Route(context.Background(), AgentRouteInput{ChannelID: "ch1", PeerID: "peer1", Text: "x"})
	if err != nil {
		t.Fatalf("Route: %v", err)
	}
	if out.AgentID != "a1" || out.Confidence != RouteConfidenceLow || out.Source != RouteSourceCurrent {
		t.Fatalf("out=%+v", out)
	}
}

func TestRoute_ClassifierTimeout_FailOpen(t *testing.T) {
	ch := &fakeRouteChannelRepo{byID: map[string]*ChannelMeta{
		"ch1": {
			ChannelID:     "ch1",
			DefaultAgent:  "a1",
			AllowedAgents: []string{"a1", "b"},
		},
	}}
	c := &fakeCompleter{fn: func(ctx context.Context, _ string) (string, error) {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(200 * time.Millisecond):
			return `{"agent_id":"b","confidence":"high"}`, nil
		}
	}}
	uc := newRouteUC(ch, &fakeRoutePeerRepo{}, &fakeRouteAgentReader{byID: map[string]*AgentMeta{
		"a1": {ID: "a1"},
		"b":  {ID: "b"},
	}}, c)

	out, err := uc.Route(context.Background(), AgentRouteInput{ChannelID: "ch1", Text: "x"})
	if err != nil {
		t.Fatalf("Route: %v", err)
	}
	if out.AgentID != "a1" || out.Confidence != RouteConfidenceLow || out.Source != RouteSourceDefault || out.Reason != "classifier_timeout" {
		t.Fatalf("out=%+v", out)
	}
}

func TestRoute_BadJSON_FailOpen(t *testing.T) {
	ch := &fakeRouteChannelRepo{byID: map[string]*ChannelMeta{
		"ch1": {
			ChannelID:     "ch1",
			DefaultAgent:  "a1",
			AllowedAgents: []string{"a1", "b"},
		},
	}}
	c := &fakeCompleter{fn: func(context.Context, string) (string, error) {
		return "not-json", nil
	}}
	uc := newRouteUC(ch, &fakeRoutePeerRepo{}, nil, c)
	out, err := uc.Route(context.Background(), AgentRouteInput{ChannelID: "ch1", Text: "x"})
	if err != nil {
		t.Fatalf("Route: %v", err)
	}
	if out.Reason != "bad_json" || out.Source != RouteSourceDefault || out.AgentID != "a1" {
		t.Fatalf("out=%+v", out)
	}
}

func TestRoute_NonAllowlistID_FailOpen(t *testing.T) {
	ch := &fakeRouteChannelRepo{byID: map[string]*ChannelMeta{
		"ch1": {
			ChannelID:     "ch1",
			DefaultAgent:  "a1",
			AllowedAgents: []string{"a1", "b"},
		},
	}}
	c := &fakeCompleter{fn: func(context.Context, string) (string, error) {
		return `{"agent_id":"evil","confidence":"high"}`, nil
	}}
	uc := newRouteUC(ch, &fakeRoutePeerRepo{}, nil, c)
	out, err := uc.Route(context.Background(), AgentRouteInput{ChannelID: "ch1", Text: "x"})
	if err != nil {
		t.Fatalf("Route: %v", err)
	}
	if out.Reason != "agent_not_allowlisted" || out.AgentID != "a1" {
		t.Fatalf("out=%+v", out)
	}
}

func TestRoute_ChannelMissing_NotFound(t *testing.T) {
	uc := newRouteUC(&fakeRouteChannelRepo{byID: map[string]*ChannelMeta{}}, &fakeRoutePeerRepo{}, nil, nil)
	_, err := uc.Route(context.Background(), AgentRouteInput{ChannelID: "missing", Text: "x"})
	if !errors.Is(err, ErrChannelNotFound) && !isReason(err, "CHANNEL_NOT_FOUND") {
		t.Fatalf("err=%v", err)
	}
}
