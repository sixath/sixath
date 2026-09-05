package service

import (
	"context"
	"testing"

	"backend/internal/biz"
)

func TestSetBgReviewInFlight(t *testing.T) {
	repo := &fakeGrowthRepoForService{}
	uc := biz.NewGrowthUsecase(repo)
	if err := uc.SetBgReviewInFlight(context.Background(), "inf-1", true); err != nil {
		t.Fatal(err)
	}
	st, err := uc.GetState(context.Background(), "inf-1")
	if err != nil {
		t.Fatal(err)
	}
	if !st.BgReviewInFlight || st.BgReviewInFlightSince == nil {
		t.Fatalf("want in_flight set, got %+v", st)
	}
	if err := uc.SetBgReviewInFlight(context.Background(), "inf-1", false); err != nil {
		t.Fatal(err)
	}
	st, _ = uc.GetState(context.Background(), "inf-1")
	if st.BgReviewInFlight || st.BgReviewInFlightSince != nil {
		t.Fatalf("want cleared, got %+v", st)
	}
}
