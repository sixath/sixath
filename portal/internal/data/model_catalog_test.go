package data

import (
	"context"
	"testing"

	"backend/internal/data/model"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func openModelCatalogDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&model.ModelProvider{}, &model.ModelCatalogEntry{}, &model.ChatSession{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

func TestModelCatalog_CreateProviderAndManualEntry(t *testing.T) {
	db := openModelCatalogDB(t)
	st := NewModelCatalogStore(db)
	ctx := context.Background()
	p, err := st.CreateProvider(ctx, ProviderInput{
		Name: "relay", Kind: KindOpenAICompat, BaseURL: "https://relay.example/v1", APIKey: "sk-1", Enabled: true,
	})
	if err != nil || p.ID == "" || p.HasAPIKey != true {
		t.Fatalf("provider %+v err=%v", p, err)
	}
	got, err := st.GetProvider(ctx, p.ID)
	if err != nil || !got.HasAPIKey {
		t.Fatal("store must keep key internally; GetProvider DTO still no plain json field")
	}
	key, err := st.MustAPIKey(ctx, p.ID)
	if err != nil || key != "sk-1" {
		t.Fatalf("MustAPIKey=%q err=%v", key, err)
	}
	e, err := st.CreateEntry(ctx, CatalogInput{ProviderID: p.ID, Model: "deepseek-v3", DisplayName: "DS V3", Source: SourceManual})
	if err != nil || e.Model != "deepseek-v3" || e.DisplayName != "DS V3" {
		t.Fatalf("entry %+v err=%v", e, err)
	}
}

func TestModelCatalog_CreateProviderDisabledPersists(t *testing.T) {
	db := openModelCatalogDB(t)
	st := NewModelCatalogStore(db)
	ctx := context.Background()
	p, err := st.CreateProvider(ctx, ProviderInput{
		Name: "off", Kind: KindDashScope, APIKey: "sk-off", Enabled: false,
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := st.GetProvider(ctx, p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Enabled {
		t.Fatalf("Enabled=true after CreateProvider Enabled:false: %+v", got)
	}
}

func TestModelCatalog_CreateEntryRejectsEmptyModel(t *testing.T) {
	db := openModelCatalogDB(t)
	st := NewModelCatalogStore(db)
	ctx := context.Background()
	p, err := st.CreateProvider(ctx, ProviderInput{
		Name: "relay", Kind: KindOpenAICompat, BaseURL: "https://relay.example/v1", APIKey: "sk-1", Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateEntry(ctx, CatalogInput{ProviderID: p.ID, Model: "", DisplayName: "x", Source: SourceManual}); err == nil {
		t.Fatal("empty model must be rejected")
	}
}

func TestModelCatalog_CreateEntryRejectsMissingProvider(t *testing.T) {
	db := openModelCatalogDB(t)
	st := NewModelCatalogStore(db)
	ctx := context.Background()
	if _, err := st.CreateEntry(ctx, CatalogInput{ProviderID: "missing", Model: "m1", Source: SourceManual}); err == nil {
		t.Fatal("missing provider must be rejected")
	}
}

func TestChatSession_SetModelOverride(t *testing.T) {
	db := openModelCatalogDB(t)
	sessRepo := &chatSessionRepo{db: db}
	ctx := context.Background()
	sess, err := sessRepo.Create(ctx, "u1", "a1", "t", "")
	if err != nil {
		t.Fatal(err)
	}
	if sess.ModelProviderID != "" || sess.Model != "" {
		t.Fatal("new session must be agent default")
	}
	if err := sessRepo.SetModelOverride(ctx, sess.ID, "prov-1", "gpt-4o"); err != nil {
		t.Fatal(err)
	}
	got, err := sessRepo.GetByID(ctx, sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.ModelProviderID != "prov-1" || got.Model != "gpt-4o" {
		t.Fatalf("%+v", got)
	}
	if err := sessRepo.SetModelOverride(ctx, sess.ID, "", ""); err != nil {
		t.Fatal(err)
	}
	got, err = sessRepo.GetByID(ctx, sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.ModelProviderID != "" || got.Model != "" {
		t.Fatal("clear failed")
	}
}

func TestChatSession_SetModelOverrideIdempotent(t *testing.T) {
	db := openModelCatalogDB(t)
	sessRepo := &chatSessionRepo{db: db}
	ctx := context.Background()
	sess, err := sessRepo.Create(ctx, "u1", "a1", "t", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := sessRepo.SetModelOverride(ctx, sess.ID, "prov-1", "gpt-4o"); err != nil {
		t.Fatal(err)
	}
	if err := sessRepo.SetModelOverride(ctx, sess.ID, "prov-1", "gpt-4o"); err != nil {
		t.Fatalf("second identical overlay: %v", err)
	}
	got, err := sessRepo.GetByID(ctx, sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.ModelProviderID != "prov-1" || got.Model != "gpt-4o" {
		t.Fatalf("%+v", got)
	}
}
