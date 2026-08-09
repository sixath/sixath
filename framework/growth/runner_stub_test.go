package growth

import (
	"context"
	"errors"
	"testing"
)

func TestStubRunner_Run_callsMemoryAndClear(t *testing.T) {
	var mem, cleared string
	r := &StubRunner{
		MemoryNotify: func(ctx context.Context, sessionID string) {
			_ = ctx
			mem = sessionID
		},
		ClearGrowthPending: func(ctx context.Context, sessionID string, cs, cm bool) error {
			_ = ctx
			if !cs || !cm {
				t.Fatalf("expected full clear, skill=%v mem=%v", cs, cm)
			}
			cleared = sessionID
			return nil
		},
	}
	if err := r.Run(context.Background(), ReviewJob{SessionID: "s1", PendingMemory: true}); err != nil {
		t.Fatal(err)
	}
	if mem != "s1" || cleared != "s1" {
		t.Fatalf("mem=%q cleared=%q", mem, cleared)
	}
}

func TestStubRunner_Run_clearError(t *testing.T) {
	want := errors.New("x")
	r := &StubRunner{
		ClearGrowthPending: func(ctx context.Context, sessionID string, cs, cm bool) error {
			_ = ctx
			_ = sessionID
			_ = cs
			_ = cm
			return want
		},
	}
	err := r.Run(context.Background(), ReviewJob{SessionID: "s2", PendingMemory: false})
	if err != want {
		t.Fatalf("err=%v", err)
	}
}

func TestStubRunner_Run_nilReceiver(t *testing.T) {
	var r *StubRunner
	if err := r.Run(context.Background(), ReviewJob{}); err != nil {
		t.Fatal(err)
	}
}
