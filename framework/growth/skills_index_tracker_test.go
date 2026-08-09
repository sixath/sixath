package growth

import (
	"sync"
	"testing"
)

func TestSkillsIndexTracker_BumpMonotonic(t *testing.T) {
	tr := NewSkillsIndexTracker()
	if got := tr.Generation("ws"); got != 0 {
		t.Fatalf("initial gen=%d", got)
	}
	g1 := tr.Bump("ws")
	g2 := tr.Bump("ws")
	if g1 != 1 || g2 != 2 {
		t.Fatalf("bump seq=%d,%d", g1, g2)
	}
	if got := tr.Generation("ws"); got != 2 {
		t.Fatalf("read gen=%d", got)
	}
	if got := tr.Generation("other"); got != 0 {
		t.Fatalf("other workspace gen=%d", got)
	}
}

func TestSkillsIndexTracker_EmptyWorkspaceIsNoop(t *testing.T) {
	tr := NewSkillsIndexTracker()
	if got := tr.Bump(""); got != 0 {
		t.Fatalf("empty bump=%d", got)
	}
	if got := tr.Generation(""); got != 0 {
		t.Fatalf("empty gen=%d", got)
	}
}

func TestSkillsIndexTracker_OnBumpHook(t *testing.T) {
	tr := NewSkillsIndexTracker()
	var mu sync.Mutex
	var events []uint64
	tr.OnBump(func(ws string, gen uint64) {
		mu.Lock()
		defer mu.Unlock()
		if ws == "ws" {
			events = append(events, gen)
		}
	})
	tr.Bump("ws")
	tr.Bump("ws")
	tr.Bump("other")
	mu.Lock()
	defer mu.Unlock()
	if len(events) != 2 || events[0] != 1 || events[1] != 2 {
		t.Fatalf("hook events=%v", events)
	}
}

func TestSkillsIndexTracker_NilSafe(t *testing.T) {
	var tr *SkillsIndexTracker
	if tr.Bump("ws") != 0 {
		t.Fatalf("nil bump should be 0")
	}
	if tr.Generation("ws") != 0 {
		t.Fatalf("nil gen should be 0")
	}
	tr.OnBump(func(string, uint64) {})
}

func TestSkillsIndexTracker_ConcurrentBump(t *testing.T) {
	tr := NewSkillsIndexTracker()
	var wg sync.WaitGroup
	const n = 200
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			tr.Bump("ws")
		}()
	}
	wg.Wait()
	if got := tr.Generation("ws"); got != n {
		t.Fatalf("concurrent gen=%d want %d", got, n)
	}
}
