package chat

import (
	"testing"
	"time"

	"github.com/sixath/framework/memory"
)

func TestParseBackfillArgs(t *testing.T) {
	cfg, err := ParseBackfillArgs([]string{"--force", "--dry-run", "--scope", "session", "--batch", "10", "--sleep", "0s"})
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Force || !cfg.DryRun {
		t.Fatalf("force/dry-run: %+v", cfg)
	}
	if len(cfg.Scopes) != 1 || cfg.Scopes[0] != memory.ScopeSession {
		t.Fatalf("scopes=%v", cfg.Scopes)
	}
	if cfg.BatchSize != 10 || cfg.BatchSleep != 0 {
		t.Fatalf("batch/sleep: %+v", cfg)
	}

	cfgAll, err := ParseBackfillArgs([]string{"--scope", "all"})
	if err != nil {
		t.Fatal(err)
	}
	if len(cfgAll.Scopes) != 2 || cfgAll.Scopes[0] != memory.ScopeSession || cfgAll.Scopes[1] != memory.ScopeUser {
		t.Fatalf("all scopes=%v", cfgAll.Scopes)
	}

	defaults, err := ParseBackfillArgs(nil)
	if err != nil {
		t.Fatal(err)
	}
	if defaults.BatchSize != 50 || defaults.BatchSleep != 200*time.Millisecond {
		t.Fatalf("defaults: %+v", defaults)
	}
	if len(defaults.Scopes) != 2 {
		t.Fatalf("default scopes=%v", defaults.Scopes)
	}
}

func TestParseBackfillArgs_Unknown(t *testing.T) {
	_, err := ParseBackfillArgs([]string{"--nope"})
	if err == nil {
		t.Fatal("expected error")
	}
}
