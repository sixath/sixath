package tool

import (
	"reflect"
	"testing"
	"time"
)

func TestApprovalPolicy_EffectiveTTL(t *testing.T) {
	if got := (ApprovalPolicy{}).EffectiveTTLSeconds(); got != DefaultConfirmTTLSeconds {
		t.Fatalf("zero policy TTL=%d want %d", got, DefaultConfirmTTLSeconds)
	}
	if got := (ApprovalPolicy{TTLSeconds: 60}).EffectiveTTLSeconds(); got != 60 {
		t.Fatalf("TTL=%d want 60", got)
	}
	if got := (ApprovalPolicy{TTLSeconds: -1}).EffectiveTTLSeconds(); got != DefaultConfirmTTLSeconds {
		t.Fatalf("negative TTL must fall back to default, got %d", got)
	}
}

func TestApprovalPolicy_ExpiredBoundary(t *testing.T) {
	p := ApprovalPolicy{TTLSeconds: 300}
	now := time.Now()
	created := now.Add(-300 * time.Second)

	if p.Expired(created, now) {
		t.Fatal("exactly at TTL must not be expired (matches previous > comparison)")
	}
	if !p.Expired(created.Add(-time.Millisecond), now) {
		t.Fatal("past TTL must be expired")
	}
	if p.Expired(time.Time{}, now) {
		t.Fatal("zero createdAt must not be treated as expired")
	}
}

func TestApprovalPolicy_VerifyNotFound(t *testing.T) {
	called := false
	failure, bad := ApprovalPolicy{}.Verify(ConfirmCheck{
		Found:    false,
		OnExpire: func() error { called = true; return nil },
	})
	if !bad {
		t.Fatal("missing pending must fail verification")
	}
	if failure.Code != ConfirmNotFound {
		t.Fatalf("code=%q want %q", failure.Code, ConfirmNotFound)
	}
	if called {
		t.Fatal("OnExpire must not run for not_found")
	}
}

func TestApprovalPolicy_VerifyExpiredDeletesPending(t *testing.T) {
	deleted := 0
	failure, bad := ApprovalPolicy{TTLSeconds: 1}.Verify(ConfirmCheck{
		Found:     true,
		CreatedAt: time.Now().Add(-2 * time.Second),
		OnExpire:  func() error { deleted++; return nil },
	})
	if !bad || failure.Code != ConfirmExpired {
		t.Fatalf("failure=%+v bad=%v want expired", failure, bad)
	}
	if deleted != 1 {
		t.Fatalf("expired pending must be deleted exactly once, got %d", deleted)
	}
}

func TestApprovalPolicy_VerifyFreshPendingPasses(t *testing.T) {
	failure, bad := ApprovalPolicy{TTLSeconds: 300}.Verify(ConfirmCheck{
		Found:     true,
		CreatedAt: time.Now(),
		OnExpire:  func() error { t.Fatal("OnExpire must not run for a fresh pending"); return nil },
	})
	if bad {
		t.Fatalf("fresh pending must pass, got %+v", failure)
	}
}

// 与既有 ConfirmTokenError 契约保持一致：迁移到策略后返回结构不得变化。
func TestConfirmFailure_MapMatchesLegacyShape(t *testing.T) {
	for _, code := range []string{ConfirmNotFound, ConfirmExpired} {
		got := ConfirmFailure{Code: code}.Map()
		want := ConfirmTokenError(code)
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("code=%q Map()=%#v want %#v", code, got, want)
		}
	}
	if msg := (ConfirmFailure{Code: ConfirmExpired}).Message(); msg != "确认已过期，请让助手重新发起操作" {
		t.Fatalf("expired message changed: %q", msg)
	}
}
