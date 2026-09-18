package data

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
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
	ID          string    `json:"id"`
	ProviderID  string    `json:"provider_id"`
	Model       string    `json:"model"`
	DisplayName string    `json:"display_name"`
	Hidden      bool      `json:"hidden"`
	Source      string    `json:"source"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type EntryPatch struct {
	ID          string
	DisplayName *string
	Hidden      *bool
}

type ProviderPatch struct {
	Name    *string
	Kind    *string
	BaseURL *string
	APIKey  *string
	Enabled *bool
}

type UsableModel struct {
	ProviderID   string `json:"provider_id"`
	ProviderName string `json:"provider_name"`
	Kind         string `json:"kind"`
	Model        string `json:"model"`
	DisplayName  string `json:"display_name"`
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
	// GORM skips zero-value fields with DB defaults on Create; wrap Create+enabled
	// correction in one transaction so a failed second step cannot leave enabled=true.
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(row).Error; err != nil {
			return err
		}
		if !in.Enabled {
			if err := tx.Model(&model.ModelProvider{}).Where("id = ?", row.ID).Update("enabled", false).Error; err != nil {
				return err
			}
			row.Enabled = false
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return providerToView(row), nil
}

func (s *ModelCatalogStore) ListProviders(ctx context.Context) ([]ProviderView, error) {
	var rows []model.ModelProvider
	if err := s.db.WithContext(ctx).Order("created_at, id").Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]ProviderView, 0, len(rows))
	for i := range rows {
		out = append(out, *providerToView(&rows[i]))
	}
	return out, nil
}

func (s *ModelCatalogStore) PatchProvider(ctx context.Context, id string, patch ProviderPatch) (*ProviderView, error) {
	id = strings.TrimSpace(id)
	var row model.ModelProvider
	if err := s.db.WithContext(ctx).Where("id = ?", id).First(&row).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if patch.Kind != nil {
		kind := strings.TrimSpace(*patch.Kind)
		if kind != KindOpenAICompat && kind != KindDashScope {
			return nil, errors.New("invalid provider kind")
		}
		row.Kind = kind
	}
	if patch.BaseURL != nil {
		row.BaseURL = *patch.BaseURL
	}
	if row.Kind == KindOpenAICompat && strings.TrimSpace(row.BaseURL) == "" {
		return nil, errors.New("openai_compat requires base_url")
	}
	updates := map[string]any{}
	if patch.Name != nil {
		updates["name"] = *patch.Name
	}
	if patch.Kind != nil {
		updates["kind"] = row.Kind
	}
	if patch.BaseURL != nil {
		updates["base_url"] = row.BaseURL
	}
	if patch.APIKey != nil {
		updates["api_key"] = *patch.APIKey
	}
	if patch.Enabled != nil {
		updates["enabled"] = *patch.Enabled
	}
	if len(updates) > 0 {
		if err := s.db.WithContext(ctx).Model(&model.ModelProvider{}).Where("id = ?", id).Updates(updates).Error; err != nil {
			return nil, err
		}
	}
	return s.GetProvider(ctx, id)
}

func (s *ModelCatalogStore) DeleteProvider(ctx context.Context, id string) error {
	id = strings.TrimSpace(id)
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("provider_id = ?", id).Delete(&model.ModelCatalogEntry{}).Error; err != nil {
			return err
		}
		res := tx.Where("id = ?", id).Delete(&model.ModelProvider{})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return ErrNotFound
		}
		return nil
	})
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

func (s *ModelCatalogStore) GetProviderSecret(ctx context.Context, id string) (kind, baseURL, apiKey string, enabled bool, err error) {
	var row model.ModelProvider
	if err := s.db.WithContext(ctx).Where("id = ?", id).First(&row).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return "", "", "", false, ErrNotFound
		}
		return "", "", "", false, err
	}
	return row.Kind, row.BaseURL, row.APIKey, row.Enabled, nil
}

func (s *ModelCatalogStore) HasUsableEntry(ctx context.Context, providerID, modelName string) (bool, error) {
	var prov model.ModelProvider
	if err := s.db.WithContext(ctx).Where("id = ?", providerID).First(&prov).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return false, nil
		}
		return false, err
	}
	if !prov.Enabled || strings.TrimSpace(prov.APIKey) == "" {
		return false, nil
	}
	var entry model.ModelCatalogEntry
	err := s.db.WithContext(ctx).Where("provider_id = ? AND model = ?", providerID, modelName).First(&entry).Error
	if err == gorm.ErrRecordNotFound {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if entry.Hidden {
		return false, nil
	}
	return true, nil
}

func (s *ModelCatalogStore) CreateEntry(ctx context.Context, in CatalogInput) (*CatalogView, error) {
	providerID := strings.TrimSpace(in.ProviderID)
	modelName := strings.TrimSpace(in.Model)
	if providerID == "" || modelName == "" {
		return nil, errors.New("provider_id and model are required")
	}
	if in.Source != SourceSync && in.Source != SourceManual {
		return nil, errors.New("invalid catalog source")
	}
	var prov model.ModelProvider
	if err := s.db.WithContext(ctx).Where("id = ?", providerID).First(&prov).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, ErrNotFound
		}
		return nil, err
	}
	display := in.DisplayName
	if display == "" {
		display = modelName
	}
	overridden := strings.TrimSpace(in.DisplayName) != "" && in.DisplayName != modelName
	row := &model.ModelCatalogEntry{
		ID:                    uuid.New().String(),
		ProviderID:            providerID,
		Model:                 modelName,
		DisplayName:           display,
		Source:                in.Source,
		DisplayNameOverridden: overridden,
	}
	if err := s.db.WithContext(ctx).Create(row).Error; err != nil {
		if isDuplicateKey(err) {
			return nil, ErrDuplicateName
		}
		return nil, err
	}
	return catalogToView(row), nil
}

func (s *ModelCatalogStore) PatchEntry(ctx context.Context, patch EntryPatch) error {
	id := strings.TrimSpace(patch.ID)
	if id == "" {
		return errors.New("id is required")
	}
	var row model.ModelCatalogEntry
	if err := s.db.WithContext(ctx).Where("id = ?", id).First(&row).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return ErrNotFound
		}
		return err
	}
	updates := map[string]any{}
	if patch.DisplayName != nil {
		updates["display_name"] = *patch.DisplayName
		updates["display_name_overridden"] = true
	}
	if patch.Hidden != nil {
		updates["hidden"] = *patch.Hidden
		updates["hidden_overridden"] = true
	}
	if len(updates) == 0 {
		return nil
	}
	return s.db.WithContext(ctx).Model(&model.ModelCatalogEntry{}).Where("id = ?", id).Updates(updates).Error
}

func (s *ModelCatalogStore) ListEntries(ctx context.Context, providerID string) ([]CatalogView, error) {
	q := s.db.WithContext(ctx).Order("model")
	if pid := strings.TrimSpace(providerID); pid != "" {
		q = q.Where("provider_id = ?", pid)
	}
	var rows []model.ModelCatalogEntry
	if err := q.Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]CatalogView, 0, len(rows))
	for i := range rows {
		out = append(out, *catalogToView(&rows[i]))
	}
	return out, nil
}

func (s *ModelCatalogStore) ListUsable(ctx context.Context) ([]UsableModel, error) {
	type row struct {
		ProviderID   string
		ProviderName string
		Kind         string
		Model        string
		DisplayName  string
	}
	var rows []row
	err := s.db.WithContext(ctx).Table("model_catalog AS e").
		Select("e.provider_id AS provider_id, p.name AS provider_name, p.kind AS kind, e.model AS model, e.display_name AS display_name").
		Joins("JOIN model_providers AS p ON p.id = e.provider_id").
		Where("p.enabled = ? AND TRIM(p.api_key) != '' AND e.hidden = ?", true, false).
		Order("p.name, e.model").
		Find(&rows).Error
	if err != nil {
		return nil, err
	}
	out := make([]UsableModel, 0, len(rows))
	for _, r := range rows {
		out = append(out, UsableModel{
			ProviderID:   r.ProviderID,
			ProviderName: r.ProviderName,
			Kind:         r.Kind,
			Model:        r.Model,
			DisplayName:  r.DisplayName,
		})
	}
	return out, nil
}

func (s *ModelCatalogStore) DeleteEntry(ctx context.Context, id string) error {
	id = strings.TrimSpace(id)
	res := s.db.WithContext(ctx).Where("id = ?", id).Delete(&model.ModelCatalogEntry{})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *ModelCatalogStore) Sync(ctx context.Context, providerID string) (int, error) {
	var prov model.ModelProvider
	if err := s.db.WithContext(ctx).Where("id = ?", providerID).First(&prov).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return 0, ErrNotFound
		}
		return 0, err
	}
	if prov.Kind != KindOpenAICompat {
		return 0, errors.New("sync is only supported for openai_compat providers")
	}
	ids, err := fetchOpenAICompatModels(ctx, prov.BaseURL, prov.APIKey)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if err := s.upsertSyncedEntry(ctx, providerID, id); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}

func (s *ModelCatalogStore) upsertSyncedEntry(ctx context.Context, providerID, modelName string) error {
	var existing model.ModelCatalogEntry
	err := s.db.WithContext(ctx).Where("provider_id = ? AND model = ?", providerID, modelName).First(&existing).Error
	if err == gorm.ErrRecordNotFound {
		row := &model.ModelCatalogEntry{
			ID:          uuid.New().String(),
			ProviderID:  providerID,
			Model:       modelName,
			DisplayName: modelName,
			Source:      SourceSync,
		}
		return s.db.WithContext(ctx).Create(row).Error
	}
	if err != nil {
		return err
	}
	updates := map[string]any{}
	if !existing.DisplayNameOverridden {
		updates["display_name"] = modelName
	}
	if !existing.HiddenOverridden {
		updates["hidden"] = false
	}
	if len(updates) == 0 {
		return nil
	}
	return s.db.WithContext(ctx).Model(&model.ModelCatalogEntry{}).Where("id = ?", existing.ID).Updates(updates).Error
}

func fetchOpenAICompatModels(ctx context.Context, baseURL, apiKey string) ([]string, error) {
	url := strings.TrimRight(baseURL, "/") + "/models"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("models list HTTP %d", resp.StatusCode)
	}
	var body struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(body.Data))
	for _, d := range body.Data {
		ids = append(ids, d.ID)
	}
	return ids, nil
}

func providerToView(row *model.ModelProvider) *ProviderView {
	return &ProviderView{
		ID:        row.ID,
		Name:      row.Name,
		Kind:      row.Kind,
		BaseURL:   row.BaseURL,
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
