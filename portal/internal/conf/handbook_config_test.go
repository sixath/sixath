package conf

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadHandbookFromConfigPath_readsSection(t *testing.T) {
	dir := t.TempDir()
	const yaml = `
handbook:
  model: "qwen/qwen-max"
  concurrency: 2
  max_cards_per_run: 100
  max_file_kb: 16
  max_run_minutes: 10
  skeleton_rebuild_days: 7
`
	if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SATH_HANDBOOK_MODEL", "")
	cfg, err := LoadHandbookFromConfigPath(dir)
	if err != nil {
		t.Fatal(err)
	}
	want := HandbookConfig{Model: "qwen/qwen-max", Concurrency: 2, MaxCardsPerRun: 100, MaxFileKB: 16, MaxRunMinutes: 10, SkeletonRebuildDays: 7}
	if *cfg != want {
		t.Fatalf("got %+v, want %+v", *cfg, want)
	}
}

func TestLoadHandbookFromConfigPath_envOverridesModel(t *testing.T) {
	dir := t.TempDir()
	const yaml = `
handbook:
  model: "qwen/qwen-max"
  concurrency: 2
`
	if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SATH_HANDBOOK_MODEL", " x/y ")
	cfg, err := LoadHandbookFromConfigPath(dir)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Model != "x/y" || cfg.Concurrency != 2 {
		t.Fatalf("got %+v, want model x/y and concurrency 2", *cfg)
	}
}

func TestLoadHandbookFromConfigPath_missingFileIsZero(t *testing.T) {
	t.Setenv("SATH_HANDBOOK_MODEL", "")
	cfg, err := LoadHandbookFromConfigPath(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if cfg == nil || *cfg != (HandbookConfig{}) {
		t.Fatalf("got %+v, want zero config", cfg)
	}
}

func TestLoadHandbookFromConfigPath_shippedConfigDisabled(t *testing.T) {
	t.Setenv("SATH_HANDBOOK_MODEL", "")
	cfg, err := LoadHandbookFromConfigPath(filepath.Join("..", "..", "configs"))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Model != "" || cfg.Concurrency != 4 || cfg.MaxCardsPerRun != 600 {
		t.Fatalf("shipped handbook config = %+v, want disabled model with documented defaults", *cfg)
	}
}
