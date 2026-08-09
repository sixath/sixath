package growth

import (
	"sync"
	"testing"
)

func TestMetrics_CountersIndependent(t *testing.T) {
	m := NewMetrics()
	m.IncReviewScheduled()
	m.IncReviewScheduled()
	m.IncReviewCompleted()
	m.IncReviewFailed()
	m.IncLeaseContention()
	m.IncLeaseAcquireErr()
	m.IncIdleSweep()
	m.IncPendingDropped()
	snap := m.Snapshot()
	if snap.ReviewsScheduled != 2 || snap.ReviewsCompleted != 1 || snap.ReviewsFailed != 1 ||
		snap.LeaseContention != 1 || snap.LeaseAcquireErr != 1 || snap.IdleSweepRuns != 1 || snap.PendingDropped != 1 {
		t.Fatalf("snapshot mismatch: %+v", snap)
	}
}

func TestMetrics_PendingDepthGauge(t *testing.T) {
	m := NewMetrics()
	m.ObservePendingDepth("ws-a", 3)
	m.ObservePendingDepth("ws-b", 7)
	m.ObservePendingDepth("ws-a", 5) // overwrite
	snap := m.Snapshot()
	if snap.PendingDepth["ws-a"] != 5 || snap.PendingDepth["ws-b"] != 7 {
		t.Fatalf("depth mismatch: %+v", snap.PendingDepth)
	}
	if got := snap.SortedWorkspaces(); len(got) != 2 || got[0] != "ws-a" || got[1] != "ws-b" {
		t.Fatalf("sort=%v", got)
	}
}

func TestMetrics_NilSafe(t *testing.T) {
	var m *Metrics
	m.IncReviewScheduled()
	m.IncLeaseContention()
	m.ObservePendingDepth("ws", 1)
	if got := m.Snapshot(); got.ReviewsScheduled != 0 || got.PendingDepth != nil {
		t.Fatalf("nil snapshot=%+v", got)
	}
}

func TestMetrics_ConcurrentIncrement(t *testing.T) {
	m := NewMetrics()
	var wg sync.WaitGroup
	const n = 500
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			m.IncReviewScheduled()
			m.IncLeaseContention()
		}()
	}
	wg.Wait()
	snap := m.Snapshot()
	if snap.ReviewsScheduled != n || snap.LeaseContention != n {
		t.Fatalf("concurrent counts: %+v", snap)
	}
}

func TestMetrics_SnapshotPendingDepthIsCopy(t *testing.T) {
	m := NewMetrics()
	m.ObservePendingDepth("ws", 1)
	snap := m.Snapshot()
	snap.PendingDepth["ws"] = 999
	if m.Snapshot().PendingDepth["ws"] != 1 {
		t.Fatal("snapshot map should be a copy")
	}
}
