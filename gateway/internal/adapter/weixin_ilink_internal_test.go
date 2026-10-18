package adapter

import (
	"fmt"
	"testing"
	"time"
)

func TestIlinkRunner_DeniedRateLimitMapPrunesStaleEntries(t *testing.T) {
	r := &ilinkRunner{
		deps:     normalizeWeixinIlinkDeps(WeixinIlinkDeps{DeniedReplyInterval: time.Minute}),
		deniedAt: make(map[string]time.Time),
	}
	now := time.Now()
	for i := range 1000 {
		r.deniedAt[fmt.Sprintf("stale-%d", i)] = now.Add(-2 * time.Minute)
	}
	r.deniedAt["fresh"] = now.Add(-30 * time.Second)

	for i := range ilinkDeniedPruneEvery {
		if !r.allowDeniedReply(fmt.Sprintf("new-%d", i), now) {
			t.Fatalf("new peer %d rate-limited", i)
		}
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.deniedAt["fresh"]; !ok {
		t.Fatal("entry within DeniedReplyInterval was pruned")
	}
	if got, want := len(r.deniedAt), 1+ilinkDeniedPruneEvery; got != want {
		t.Fatalf("deniedAt len=%d want %d (stale entries pruned)", got, want)
	}
}

func TestIlinkRunner_DeniedRateLimitStillAppliesAfterPrune(t *testing.T) {
	r := &ilinkRunner{
		deps:     normalizeWeixinIlinkDeps(WeixinIlinkDeps{DeniedReplyInterval: time.Minute}),
		deniedAt: make(map[string]time.Time),
	}
	now := time.Now()
	if !r.allowDeniedReply("bob", now) {
		t.Fatal("first denied reply suppressed")
	}
	for i := range 2 * ilinkDeniedPruneEvery {
		r.allowDeniedReply(fmt.Sprintf("p-%d", i), now)
	}
	if r.allowDeniedReply("bob", now.Add(10*time.Second)) {
		t.Fatal("bob replied again within DeniedReplyInterval")
	}
	if !r.allowDeniedReply("bob", now.Add(time.Minute)) {
		t.Fatal("bob not replied after DeniedReplyInterval")
	}
}
