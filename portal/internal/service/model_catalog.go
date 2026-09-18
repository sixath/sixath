package service

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"backend/internal/biz"
	"backend/internal/chat"
	"backend/internal/data"
	pkgErrors "backend/internal/pkg/errors"

	kratosErrors "github.com/go-kratos/kratos/v2/errors"
)

var errCatalogStoreUnavailable = kratosErrors.InternalServer("MODEL_CATALOG_UNAVAILABLE", "model catalog store unavailable")

func requireLogin(ctx context.Context) error {
	if _, ok := biz.CallerUserID(ctx); !ok {
		return kratosErrors.Unauthorized("UNAUTHORIZED", "login required")
	}
	return nil
}

func (s *ChatService) requireCatalog() (*data.ModelCatalogStore, error) {
	if s == nil || s.catalog == nil {
		return nil, errCatalogStoreUnavailable
	}
	return s.catalog, nil
}

func mapCatalogErr(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, pkgErrors.ErrNotFound) || errors.Is(err, data.ErrNotFound) {
		return kratosErrors.NotFound("MODEL_PROVIDER_NOT_FOUND", "model provider or catalog entry not found")
	}
	if errors.Is(err, pkgErrors.ErrDuplicateName) || errors.Is(err, data.ErrDuplicateName) {
		return kratosErrors.Conflict("MODEL_CATALOG_DUPLICATE", "catalog entry already exists")
	}
	msg := err.Error()
	switch {
	case strings.Contains(msg, "invalid provider kind"),
		strings.Contains(msg, "requires base_url"),
		strings.Contains(msg, "provider_id and model"),
		strings.Contains(msg, "invalid catalog source"),
		strings.Contains(msg, "sync is only supported"):
		return kratosErrors.BadRequest("INVALID_ARGUMENT", msg)
	default:
		return err
	}
}

func (s *ChatService) CreateProvider(ctx context.Context, in data.ProviderInput) (*data.ProviderView, error) {
	if err := requireLogin(ctx); err != nil {
		return nil, err
	}
	st, err := s.requireCatalog()
	if err != nil {
		return nil, err
	}
	out, err := st.CreateProvider(ctx, in)
	if err != nil {
		return nil, mapCatalogErr(err)
	}
	return out, nil
}

func (s *ChatService) ListProviders(ctx context.Context) ([]data.ProviderView, error) {
	if err := requireLogin(ctx); err != nil {
		return nil, err
	}
	st, err := s.requireCatalog()
	if err != nil {
		return nil, err
	}
	out, err := st.ListProviders(ctx)
	if err != nil {
		return nil, mapCatalogErr(err)
	}
	return out, nil
}

func (s *ChatService) GetProvider(ctx context.Context, id string) (*data.ProviderView, error) {
	if err := requireLogin(ctx); err != nil {
		return nil, err
	}
	st, err := s.requireCatalog()
	if err != nil {
		return nil, err
	}
	out, err := st.GetProvider(ctx, id)
	if err != nil {
		return nil, mapCatalogErr(err)
	}
	return out, nil
}

func (s *ChatService) PatchProvider(ctx context.Context, id string, patch data.ProviderPatch) (*data.ProviderView, error) {
	if err := requireLogin(ctx); err != nil {
		return nil, err
	}
	st, err := s.requireCatalog()
	if err != nil {
		return nil, err
	}
	out, err := st.PatchProvider(ctx, id, patch)
	if err != nil {
		return nil, mapCatalogErr(err)
	}
	return out, nil
}

func (s *ChatService) DeleteProvider(ctx context.Context, id string) error {
	if err := requireLogin(ctx); err != nil {
		return err
	}
	st, err := s.requireCatalog()
	if err != nil {
		return err
	}
	return mapCatalogErr(st.DeleteProvider(ctx, id))
}

func (s *ChatService) SyncProvider(ctx context.Context, id string) (int, error) {
	if err := requireLogin(ctx); err != nil {
		return 0, err
	}
	st, err := s.requireCatalog()
	if err != nil {
		return 0, err
	}
	n, err := st.Sync(ctx, id)
	if err != nil {
		return 0, mapCatalogErr(err)
	}
	return n, nil
}

func (s *ChatService) ListCatalog(ctx context.Context, providerID string) ([]data.CatalogView, error) {
	if err := requireLogin(ctx); err != nil {
		return nil, err
	}
	st, err := s.requireCatalog()
	if err != nil {
		return nil, err
	}
	out, err := st.ListEntries(ctx, providerID)
	if err != nil {
		return nil, mapCatalogErr(err)
	}
	return out, nil
}

