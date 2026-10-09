package data

import (
	"context"
	"testing"
	"time"

	"backend/internal/biz"
)

func TestRepoRegistryRepo_HandbookClaimAndFinish(t *testing.T) {
	ctx := context.Background()
	r := newRepoRegistryRepoForTest(t)
	now := time.Now()
	a, err := r.UpsertScannedRepository(ctx, &biz.Repository{CodeRoot: "/c", RelPath: "cg/a", Name: "a", HeadCommit: "111", LastScannedAt: &now})
	if err != nil {
		t.Fatal(err)
	}
	reload := func() *biz.Repository {
		m, err := r.GetRepositoriesByIDs(ctx, []string{a.ID})
		if err != nil {
			t.Fatal(err)
		}
		return m[a.ID]
	}

	if ok, err := r.ClaimHandbookBuild(ctx, a.ID, now, now.Add(time.Minute)); err != nil || !ok {
		t.Fatalf("first claim: ok=%v err=%v", ok, err)
	}
	if got := reload(); got.HandbookStatus != biz.HandbookStatusBuilding {
		t.Fatalf("status = %s", got.HandbookStatus)
	}
	if ok, _ := r.ClaimHandbookBuild(ctx, a.ID, now, now.Add(time.Minute)); ok {
		t.Fatal("second claim must fail while the lease is live")
	}
	if ok, _ := r.ClaimHandbookBuild(ctx, a.ID, now.Add(2*time.Minute), now.Add(3*time.Minute)); !ok {
		t.Fatal("an expired lease must be claimable")
	}

	if err := r.FinishHandbookBuild(ctx, a.ID, biz.HandbookBuildResult{
		Status: biz.HandbookStatusReady, Commit: "111", Version: 1, Stats: map[string]any{"files": 3},
	}); err != nil {
		t.Fatal(err)
	}
	g := reload()
	if g.HandbookStatus != biz.HandbookStatusReady || g.HandbookCommit != "111" || g.HandbookVersion != 1 || g.HandbookStats["files"] != float64(3) {
		t.Fatalf("after ready: %#v", g)
	}

	if ok, _ := r.ClaimHandbookBuild(ctx, a.ID, now.Add(5*time.Minute), now.Add(6*time.Minute)); !ok {
		t.Fatal("a finished build must release the lease")
	}
	if err := r.FinishHandbookBuild(ctx, a.ID, biz.HandbookBuildResult{
		Status: biz.HandbookStatusFailed, Stats: map[string]any{"last_error": "boom"},
	}); err != nil {
		t.Fatal(err)
	}
	g = reload()
	if g.HandbookStatus != biz.HandbookStatusFailed || g.HandbookCommit != "111" || g.HandbookVersion != 1 || g.HandbookStats["last_error"] != "boom" {
		t.Fatalf("a failed build must keep commit and version: %#v", g)
	}
}

func TestRepoRegistryRepo_ClaimUnknownRepo(t *testing.T) {
	r := newRepoRegistryRepoForTest(t)
	now := time.Now()
	if ok, err := r.ClaimHandbookBuild(context.Background(), "nope", now, now.Add(time.Minute)); err != nil || ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
}
