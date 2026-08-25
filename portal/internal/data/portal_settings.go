package data

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"backend/internal/data/model"

	"gorm.io/gorm"
)

const portalSettingKeyCodeModel = "code_model"

// StoredCodeModel is the global code-family model persisted in portal_settings.
type StoredCodeModel struct {
	Provider string
	Model    string
	APIKey   string
	BaseURL  string
}

// GetCodeModelSetting loads the global code-family model. Missing row → empty spec.
func (d *Data) GetCodeModelSetting(ctx context.Context) (StoredCodeModel, error) {
	if d == nil || d.db == nil {
		return StoredCodeModel{}, errors.New("database not ready")
	}
	var row model.PortalSetting
	err := d.db.WithContext(ctx).Where("setting_key = ?", portalSettingKeyCodeModel).First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return StoredCodeModel{}, nil
	}
	if err != nil {
		return StoredCodeModel{}, err
	}
	return parseStoredCodeModel(row.Value), nil
}

// PutCodeModelSetting upserts the global code-family model.
func (d *Data) PutCodeModelSetting(ctx context.Context, spec StoredCodeModel) error {
	if d == nil || d.db == nil {
		return errors.New("database not ready")
	}
	body, err := json.Marshal(map[string]string{
		"provider": strings.TrimSpace(spec.Provider),
		"model":    strings.TrimSpace(spec.Model),
		"api_key":  strings.TrimSpace(spec.APIKey),
		"base_url": strings.TrimSpace(spec.BaseURL),
	})
	if err != nil {
		return err
	}
	row := model.PortalSetting{
		Key:       portalSettingKeyCodeModel,
		Value:     string(body),
		UpdatedAt: time.Now(),
	}
	return d.db.WithContext(ctx).Save(&row).Error
}

func parseStoredCodeModel(raw string) StoredCodeModel {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return StoredCodeModel{}
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		return StoredCodeModel{}
	}
	str := func(keys ...string) string {
		for _, k := range keys {
			if s, ok := m[k].(string); ok && strings.TrimSpace(s) != "" {
				return strings.TrimSpace(s)
			}
		}
		return ""
	}
	return StoredCodeModel{
		Provider: str("provider"),
		Model:    str("model"),
		APIKey:   str("api_key", "apiKey"),
		BaseURL:  str("base_url", "baseUrl"),
	}
}
