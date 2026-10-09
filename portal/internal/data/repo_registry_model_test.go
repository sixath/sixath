package data

import (
	"testing"
	"time"

	"backend/internal/data/model"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestRepoGroupModel_FalseAutoApplyAndNilRule(t *testing.T) {
	dsn := "file:" + t.Name() + "?mode=memory&cache=shared"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&model.RepoGroup{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	now := time.Now()
	g := model.RepoGroup{ID: "g1", Name: "manual", Kind: "manual", AutoApplyNew: false, CreatedAt: now, UpdatedAt: now}
	if err := db.Create(&g).Error; err != nil {
		t.Fatalf("create: %v", err)
	}

	var got model.RepoGroup
	if err := db.First(&got, "id = ?", "g1").Error; err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.AutoApplyNew {
		t.Fatalf("AutoApplyNew = true, want false")
	}
	if got.Rule != nil {
		t.Fatalf("Rule = %+v, want nil", got.Rule)
	}
}
