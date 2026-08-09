package data

import (
	"testing"

	"backend/internal/data/model"

	"github.com/sixath/framework/memory/hub"
	"github.com/sixath/framework/memory/hub/local"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestMySQLBindingStore_RoundTrip(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:bindings?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.AgentAssetBinding{}); err != nil {
		t.Fatal(err)
	}
	store := NewMySQLBindingStore(db)
	var _ local.BindingStore = store

	if err := store.Upsert(local.Binding{
		AgentID:   "a1",
		AssetKind: hub.AssetKindSkill,
		AssetID:   "s1",
		Hub:       "local",
		Name:      "s1",
		Status:    hub.AssetActive,
	}); err != nil {
		t.Fatal(err)
	}
	list, err := store.ListByAgent("a1")
	if err != nil || len(list) != 1 || list[0].AssetID != "s1" {
		t.Fatalf("%v %#v", err, list)
	}
	if err := store.Delete("a1", hub.AssetKindSkill, "s1"); err != nil {
		t.Fatal(err)
	}
	list, err = store.ListByAgent("a1")
	if err != nil || len(list) != 0 {
		t.Fatalf("%v %#v", err, list)
	}
}
