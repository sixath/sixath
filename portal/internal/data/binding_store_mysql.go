package data

import (
	"backend/internal/data/model"

	"github.com/sixath/framework/memory/hub"
	"github.com/sixath/framework/memory/hub/local"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// MySQLBindingStore persists agent_asset_bindings via GORM.
type MySQLBindingStore struct {
	db *gorm.DB
}

func NewMySQLBindingStore(db *gorm.DB) *MySQLBindingStore {
	return &MySQLBindingStore{db: db}
}

var _ local.BindingStore = (*MySQLBindingStore)(nil)

func (s *MySQLBindingStore) ListByAgent(agentID string) ([]local.Binding, error) {
	var rows []model.AgentAssetBinding
	if err := s.db.Where("agent_id = ?", agentID).Order("priority ASC, asset_kind ASC, asset_id ASC").Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]local.Binding, len(rows))
	for i, r := range rows {
		out[i] = rowToBinding(r)
	}
	return out, nil
}

func (s *MySQLBindingStore) Upsert(b local.Binding) error {
	row := bindingToRow(b)
	return s.db.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "agent_id"}, {Name: "asset_kind"}, {Name: "asset_id"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"hub", "name", "status", "priority", "owner_id", "visibility", "metadata", "updated_at",
		}),
	}).Create(&row).Error
}

func (s *MySQLBindingStore) Delete(agentID string, kind hub.AssetKind, assetID string) error {
	return s.db.Where("agent_id = ? AND asset_kind = ? AND asset_id = ?", agentID, string(kind), assetID).
		Delete(&model.AgentAssetBinding{}).Error
}

func (s *MySQLBindingStore) Get(kind hub.AssetKind, assetID string) (local.Binding, bool, error) {
	var row model.AgentAssetBinding
	err := s.db.Where("asset_kind = ? AND asset_id = ?", string(kind), assetID).First(&row).Error
	if err == gorm.ErrRecordNotFound {
		return local.Binding{}, false, nil
	}
	if err != nil {
		return local.Binding{}, false, err
	}
	return rowToBinding(row), true, nil
}

func (s *MySQLBindingStore) UpdateMeta(kind hub.AssetKind, assetID string, vis *hub.Visibility, st *hub.AssetStatus) error {
	updates := map[string]any{}
	if vis != nil {
		updates["visibility"] = string(*vis)
	}
	if st != nil {
		updates["status"] = string(*st)
	}
	if len(updates) == 0 {
		return nil
	}
	res := s.db.Model(&model.AgentAssetBinding{}).
		Where("asset_kind = ? AND asset_id = ?", string(kind), assetID).
		Updates(updates)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return hub.ErrNotSupported
	}
	return nil
}

func rowToBinding(r model.AgentAssetBinding) local.Binding {
	return local.Binding{
		AgentID:    r.AgentID,
		AssetKind:  hub.AssetKind(r.AssetKind),
		AssetID:    r.AssetID,
		Priority:   r.Priority,
		Hub:        r.Hub,
		Name:       r.Name,
		Status:     hub.AssetStatus(r.Status),
		OwnerID:    r.OwnerID,
		Visibility: hub.Visibility(r.Visibility),
		Meta:       map[string]any(r.Metadata),
	}
}

func bindingToRow(b local.Binding) model.AgentAssetBinding {
	hubName := b.Hub
	if hubName == "" {
		hubName = "local"
	}
	st := string(b.Status)
	if st == "" {
		st = string(hub.AssetActive)
	}
	return model.AgentAssetBinding{
		AgentID:    b.AgentID,
		AssetKind:  string(b.AssetKind),
		AssetID:    b.AssetID,
		Hub:        hubName,
		Name:       b.Name,
		Status:     st,
		Priority:   b.Priority,
		OwnerID:    b.OwnerID,
		Visibility: string(b.Visibility),
		Metadata:   model.BindingMetadata(b.Meta),
	}
}
