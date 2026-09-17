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
	if p.APIKey != "" { // public DTO 不得带明文
		t.Fatal("api_key leaked on create result")
	}
	got, err := st.GetProvider(ctx, p.ID)
	if err != nil || !got.HasAPIKey {
		t.Fatal("store must keep key internally; GetProvider DTO still no plain json field")
	}
	if got.APIKey != "" {
		t.Fatal("api_key leaked on get result")
	}
	key, err := st.MustAPIKey(ctx, p.ID)
	if err != nil || key == "" {
		t.Fatal("store must keep key internally")
	}
	e, err := st.CreateEntry(ctx, CatalogInput{ProviderID: p.ID, Model: "deepseek-v3", DisplayName: "DS V3", Source: SourceManual})
	if err != nil || e.Model != "deepseek-v3" {
		t.Fatal(err)
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
	got, _ := sessRepo.GetByID(ctx, sess.ID)
	if got.ModelProviderID != "prov-1" || got.Model != "gpt-4o" {
		t.Fatalf("%+v", got)
	}
	if err := sessRepo.SetModelOverride(ctx, sess.ID, "", ""); err != nil {
		t.Fatal(err)
	}
	got, _ = sessRepo.GetByID(ctx, sess.ID)
	if got.ModelProviderID != "" || got.Model != "" {
		t.Fatal("clear failed")
	}
}
