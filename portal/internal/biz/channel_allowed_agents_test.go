package biz

import (
	"context"
	"fmt"
	"testing"
	"time"

	pkgErrors "backend/internal/pkg/errors"

	"github.com/go-kratos/kratos/v2/log"
)

type fakeChannelRepo struct {
	byID map[string]*ChannelMeta
	seq  int
}

func (f *fakeChannelRepo) Create(_ context.Context, ch *ChannelCreate) (*ChannelMeta, error) {
	if f.byID == nil {
		f.byID = map[string]*ChannelMeta{}
	}
	for _, existing := range f.byID {
		if existing.ChannelID == ch.ChannelID {
			return nil, pkgErrors.ErrDuplicateName
		}
	}
	f.seq++
	id := fmt.Sprintf("ch-%d", f.seq)
	allowed := ch.AllowedAgents
	if allowed == nil {
		allowed = []string{}
	}
	meta := &ChannelMeta{
		ID:            id,
		ChannelID:     ch.ChannelID,
		Type:          ch.Type,
		DefaultAgent:  ch.DefaultAgent,
		AllowedAgents: append([]string(nil), allowed...),
		AutoRouteEnabled:    ch.AutoRouteEnabled,
		AutoRouteMention:    ch.AutoRouteMention,
		AutoRouteClassifier: ch.AutoRouteClassifier,
		Enabled:       ch.Enabled,
		CreatedAt:     time.Now(),
		UpdatedAt:     time.Now(),
	}
	f.byID[id] = meta
	cp := *meta
	return &cp, nil
}

func (f *fakeChannelRepo) GetByID(_ context.Context, id string) (*ChannelMeta, error) {
	ch, ok := f.byID[id]
	if !ok {
		return nil, pkgErrors.ErrNotFound
	}
	cp := *ch
	return &cp, nil
}

func (f *fakeChannelRepo) GetByChannelID(_ context.Context, channelID string) (*ChannelMeta, error) {
	for _, ch := range f.byID {
		if ch.ChannelID == channelID {
			cp := *ch
			return &cp, nil
		}
	}
	return nil, pkgErrors.ErrNotFound
}

func (f *fakeChannelRepo) GetWecomByDefaultAgent(context.Context, string) (*ChannelMeta, error) {
	return nil, pkgErrors.ErrNotFound
}

func (f *fakeChannelRepo) List(context.Context, int32, int32, string, *bool) ([]*ChannelMeta, int, error) {
	return nil, 0, nil
}

func (f *fakeChannelRepo) Update(_ context.Context, id string, updates map[string]any) (*ChannelMeta, error) {
	ch, ok := f.byID[id]
	if !ok {
		return nil, pkgErrors.ErrNotFound
	}
	cp := *ch
	if v, ok := updates["default_agent"].(string); ok {
		cp.DefaultAgent = v
	}
	if v, ok := updates["allowed_agents"].([]string); ok {
		cp.AllowedAgents = append([]string(nil), v...)
	}
	if v, ok := updates["auto_route_enabled"].(bool); ok {
		cp.AutoRouteEnabled = v
	}
	if v, ok := updates["auto_route_mention"].(bool); ok {
		cp.AutoRouteMention = v
	}
	if v, ok := updates["auto_route_classifier"].(bool); ok {
		cp.AutoRouteClassifier = v
	}
	cp.UpdatedAt = time.Now()
	f.byID[id] = &cp
	out := cp
	return &out, nil
}

func (f *fakeChannelRepo) Delete(_ context.Context, id string) error {
	if _, ok := f.byID[id]; !ok {
		return pkgErrors.ErrNotFound
	}
	delete(f.byID, id)
	return nil
}

func newChannelUsecaseForTest(repo ChannelRepo) *ChannelUsecase {
	return NewChannelUsecase(repo, nil, log.DefaultLogger)
}

func TestChannelCreate_DefaultMustBeInAllowedWhenNonEmpty(t *testing.T) {
	repo := &fakeChannelRepo{byID: map[string]*ChannelMeta{}}
	uc := newChannelUsecaseForTest(repo)

	_, err := uc.Create(context.Background(), &ChannelCreate{
		ChannelID:     "test-ch",
		Type:          "webhook",
		DefaultAgent:  "a1",
		AllowedAgents: []string{"a2"},
		Enabled:       true,
	})
	if !isReason(err, "INVALID_ARGUMENT") {
		t.Fatalf("Create error = %v, want INVALID_ARGUMENT", err)
	}
}

func TestChannelCreate_EmptyAllowedMeansDefaultOnly(t *testing.T) {
	repo := &fakeChannelRepo{byID: map[string]*ChannelMeta{}}
	uc := newChannelUsecaseForTest(repo)

	meta, err := uc.Create(context.Background(), &ChannelCreate{
		ChannelID:    "test-ch",
		Type:         "webhook",
		DefaultAgent: "a1",
		Enabled:      true,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if meta.DefaultAgent != "a1" {
		t.Fatalf("default_agent = %q, want a1", meta.DefaultAgent)
	}
	if len(meta.AllowedAgents) != 0 {
		t.Fatalf("allowed_agents = %v, want empty", meta.AllowedAgents)
	}
}

func TestChannelUpdate_RejectsDefaultOutsideAllowed(t *testing.T) {
	repo := &fakeChannelRepo{byID: map[string]*ChannelMeta{}}
	uc := newChannelUsecaseForTest(repo)

	created, err := uc.Create(context.Background(), &ChannelCreate{
		ChannelID:     "test-ch",
		Type:          "webhook",
		DefaultAgent:  "a1",
		AllowedAgents: []string{"a1"},
		Enabled:       true,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	_, err = uc.Update(context.Background(), created.ID, map[string]any{
		"default_agent": "a2",
	})
	if !isReason(err, "INVALID_ARGUMENT") {
		t.Fatalf("Update error = %v, want INVALID_ARGUMENT", err)
	}
}
