package pendingswitch

import (
	"sync"
	"time"
)

// Agent is a selectable agent shown on a switch card.
type Agent struct {
	ID   string
	Name string
}

// Entry holds pending switch options and expiry.
type Entry struct {
	Agents    []Agent
	ExpiresAt time.Time
}

// Store is an in-memory pending switch map keyed by channel and peer.
type Store struct {
	mu sync.Mutex
	m  map[string]Entry
}

// New creates an empty pending switch store.
func New() *Store {
	return &Store{
		m: make(map[string]Entry),
	}
}

func key(channelID, peerID string) string {
	return channelID + "\x00" + peerID
}

func copyAgents(agents []Agent) []Agent {
	if len(agents) == 0 {
		return nil
	}
	out := make([]Agent, len(agents))
	copy(out, agents)
	return out
}

// Put stores a pending switch entry for channelID and peerID.
func (s *Store) Put(channelID, peerID string, e Entry) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.m[key(channelID, peerID)] = Entry{
		Agents:    copyAgents(e.Agents),
		ExpiresAt: e.ExpiresAt,
	}
}

// Get returns a non-expired entry. Expired entries are deleted and reported as missing.
func (s *Store) Get(channelID, peerID string, now time.Time) (Entry, bool) {
	if s == nil {
		return Entry{}, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	k := key(channelID, peerID)
	e, ok := s.m[k]
	if !ok {
		return Entry{}, false
	}
	if !e.ExpiresAt.IsZero() && !now.Before(e.ExpiresAt) {
		delete(s.m, k)
		return Entry{}, false
	}
	return Entry{
		Agents:    copyAgents(e.Agents),
		ExpiresAt: e.ExpiresAt,
	}, true
}

// Delete removes a pending switch entry.
func (s *Store) Delete(channelID, peerID string) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.m, key(channelID, peerID))
}
