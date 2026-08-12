package chat

import (
	"sync"
	"testing"

	"github.com/sixath/framework/config"
	"github.com/sixath/framework/memory"
)

func TestDefaultFailureSignalSink_NonNil(t *testing.T) {
	SetProceduralRepairConfig(nil)
	s := DefaultFailureSignalSink()
	if s == nil {
		t.Fatal("expected non-nil sink")
	}
	_ = memory.FailureSignalSink(s)
}

// Regression: previously rebuildDefaultFailureSinkLocked reset sync.Once inside
// Once.Do, causing unlock of unlocked mutex fatal when sink was still nil.
func TestDefaultFailureSignalSink_NoPanicWithoutPriorConfig(t *testing.T) {
	proceduralMu.Lock()
	defaultFailureSink = nil
	proceduralCatalog = nil
	proceduralMu.Unlock()

	s := DefaultFailureSignalSink()
	if s == nil {
		t.Fatal("expected non-nil sink")
	}
}

func TestDefaultFailureSignalSink_ConcurrentWithConfigRebuild(t *testing.T) {
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			_ = DefaultFailureSignalSink()
		}()
		go func() {
			defer wg.Done()
			SetProceduralRepairConfig(&config.MemoryProceduralRepair{Enabled: false})
		}()
	}
	wg.Wait()
	if DefaultFailureSignalSink() == nil {
		t.Fatal("expected non-nil sink after concurrent rebuild")
	}
}
