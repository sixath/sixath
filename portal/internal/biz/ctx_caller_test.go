package biz

import (
	"context"
	"testing"
)

func TestDetachCallerContext_PreservesCallerAndOrg(t *testing.T) {
	parent := WithOrgID(WithCallerUserID(context.Background(), "user-1"), "org-1")
	bg := DetachCallerContext(parent)
	uid, ok := CallerUserID(bg)
	if !ok || uid != "user-1" {
		t.Fatalf("caller=%q ok=%v", uid, ok)
	}
	org, ok := OrgID(bg)
	if !ok || org != "org-1" {
		t.Fatalf("org=%q ok=%v", org, ok)
	}
}

func TestDetachCallerContext_NilParent(t *testing.T) {
	bg := DetachCallerContext(nil)
	if _, ok := CallerUserID(bg); ok {
		t.Fatal("expected no caller")
	}
}