func (s *ChatService) CreateCatalogEntry(ctx context.Context, in data.CatalogInput) (*data.CatalogView, error) {
	if err := requireLogin(ctx); err != nil {
		return nil, err
	}
	st, err := s.requireCatalog()
	if err != nil {
		return nil, err
	}
	if in.Source == "" {
		in.Source = data.SourceManual
	}
	out, err := st.CreateEntry(ctx, in)
	if err != nil {
		return nil, mapCatalogErr(err)
	}
	return out, nil
}

func (s *ChatService) PatchCatalogEntry(ctx context.Context, patch data.EntryPatch) error {
	if err := requireLogin(ctx); err != nil {
		return err
	}
	st, err := s.requireCatalog()
	if err != nil {
		return err
	}
	return mapCatalogErr(st.PatchEntry(ctx, patch))
}

func (s *ChatService) DeleteCatalogEntry(ctx context.Context, id string) error {
	if err := requireLogin(ctx); err != nil {
		return err
	}
	st, err := s.requireCatalog()
	if err != nil {
		return err
	}
	return mapCatalogErr(st.DeleteEntry(ctx, id))
}

// SetSessionModelInput is the body for PATCH /sessions/{id}/model.
type SetSessionModelInput struct {
	Choice          string
	ModelProviderID string
	Model           string
}

func (s *ChatService) SetSessionModel(ctx context.Context, sessionID string, in SetSessionModelInput) error {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return biz.ErrSessionNotFound
	}
	if in.Choice == "agent_default" {
		return s.chatUC.SetModelOverride(ctx, sessionID, "", "")
	}
	providerID := strings.TrimSpace(in.ModelProviderID)
	modelName := strings.TrimSpace(in.Model)
	if providerID == "" || modelName == "" {
		return chat.ErrModelChoiceInvalid
	}
	if s.catalog == nil {
		return chat.ErrModelChoiceInvalid
	}
	ok, err := s.catalog.HasUsableEntry(ctx, providerID, modelName)
	if err != nil {
		return err
	}
	if !ok {
		return chat.ErrModelChoiceInvalid
	}
	return s.chatUC.SetModelOverride(ctx, sessionID, providerID, modelName)
}

// ModelChoiceItem is one row in GET model-choices.
type ModelChoiceItem struct {
	ID           string `json:"id"`
	Label        string `json:"label"`
	ProviderID   string `json:"provider_id,omitempty"`
	ProviderName string `json:"provider_name,omitempty"`
	Model        string `json:"model,omitempty"`
	DisplayName  string `json:"display_name,omitempty"`
	Kind         string `json:"kind,omitempty"`
}

// ModelChoiceSelected is the current session overlay, or nil for Agent default.
type ModelChoiceSelected struct {
	ProviderID string `json:"provider_id"`
	Model      string `json:"model"`
}

// ModelChoicesReply is GET /agents/{id}/model-choices.
type ModelChoicesReply struct {
	Items    []ModelChoiceItem    `json:"items"`
	Selected *ModelChoiceSelected `json:"selected"`
}

func (s *ChatService) ListModelChoices(ctx context.Context, agentID, sessionID string) (*ModelChoicesReply, error) {
	agentID = strings.TrimSpace(agentID)
	if agentID == "" {
		return nil, biz.ErrAgentNotFound
	}
	agentMeta, err := s.agentUC.Get(ctx, agentID)
	if err != nil {
		return nil, err
	}
	var sess *biz.ChatSession
	if sid := strings.TrimSpace(sessionID); sid != "" {
		sess, err = s.chatUC.GetSession(ctx, sid)
		if err != nil {
			return nil, err
		}
		if sess.AgentID != agentID {
			return nil, biz.ErrSessionNotFound
		}
	}
	items := []ModelChoiceItem{{
		ID:    "agent_default",
		Label: fmt.Sprintf("Agent 默认（%s/%s）", agentMeta.ModelConfig.Provider, agentMeta.ModelConfig.Model),
	}}
	if s.catalog != nil {
		usable, err := s.catalog.ListUsable(ctx)
		if err != nil {
			return nil, err
		}
		for _, u := range usable {
			items = append(items, ModelChoiceItem{
				ID:           u.ProviderID + "::" + u.Model,
				Label:        u.DisplayName,
				ProviderID:   u.ProviderID,
				ProviderName: u.ProviderName,
				Model:        u.Model,
				DisplayName:  u.DisplayName,
				Kind:         u.Kind,
			})
		}
	}
	out := &ModelChoicesReply{Items: items}
	if sess != nil && sess.ModelProviderID != "" && sess.Model != "" {
		out.Selected = &ModelChoiceSelected{ProviderID: sess.ModelProviderID, Model: sess.Model}
	}
	return out, nil
}
