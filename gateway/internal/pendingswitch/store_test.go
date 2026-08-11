package pendingswitch

import (
	"testing"
	"time"
)

func TestStore_PutGetDelete(t *testing.T) {
	s := New()
	now := time.Date(2026, 8, 11, 10, 0, 0, 0, time.UTC)
	expires := now.Add(5 * time.Minute)

	entry := Entry{
		Agents: []Agent{
			{ID: "agent-1", Name: "Alpha"},
			{ID: "agent-2", Name: "Beta"},
		},
		ExpiresAt: expires,
	}
	s.Put("channel-a", "peer-1", entry)

	got, ok := s.Get("channel-a", "peer-1", now)
	if !ok {
		t.Fatal("Get: expected hit")
	}
	if len(got.Agents) != 2 {
		t.Fatalf("Agents len=%d, want 2", len(got.Agents))
	}
	if got.Agents[0].ID != "agent-1" || got.Agents[0].Name != "Alpha" {
		t.Fatalf("Agents[0]=%+v", got.Agents[0])
	}
	if !got.ExpiresAt.Equal(expires) {
		t.Fatalf("ExpiresAt=%v, want %v", got.ExpiresAt, expires)
	}

	// Mutating returned slice must not affect stored entry.
	got.Agents[0].Name = "Mutated"
	got2, ok := s.Get("channel-a", "peer-1", now)
	if !ok {
		t.Fatal("Get after mutate: expected hit")
	}
	if got2.Agents[0].Name != "Alpha" {
		t.Fatalf("stored agent name=%q, want Alpha (no aliasing)", got2.Agents[0].Name)
	}

	// Mutating input slice on Put must not affect stored entry.
	entry.Agents[1].Name = "AlsoMutated"
	got3, ok := s.Get("channel-a", "peer-1", now)
	if !ok {
		t.Fatal("Get after Put mutate: expected hit")
	}
	if got3.Agents[1].Name != "Beta" {
		t.Fatalf("stored agent name=%q, want Beta (no aliasing on Put)", got3.Agents[1].Name)
	}

	_, ok = s.Get("other-channel", "peer-1", now)
	if ok {
		t.Fatal("Get wrong channel: expected miss")
	}
	_, ok = s.Get("channel-a", "other-peer", now)
	if ok {
		t.Fatal("Get wrong peer: expected miss")
	}

	s.Delete("channel-a", "peer-1")
	_, ok = s.Get("channel-a", "peer-1", now)
	if ok {
		t.Fatal("Get after Delete: expected miss")
	}
}

func TestStore_ExpiredIsMiss(t *testing.T) {
	s := New()
	now := time.Date(2026, 8, 11, 10, 0, 0, 0, time.UTC)
	expired := now.Add(-time.Minute)

	s.Put("channel-a", "peer-1", Entry{
		Agents:    []Agent{{ID: "agent-1", Name: "Alpha"}},
		ExpiresAt: expired,
	})

	_, ok := s.Get("channel-a", "peer-1", now)
	if ok {
		t.Fatal("Get expired: expected miss")
	}

	// Entry should be deleted on expired Get.
	_, ok = s.Get("channel-a", "peer-1", now)
	if ok {
		t.Fatal("Get after expired purge: expected miss")
	}
}
