package hub_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/sixath/framework/memory/hub"
)

type transportGov struct {
	fakeGov
	loadoutErr error
	accessErr  error
	loadout    []hub.AssetRef
	accessOK   bool
}

func (t transportGov) ResolveLoadout(context.Context, hub.Identity) ([]hub.AssetRef, error) {
	if t.loadoutErr != nil {
		return nil, t.loadoutErr
	}
	return t.loadout, nil
}

func (t transportGov) CheckAccess(context.Context, hub.Identity, hub.AssetRef, string) (bool, error) {
	if t.accessErr != nil {
		return false, t.accessErr
	}
	return t.accessOK, nil
}

func TestReadLoadout_TransportFailOpen(t *testing.T) {
	g := transportGov{fakeGov: fakeGov{name: "tencent"}, loadoutErr: fmt.Errorf("%w: down", hub.ErrTransport)}
	refs, err := hub.ReadLoadout(context.Background(), g, hub.Identity{}, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	if refs != nil && len(refs) != 0 {
		t.Fatalf("want empty, got %#v", refs)
	}
}

func TestReadLoadout_FallbackDefault(t *testing.T) {
	primary := transportGov{fakeGov: fakeGov{name: "tencent"}, loadoutErr: fmt.Errorf("%w: down", hub.ErrTransport)}
	fallback := transportGov{
		fakeGov: fakeGov{name: "local"},
		loadout: []hub.AssetRef{{ID: "s1", Hub: "local", Kind: hub.AssetKindSkill, Status: hub.AssetActive}},
	}
	refs, err := hub.ReadLoadout(context.Background(), primary, hub.Identity{}, true, fallback)
	if err != nil {
		t.Fatal(err)
	}
	if len(refs) != 1 || refs[0].ID != "s1" {
		t.Fatalf("%#v", refs)
	}
}

func TestCheckAccessSafe_TransportDenies(t *testing.T) {
	g := transportGov{fakeGov: fakeGov{name: "tencent"}, accessErr: fmt.Errorf("%w: down", hub.ErrTransport)}
	ok, err := hub.CheckAccessSafe(context.Background(), g, hub.Identity{}, hub.AssetRef{ID: "1", Hub: "tencent"}, "read", false, nil)
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("expected deny")
	}
}

func TestReadLoadout_BusinessErrorPropagates(t *testing.T) {
	g := transportGov{fakeGov: fakeGov{name: "local"}, loadoutErr: hub.ErrNotSupported}
	_, err := hub.ReadLoadout(context.Background(), g, hub.Identity{}, true, nil)
	if err == nil {
		t.Fatal("expected error")
	}
}
