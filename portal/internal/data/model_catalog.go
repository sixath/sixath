package data

import (
	"context"
	"errors"
	"strings"
	"time"

	"backend/internal/data/model"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

const (
	KindOpenAICompat = model.KindOpenAICompat
	KindDashScope    = model.KindDashScope
	SourceSync       = model.SourceSync
	SourceManual     = model.SourceManual
)

type ModelCatalogStore struct {
	db *gorm.DB
}

func NewModelCatalogStore(db *gorm.DB) *ModelCatalogStore {
	return &ModelCatalogStore{db: db}
}

type ProviderInput struct {
	Name    string
	Kind    string
	BaseURL string
	APIKey  string
	Enabled bool
}

type ProviderView struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Kind      string    `json:"kind"`
	BaseURL   string    `json:"base_url"`
	APIKey    string    `json:"-"`
	HasAPIKey bool      `json:"has_api_key"`
	Enabled   bool      `json:"enabled"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type CatalogInput struct {
	ProviderID  string
	Model       string
	DisplayName string
	Source      string
}

type CatalogView struct {
	ID          string
	ProviderID  string
	Model       string
	DisplayName string
	Hidden      bool
	Source      string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

func (s *ModelCatalogStore) CreateProvider(ctx context.Context, in ProviderInput) (*ProviderView, error) {
	if in.Kind != KindOpenAICompat && in.Kind != KindDashScope {
		return nil, errors.New("invalid provider kind")
	}
	if in.Kind == KindOpenAICompat && strings.TrimSpace(in.BaseURL) == "" {
		return nil, errors.New("openai_compat requires base_url")
	}
	row := &model.ModelProvider{
		ID:      uuid.New().String(),
		Name:    in.Name,
		Kind:    in.Kind,
		BaseURL: in.BaseURL,
		APIKey:  in.APIKey,
		Enabled: in.Enabled,
	}
	if err := s.db.WithContext(ctx).Create(row).Error; err != nil {
		return nil, err
	}
	return providerToView(row), nil
}

func (s *ModelCatalogStore) GetProvider(ctx context.Context, id string) (*ProviderView, error) {
	var row model.ModelProvider
	if err := s.db.WithContext(ctx).Where("id = ?", id).First(&row).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return providerToView(&row), nil
}

func (s *ModelCatalogStore) MustAPIKey(ctx context.Context, id string) (string, error) {
	var row model.ModelProvider
	if err := s.db.WithContext(ctx).Where("id = ?", id).First(&row).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return "", ErrNotFound
		}
		return "", err
	}
	return row.APIKey, nil
}

func (s *ModelCatalogStore) CreateEntry(ctx context.Context, in CatalogInput) (*CatalogView, error) {
	display := in.DisplayName
	if display == "" {
		display = in.Model
	}
	row := &model.ModelCatalogEntry{
		ID:          uuid.New().String(),
		ProviderID:  in.ProviderID,
		Model:       in.Model,
		DisplayName: display,
		Source:      in.Source,
	}
	if err := s.db.WithContext(ctx).Create(row).Error; err != nil {
		return nil, err
	}
	return catalogToView(row), nil
}

func providerToView(row *model.ModelProvider) *ProviderView {
	return &ProviderView{
		ID:        row.ID,
		Name:      row.Name,
		Kind:      row.Kind,
		BaseURL:   row.BaseURL,
		APIKey:    "",
		HasAPIKey: row.APIKey != "",
		Enabled:   row.Enabled,
		CreatedAt: row.CreatedAt,
		UpdatedAt: row.UpdatedAt,
	}
}

func catalogToView(row *model.ModelCatalogEntry) *CatalogView {
	return &CatalogView{
		ID:          row.ID,
		ProviderID:  row.ProviderID,
		Model:       row.Model,
		DisplayName: row.DisplayName,
		Hidden:      row.Hidden,
		Source:      row.Source,
		CreatedAt:   row.CreatedAt,
		UpdatedAt:   row.UpdatedAt,
	}
}
