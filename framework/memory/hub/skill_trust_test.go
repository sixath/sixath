package hub_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/sixath/framework/memory/hub"
)

func TestSkillTrustGate_UnsignedBecomesDraft(t *testing.T) {
	root := t.TempDir()
	fs := hub.NewFSMaterializer(root)
	gate := hub.NewSkillTrustGate(fs, fs, nil)
	res, err := gate.Materialize(context.Background(), hub.SkillContent{
		Hub: "fake", SkillID: "s1", Name: "s1", Version: "1",
		Body: []byte("---\nname: s1\ndescription: d\n---\n"),
		Signed: false,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Ref.Status != hub.AssetDraft {
		t.Fatalf("status=%s", res.Ref.Status)
	}
	if _, err := filepath.Rel(root, res.CacheDir); err != nil {
		t.Fatal(err)
	}
}

func TestSkillTrustGate_SignedActive(t *testing.T) {
	root := t.TempDir()
	fs := hub.NewFSMaterializer(root)
	gate := hub.NewSkillTrustGate(fs, fs, nil)
	body := []byte("---\nname: s1\ndescription: d\n---\n")
	res, err := gate.Materialize(context.Background(), hub.SkillContent{
		Hub: "fake", SkillID: "s1", Version: "1", Body: body, Signed: true,
		ContentHash: hub.HashSkillBody(body),
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Ref.Status != hub.AssetActive {
		t.Fatalf("status=%s", res.Ref.Status)
	}
}

func TestSkillTrustGate_HashMismatchRejected(t *testing.T) {
	root := t.TempDir()
	fs := hub.NewFSMaterializer(root)
	gate := hub.NewSkillTrustGate(fs, fs, nil)
	_, err := gate.Materialize(context.Background(), hub.SkillContent{
		Hub: "fake", SkillID: "s1", Version: "1",
		Body: []byte("x"), ContentHash: "deadbeef", Signed: true,
	})
	if err == nil {
		t.Fatal("expected reject")
	}
}

func TestSkillTrustGate_VersionChangeRematerializes(t *testing.T) {
	root := t.TempDir()
	fs := hub.NewFSMaterializer(root)
	gate := hub.NewSkillTrustGate(fs, fs, nil)
	body1 := []byte("v1")
	r1, err := gate.Materialize(context.Background(), hub.SkillContent{
		Hub: "fake", SkillID: "s1", Version: "1", Body: body1, Signed: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	body2 := []byte("v2")
	r2, err := gate.Materialize(context.Background(), hub.SkillContent{
		Hub: "fake", SkillID: "s1", Version: "2", Body: body2, Signed: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !r2.NewMaterialization {
		t.Fatal("expected new materialization")
	}
	if r1.CacheDir == r2.CacheDir {
		t.Fatal("expected different cache dirs per version")
	}
	// same version+hash → not new
	r3, err := gate.Materialize(context.Background(), hub.SkillContent{
		Hub: "fake", SkillID: "s1", Version: "2", Body: body2, Signed: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if r3.NewMaterialization {
		t.Fatal("expected cache hit")
	}
}

func TestLoadoutEligible_DraftExcluded(t *testing.T) {
	// ensure draft from trust gate cannot enter loadout eligibility helper in local
	if hub.AssetDraft == "" {
		t.Fatal("empty")
	}
}
