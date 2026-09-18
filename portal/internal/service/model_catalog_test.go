package service

import (
	"context"
	"strings"
	"testing"

	"backend/internal/biz"
	"backend/internal/chat"
	"backend/internal/data"
	"backend/internal/data/model"

	kratosErrors "github.com/go-kratos/kratos/v2/errors"
	"github.com/go-kratos/kratos/v2/log"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func openServiceCatalogDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:svc_"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&model.ModelProvider{}, &model.ModelCatalogEntry{}, &model.ChatSession{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

func catalogReason(err error) string {
	if err == nil {
		return ""
	}
	return kratosErrors.FromError(err).Reason
}

func TestSetSessionModel_RejectsUnknown(t *testing.T) {
	sess := &biz.ChatSession{ID: "s1", UserID: "user-1", AgentID: "a1"}
	s := &ChatService{
		chatUC: biz.NewChatUsecase(&rewindSessRepo{sess: sess}, nil, nil, nil, nil),
		log:    log.NewHelper(log.DefaultLogger),
	}
	ctx := biz.WithCallerUserID(context.Background(), "user-1")
	err := s.SetSessionModel(ctx, "s1", SetSessionModelInput{ModelProviderID: "nope", Model: "missing"})
	if catalogReason(err) != "MODEL_CHOICE_INVALID" {
		t.Fatalf("err=%v want MODEL_CHOICE_INVALID", err)
	}
}

func TestListModelChoices_IncludesAgentDefault(t *testing.T) {
	db := openServiceCatalogDB(t)
	st := data.NewModelCatalogStore(db)
	ctx := biz.WithCallerUserID(context.Background(), "user-1")
	p, err := st.CreateProvider(ctx, data.ProviderInput{
		Name: "relay", Kind: data.KindOpenAICompat, BaseURL: "https://relay.example/v1", APIKey: "sk-1", Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateEntry(ctx, data.CatalogInput{ProviderID: p.ID, Model: "deepseek-v3", DisplayName: "DS V3", Source: data.SourceManual}); err != nil {
		t.Fatal(err)
	}
	agent := &biz.AgentMeta{
		ID: "a1", Name: "agent",
		ModelConfig: biz.ModelConfig{Provider: "openai", Model: "gpt-4"},
	}
	sess := &biz.ChatSession{ID: "s1", UserID: "user-1", AgentID: "a1", ModelProviderID: p.ID, Model: "deepseek-v3"}
	repo := &hybridAgentRepo{agent: agent}
	res := &hybridResourceRepo{res: &biz.Resource{
		ID: "res-1", Type: biz.ResourceTypeAgent, PayloadRef: "a1",
		OwnerUserID: "user-1", Visibility: biz.VisibilityPrivate,
	}}
	agentUC := biz.NewAgentUsecase(repo, res, biz.NewAccessChecker(res), t.TempDir(), log.NewStdLogger(nil))
	s := &ChatService{
		chatUC:  biz.NewChatUsecase(&rewindSessRepo{sess: sess}, nil, nil, nil, nil),
		agentUC: agentUC,
		catalog: st,
		log:     log.NewHelper(log.DefaultLogger),
	}
	out, err := s.ListModelChoices(ctx, "a1", "s1")
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Items) < 2 {
		t.Fatalf("items=%+v", out.Items)
	}
	if out.Items[0].ID != "agent_default" {
		t.Fatalf("first=%+v", out.Items[0])
	}
	if !strings.Contains(out.Items[0].Label, "openai") || !strings.Contains(out.Items[0].Label, "gpt-4") {
		t.Fatalf("label=%q", out.Items[0].Label)
	}
	if out.Selected == nil || out.Selected.ProviderID != p.ID || out.Selected.Model != "deepseek-v3" {
		t.Fatalf("selected=%+v", out.Selected)
	}
}

func TestSetSessionModel_Clear(t *testing.T) {
	sess := &biz.ChatSession{ID: "s1", UserID: "user-1", AgentID: "a1", ModelProviderID: "p1", Model: "m1"}
	repo := &rewindSessRepo{sess: sess}
	s := &ChatService{
		chatUC: biz.NewChatUsecase(repo, nil, nil, nil, nil),
		log:    log.NewHelper(log.DefaultLogger),
	}
	ctx := biz.WithCallerUserID(context.Background(), "user-1")
	if err := s.SetSessionModel(ctx, "s1", SetSessionModelInput{Choice: "agent_default"}); err != nil {
		t.Fatal(err)
	}
	got, err := repo.GetByID(ctx, "s1")
	if err != nil {
		t.Fatal(err)
	}
	if got.ModelProviderID != "" || got.Model != "" {
		t.Fatalf("%+v", got)
	}
}

func TestCreateProvider_RequiresCaller(t *testing.T) {
	s := &ChatService{log: log.NewHelper(log.DefaultLogger)}
	_, err := s.CreateProvider(context.Background(), data.ProviderInput{
		Name: "x", Kind: data.KindOpenAICompat, BaseURL: "https://x.example/v1", Enabled: true,
	})
	if catalogReason(err) != "UNAUTHORIZED" {
		t.Fatalf("err=%v want UNAUTHORIZED", err)
	}
}

func TestSetSessionModel_ThenResolveUsesOverlay(t *testing.T) {
	db := openServiceCatalogDB(t)
	st := data.NewModelCatalogStore(db)
	ctx := biz.WithCallerUserID(context.Background(), "user-1")
	p, err := st.CreateProvider(ctx, data.ProviderInput{
		Name: "relay", Kind: data.KindOpenAICompat, BaseURL: "https://relay.example/v1", APIKey: "sk-relay", Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateEntry(ctx, data.CatalogInput{ProviderID: p.ID, Model: "deepseek-v3", Source: data.SourceManual}); err != nil {
		t.Fatal(err)
	}
	sess := &biz.ChatSession{ID: "s1", UserID: "user-1", AgentID: "a1"}
	sessRepo := &rewindSessRepo{sess: sess}
	s := &ChatService{
		chatUC:  biz.NewChatUsecase(sessRepo, nil, nil, nil, nil),
		catalog: st,
		log:     log.NewHelper(log.DefaultLogger),
	}
	if err := s.SetSessionModel(ctx, "s1", SetSessionModelInput{ModelProviderID: p.ID, Model: "deepseek-v3"}); err != nil {
		t.Fatal(err)
	}
	got, err := sessRepo.GetByID(ctx, "s1")
	if err != nil {
		t.Fatal(err)
	}
	agent := &biz.AgentMeta{ModelConfig: biz.ModelConfig{Provider: "openai", Model: "gpt-4", APIKey: "ak", MaxOutputTokens: 128}}
	cfg, err := chat.ResolveTurnModelConfig(ctx, st, agent, got)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Model != "deepseek-v3" || cfg.Provider != "openai" || cfg.APIKey != "sk-relay" {
		t.Fatalf("%+v", cfg)
	}
	if cfg.MaxOutputTokens != 128 {
		t.Fatalf("max_output_tokens=%d", cfg.MaxOutputTokens)
	}
}
