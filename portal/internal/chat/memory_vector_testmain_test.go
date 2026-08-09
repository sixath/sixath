package chat

import (
	"os"
	"testing"

	"github.com/sixath/framework/config"
)

// TestMain pins the vector sidecar off for the whole package binary. Production
// DefaultMemoryStoreOptions opens sqlite when config is omitted; without this
// pin, every existing test that calls BuildMemoryStore / DefaultMemoryStoreOptions
// (memory_conflict_test, hermes_*, browser_wiring_test, memory_extract_pipeline_test,
// …) would create portal/internal/chat/data/memory_unit_vectors.db under the
// package cwd — a path NOT covered by portal/.gitignore's repo-root /data/ rule.
func TestMain(m *testing.M) {
	SetMemoryVectorConfig(&config.MemoryVector{Provider: "none"})
	os.Exit(m.Run())
}
