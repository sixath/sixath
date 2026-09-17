package data

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
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

func TestModelCatalog_SyncUpsertKeepsOverrides(t *testing.T) {
	db := openModelCatalogDB(t)
	st := NewModelCatalogStore(db)
	ctx := context.Background()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/models" && r.URL.Path != "/v1/models" {
			t.Errorf("path %s", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); !strings.Contains(got, "sk-1") {
			t.Errorf("auth %s", got)
		}
		_, _ = w.Write([]byte(`{"data":[{"id":"gpt-4o"},{"id":"gpt-4"}]}`))
	}))
	t.Cleanup(srv.Close)
	p, err := st.CreateProvider(ctx, ProviderInput{Name: "o", Kind: KindOpenAICompat, BaseURL: srv.URL, APIKey: "sk-1", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	e, err := st.CreateEntry(ctx, CatalogInput{ProviderID: p.ID, Model: "gpt-4o", DisplayName: "Custom", Source: SourceSync})
	if err != nil {
		t.Fatal(err)
	}
	display := "Custom"
	hidden := true
	if err := st.PatchEntry(ctx, EntryPatch{ID: e.ID, DisplayName: &display, Hidden: &hidden}); err != nil {
		t.Fatal(err)
	}
	n, err := st.Sync(ctx, p.ID)
	if err != nil || n < 1 {
		t.Fatalf("sync n=%d err=%v", n, err)
	}
	list, err := st.ListEntries(ctx, p.ID)
	if err != nil {
		t.Fatal(err)
	}
	var four, fourO *CatalogView
	for i := range list {
		if list[i].Model == "gpt-4" {
			four = &list[i]
		}
		if list[i].Model == "gpt-4o" {
			fourO = &list[i]
		}
	}
	if four == nil || fourO == nil {
		t.Fatalf("list %+v", list)
	}
	if fourO.DisplayName != "Custom" || !fourO.Hidden {
		t.Fatalf("override lost %+v", fourO)
	}
}

func TestModelCatalog_SyncDoesNotDeleteMissing(t *testing.T) {
	db := openModelCatalogDB(t)
	st := NewModelCatalogStore(db)
	ctx := context.Background()
	var round atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/models" && r.URL.Path != "/v1/models" {
			t.Errorf("path %s", r.URL.Path)
		}
		if round.Add(1) == 1 {
			_, _ = w.Write([]byte(`{"data":[{"id":"gpt-4o"},{"id":"gpt-4"}]}`))
			return
		}
		_, _ = w.Write([]byte(`{"data":[{"id":"gpt-4o"}]}`))
	}))
	t.Cleanup(srv.Close)
	p, err := st.CreateProvider(ctx, ProviderInput{Name: "o", Kind: KindOpenAICompat, BaseURL: srv.URL, APIKey: "sk-1", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.Sync(ctx, p.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Sync(ctx, p.ID); err != nil {
		t.Fatal(err)
	}
	list, err := st.ListEntries(ctx, p.ID)
	if err != nil {
		t.Fatal(err)
	}
	var four, fourO *CatalogView
	for i := range list {
		if list[i].Model == "gpt-4" {
			four = &list[i]
		}
		if list[i].Model == "gpt-4o" {
			fourO = &list[i]
		}
	}
	if four == nil || fourO == nil {
		t.Fatalf("gpt-4 must remain after second sync; list %+v", list)
	}
}

func TestModelCatalog_SyncDashScopeRejected(t *testing.T) {
	db := openModelCatalogDB(t)
	st := NewModelCatalogStore(db)
	ctx := context.Background()
	p, err := st.CreateProvider(ctx, ProviderInput{Name: "ds", Kind: KindDashScope, APIKey: "sk-ds", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.Sync(ctx, p.ID); err == nil {
		t.Fatal("expected error")
	}
}
