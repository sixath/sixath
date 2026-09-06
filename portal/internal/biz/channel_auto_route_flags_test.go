package biz

import (
	"context"
	"testing"
)

func TestChannelCreate_AutoRouteFlagsDefaultTrue(t *testing.T) {
	repo := &fakeChannelRepo{byID: map[string]*ChannelMeta{}}
	uc := newChannelUsecaseForTest(repo)

	meta, err := uc.Create(context.Background(), &ChannelCreate{
		ChannelID:    "test-ch",
		Type:         "webhook",
		DefaultAgent: "a1",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if !meta.AutoRouteEnabled {
		t.Fatal("AutoRouteEnabled = false, want true")
	}
	if !meta.AutoRouteMention {
		t.Fatal("AutoRouteMention = false, want true")
	}
	if !meta.AutoRouteClassifier {
		t.Fatal("AutoRouteClassifier = false, want true")
	}
}

func TestChannelUpdate_CanDisableMasterSwitch(t *testing.T) {
	repo := &fakeChannelRepo{byID: map[string]*ChannelMeta{}}
	uc := newChannelUsecaseForTest(repo)

	created, err := uc.Create(context.Background(), &ChannelCreate{
		ChannelID:    "test-ch",
		Type:         "webhook",
		DefaultAgent: "a1",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	meta, err := uc.Update(context.Background(), created.ID, map[string]any{
		"auto_route_enabled": false,
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if meta.AutoRouteEnabled {
		t.Fatal("AutoRouteEnabled = true, want false")
	}
}
