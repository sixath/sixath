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

func TestModelCatalog_GetProviderSecret(t *testing.T) {
	db := openModelCatalogDB(t)
	st := NewModelCatalogStore(db)
	ctx := context.Background()
	p, err := st.CreateProvider(ctx, ProviderInput{
		Name: "relay", Kind: KindOpenAICompat, BaseURL: "https://relay.example/v1", APIKey: "sk-secret", Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	kind, baseURL, apiKey, enabled, err := st.GetProviderSecret(ctx, p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if kind != KindOpenAICompat || baseURL != "https://relay.example/v1" || apiKey != "sk-secret" || !enabled {
		t.Fatalf("kind=%q baseURL=%q apiKey=%q enabled=%v", kind, baseURL, apiKey, enabled)
	}
}

func TestModelCatalog_GetProviderSecret_NotFound(t *testing.T) {
	db := openModelCatalogDB(t)
	st := NewModelCatalogStore(db)
	ctx := context.Background()
	_, _, _, _, err := st.GetProviderSecret(ctx, "missing-provider")
	if err != ErrNotFound {
		t.Fatalf("err=%v want ErrNotFound", err)
	}
	if strings.Contains(err.Error(), "sk-") || strings.Contains(err.Error(), "api_key") {
		t.Fatalf("error must not contain key: %v", err)
	}
}

func TestModelCatalog_HasUsableEntry(t *testing.T) {
	db := openModelCatalogDB(t)
	st := NewModelCatalogStore(db)
	ctx := context.Background()
	p, err := st.CreateProvider(ctx, ProviderInput{
		Name: "relay", Kind: KindOpenAICompat, BaseURL: "https://relay.example/v1", APIKey: "sk-1", Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateEntry(ctx, CatalogInput{ProviderID: p.ID, Model: "deepseek-v3", Source: SourceManual}); err != nil {
		t.Fatal(err)
	}
	ok, err := st.HasUsableEntry(ctx, p.ID, "deepseek-v3")
	if err != nil || !ok {
		t.Fatalf("ok=%v err=%v want true", ok, err)
	}
	ok, err = st.HasUsableEntry(ctx, p.ID, "other-model")
	if err != nil || ok {
		t.Fatalf("exact mismatch ok=%v err=%v want false", ok, err)
	}
}

func TestModelCatalog_HasUsableEntry_FalseCases(t *testing.T) {
	db := openModelCatalogDB(t)
	st := NewModelCatalogStore(db)
	ctx := context.Background()

	ok, err := st.HasUsableEntry(ctx, "missing", "m1")
	if err != nil || ok {
		t.Fatalf("missing provider ok=%v err=%v", ok, err)
	}

	disabled, err := st.CreateProvider(ctx, ProviderInput{
		Name: "off", Kind: KindDashScope, APIKey: "sk-off", Enabled: false,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateEntry(ctx, CatalogInput{ProviderID: disabled.ID, Model: "qwen-max", Source: SourceManual}); err != nil {
		t.Fatal(err)
	}
	ok, err = st.HasUsableEntry(ctx, disabled.ID, "qwen-max")
	if err != nil || ok {
		t.Fatalf("disabled ok=%v err=%v", ok, err)
	}

	noKey, err := st.CreateProvider(ctx, ProviderInput{
		Name: "empty", Kind: KindDashScope, APIKey: "", Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateEntry(ctx, CatalogInput{ProviderID: noKey.ID, Model: "qwen-max", Source: SourceManual}); err != nil {
		t.Fatal(err)
	}
	ok, err = st.HasUsableEntry(ctx, noKey.ID, "qwen-max")
	if err != nil || ok {
		t.Fatalf("empty key ok=%v err=%v", ok, err)
	}

	hiddenProv, err := st.CreateProvider(ctx, ProviderInput{
		Name: "hid", Kind: KindOpenAICompat, BaseURL: "https://relay.example/v1", APIKey: "sk-h", Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	e, err := st.CreateEntry(ctx, CatalogInput{ProviderID: hiddenProv.ID, Model: "hidden-model", Source: SourceManual})
	if err != nil {
		t.Fatal(err)
	}
	hidden := true
	if err := st.PatchEntry(ctx, EntryPatch{ID: e.ID, Hidden: &hidden}); err != nil {
		t.Fatal(err)
	}
	ok, err = st.HasUsableEntry(ctx, hiddenProv.ID, "hidden-model")
	if err != nil || ok {
		t.Fatalf("hidden ok=%v err=%v", ok, err)
	}
}

func TestModelCatalog_ListProviders(t *testing.T) {
	db := openModelCatalogDB(t)
	st := NewModelCatalogStore(db)
	ctx := context.Background()
	a, err := st.CreateProvider(ctx, ProviderInput{Name: "a", Kind: KindDashScope, APIKey: "sk-a", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	b, err := st.CreateProvider(ctx, ProviderInput{Name: "b", Kind: KindOpenAICompat, BaseURL: "https://b.example/v1", APIKey: "sk-b", Enabled: false})
	if err != nil {
		t.Fatal(err)
	}
	list, err := st.ListProviders(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 {
		t.Fatalf("len=%d", len(list))
	}
	ids := map[string]ProviderView{}
	for _, p := range list {
		ids[p.ID] = p
	}
	if _, ok := ids[a.ID]; !ok {
		t.Fatal("missing a")
	}
	if got := ids[b.ID]; got.Enabled {
		t.Fatalf("b should stay disabled %+v", got)
	}
}

func TestModelCatalog_PatchProvider(t *testing.T) {
	db := openModelCatalogDB(t)
	st := NewModelCatalogStore(db)
	ctx := context.Background()
	p, err := st.CreateProvider(ctx, ProviderInput{
		Name: "relay", Kind: KindOpenAICompat, BaseURL: "https://relay.example/v1", APIKey: "sk-1", Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	name := "relay-2"
	got, err := st.PatchProvider(ctx, p.ID, ProviderPatch{Name: &name})
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "relay-2" {
		t.Fatalf("name=%q", got.Name)
	}
	key, err := st.MustAPIKey(ctx, p.ID)
	if err != nil || key != "sk-1" {
		t.Fatalf("key must be unchanged: %q err=%v", key, err)
	}
	empty := ""
	got, err = st.PatchProvider(ctx, p.ID, ProviderPatch{APIKey: &empty})
	if err != nil {
		t.Fatal(err)
	}
	if got.HasAPIKey {
		t.Fatal("empty api_key must clear")
	}
	enabled := false
	got, err = st.PatchProvider(ctx, p.ID, ProviderPatch{Enabled: &enabled})
	if err != nil {
		t.Fatal(err)
	}
	if got.Enabled {
		t.Fatalf("Enabled=true after patch false: %+v", got)
	}
}

func TestModelCatalog_DeleteProviderCascadesEntries(t *testing.T) {
	db := openModelCatalogDB(t)
	st := NewModelCatalogStore(db)
	sessRepo := &chatSessionRepo{db: db}
	ctx := context.Background()
	p, err := st.CreateProvider(ctx, ProviderInput{
		Name: "relay", Kind: KindOpenAICompat, BaseURL: "https://relay.example/v1", APIKey: "sk-1", Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateEntry(ctx, CatalogInput{ProviderID: p.ID, Model: "m1", Source: SourceManual}); err != nil {
		t.Fatal(err)
	}
	sess, err := sessRepo.Create(ctx, "u1", "a1", "t", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := sessRepo.SetModelOverride(ctx, sess.ID, p.ID, "m1"); err != nil {
		t.Fatal(err)
	}
	if err := st.DeleteProvider(ctx, p.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := st.GetProvider(ctx, p.ID); err != ErrNotFound {
		t.Fatalf("provider err=%v", err)
	}
	entries, err := st.ListEntries(ctx, p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("entries leftover %+v", entries)
	}
	got, err := sessRepo.GetByID(ctx, sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.ModelProviderID != p.ID || got.Model != "m1" {
		t.Fatalf("session overlay must stay %+v", got)
	}
}

func TestModelCatalog_ListUsable(t *testing.T) {
	db := openModelCatalogDB(t)
	st := NewModelCatalogStore(db)
	ctx := context.Background()
	okProv, err := st.CreateProvider(ctx, ProviderInput{
		Name: "ok", Kind: KindOpenAICompat, BaseURL: "https://ok.example/v1", APIKey: "sk-ok", Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateEntry(ctx, CatalogInput{ProviderID: okProv.ID, Model: "usable", DisplayName: "Usable", Source: SourceManual}); err != nil {
		t.Fatal(err)
	}
	off, err := st.CreateProvider(ctx, ProviderInput{Name: "off", Kind: KindDashScope, APIKey: "sk-off", Enabled: false})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateEntry(ctx, CatalogInput{ProviderID: off.ID, Model: "qwen", Source: SourceManual}); err != nil {
		t.Fatal(err)
	}
	emptyKey, err := st.CreateProvider(ctx, ProviderInput{Name: "empty", Kind: KindDashScope, APIKey: "", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateEntry(ctx, CatalogInput{ProviderID: emptyKey.ID, Model: "qwen2", Source: SourceManual}); err != nil {
		t.Fatal(err)
	}
	hiddenProv, err := st.CreateProvider(ctx, ProviderInput{
		Name: "hid", Kind: KindOpenAICompat, BaseURL: "https://h.example/v1", APIKey: "sk-h", Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	he, err := st.CreateEntry(ctx, CatalogInput{ProviderID: hiddenProv.ID, Model: "hidden", Source: SourceManual})
	if err != nil {
		t.Fatal(err)
	}
	hidden := true
	if err := st.PatchEntry(ctx, EntryPatch{ID: he.ID, Hidden: &hidden}); err != nil {
		t.Fatal(err)
	}
	list, err := st.ListUsable(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].Model != "usable" || list[0].ProviderID != okProv.ID || list[0].ProviderName != "ok" || list[0].Kind != KindOpenAICompat {
		t.Fatalf("%+v", list)
	}
}

func TestModelCatalog_DeleteEntry(t *testing.T) {
	db := openModelCatalogDB(t)
	st := NewModelCatalogStore(db)
	ctx := context.Background()
	p, err := st.CreateProvider(ctx, ProviderInput{
		Name: "relay", Kind: KindOpenAICompat, BaseURL: "https://relay.example/v1", APIKey: "sk-1", Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	e, err := st.CreateEntry(ctx, CatalogInput{ProviderID: p.ID, Model: "m1", Source: SourceManual})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.DeleteEntry(ctx, e.ID); err != nil {
		t.Fatal(err)
	}
	list, err := st.ListEntries(ctx, p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 0 {
		t.Fatalf("leftover %+v", list)
	}
}

func TestModelCatalog_CreateEntryCustomDisplayNameOverridden(t *testing.T) {
	db := openModelCatalogDB(t)
	st := NewModelCatalogStore(db)
	ctx := context.Background()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"data":[{"id":"deepseek-v3"}]}`))
	}))
	t.Cleanup(srv.Close)
	p, err := st.CreateProvider(ctx, ProviderInput{
		Name: "relay", Kind: KindOpenAICompat, BaseURL: srv.URL, APIKey: "sk-1", Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateEntry(ctx, CatalogInput{ProviderID: p.ID, Model: "deepseek-v3", DisplayName: "DS V3", Source: SourceManual}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Sync(ctx, p.ID); err != nil {
		t.Fatal(err)
	}
	list, err := st.ListEntries(ctx, p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].DisplayName != "DS V3" {
		t.Fatalf("custom display name overwritten %+v", list)
	}
}

func TestModelCatalog_ListEntriesAll(t *testing.T) {
	db := openModelCatalogDB(t)
	st := NewModelCatalogStore(db)
	ctx := context.Background()
	a, err := st.CreateProvider(ctx, ProviderInput{Name: "a", Kind: KindDashScope, APIKey: "sk-a", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	b, err := st.CreateProvider(ctx, ProviderInput{Name: "b", Kind: KindDashScope, APIKey: "sk-b", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateEntry(ctx, CatalogInput{ProviderID: a.ID, Model: "m-a", Source: SourceManual}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateEntry(ctx, CatalogInput{ProviderID: b.ID, Model: "m-b", Source: SourceManual}); err != nil {
		t.Fatal(err)
	}
	list, err := st.ListEntries(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 {
		t.Fatalf("len=%d %+v", len(list), list)
	}
}
