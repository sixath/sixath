package chat

import (
	"crypto/sha256"
	"fmt"
	"strings"
	"sync"
)

// trialEntry holds accumulated data for one problem pattern.
type trialEntry struct {
	AgentID       string
	Problem       string
	FailCounts    []int
	SuccessSteps  []string
	Occurrence    int
	State         string // observing, proposed
	ObserveWindow int
	ObserveCount  int
}

// TrialBuffer is an in-process LRU buffer for trial-and-error detection.
// Thread-safe, capacity 256 entries.
type TrialBuffer struct {
	mu       sync.Mutex
	entries  map[string]*trialEntry
	keys     []string // LRU order
	capacity int
}

// NewTrialBuffer creates a buffer with the given capacity.
func NewTrialBuffer(capacity int) *TrialBuffer {
	if capacity <= 0 {
		capacity = 256
	}
	return &TrialBuffer{
		entries:  make(map[string]*trialEntry),
		capacity: capacity,
	}
}

// problemKey generates a hash key for an agent + problem combination.
func problemKey(agentID, problem string) string {
	h := sha256.Sum256([]byte(agentID + problem))
	return fmt.Sprintf("%x", h[:12])
}

// Record records a trial-and-error occurrence. Returns true if the threshold
// is met and a proposal should be generated.
func (b *TrialBuffer) Record(agentID, problem string, failCount int, successSteps []string, minFailures, minOccurrences int) (met bool) {
	if agentID == "" || problem == "" {
		return false
	}
	b.mu.Lock()
	defer b.mu.Unlock()

	key := problemKey(agentID, problem)
	entry, exists := b.entries[key]
	if !exists {
		// Evict oldest if at capacity
		for len(b.keys) >= b.capacity {
			oldest := b.keys[0]
			b.keys = b.keys[1:]
			delete(b.entries, oldest)
		}
		entry = &trialEntry{
			AgentID: agentID,
			Problem: problem,
			State:   "observing",
		}
		b.entries[key] = entry
		b.keys = append(b.keys, key)
	} else {
		// Move to end (LRU)
		for i, k := range b.keys {
			if k == key {
				b.keys = append(b.keys[:i], b.keys[i+1:]...)
				break
			}
		}
		b.keys = append(b.keys, key)
	}

	if failCount < minFailures {
		return false
	}

	entry.FailCounts = append(entry.FailCounts, failCount)
	entry.Occurrence++

	// Accumulate success steps: longer path wins
	if len(successSteps) > len(entry.SuccessSteps) {
		entry.SuccessSteps = successSteps
	} else if len(successSteps) == len(entry.SuccessSteps) && len(successSteps) > 0 {
		entry.SuccessSteps = successSteps // latest wins on tie
	}

	if entry.Occurrence >= minOccurrences && entry.State == "observing" {
		entry.State = "proposed"
		return true
	}
	return false
}

// Get returns entry data for proposal generation, or nil if not found.
func (b *TrialBuffer) Get(agentID, problem string) *trialEntry {
	b.mu.Lock()
	defer b.mu.Unlock()
	key := problemKey(agentID, problem)
	return b.entries[key]
}

// Remove removes an entry (called after proposal is reviewed).
func (b *TrialBuffer) Remove(agentID, problem string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	key := problemKey(agentID, problem)
	delete(b.entries, key)
	for i, k := range b.keys {
		if k == key {
			b.keys = append(b.keys[:i], b.keys[i+1:]...)
			break
		}
	}
}

// AdvanceObservation increments observation counters and removes stale entries.
func (b *TrialBuffer) AdvanceObservation(maxWindow int) []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	var remove []string
	for key, entry := range b.entries {
		if entry.State == "observing" {
			entry.ObserveCount++
			if entry.ObserveCount > maxWindow {
				remove = append(remove, key)
			}
		}
	}
	for _, key := range remove {
		delete(b.entries, key)
		for i, k := range b.keys {
			if k == key {
				b.keys = append(b.keys[:i], b.keys[i+1:]...)
				break
			}
		}
	}
	return remove
}

// factualErrorPatterns defines error substrings that indicate a factual/reproducible error.
var factualErrorPatterns = []string{
	"type mismatch",
	"cannot convert",
	"cannot cast",
	"column count",
	"data type",
	"column",
	"field",
	"no such column",
	"unknown column",
	"table",
	"relation",
	"no such table",
	"does not exist",
	"view",
	"sequence",
	"function",
	"procedure",
	"schema",
	"index",
	"no such index",
	"database",
	"no such database",
	"syntax error",
	"parse error",
	"protocol error",
	"duplicate key",
	"unique constraint",
	"foreign key",
	"cannot be null",
	"check constraint",
	"constraint",
	"value out of range",
	"overflow",
	"division by zero",
}

// IsFactualError reports whether a tool error message indicates a factual/reproducible error.
func IsFactualError(errMsg string) bool {
	if errMsg == "" {
		return false
	}
	lower := strings.ToLower(errMsg)
	for _, pat := range factualErrorPatterns {
		if strings.Contains(lower, pat) {
			return true
		}
	}
	return false
}