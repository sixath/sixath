package data

import (
	"context"

	"backend/internal/biz"
	"backend/internal/data/model"

	"github.com/go-kratos/kratos/v2/log"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var _ biz.ResourceRepo = (*resourceRepo)(nil)

type resourceRepo struct {
	db  *gorm.DB
	log *log.Helper
}

// NewResourceRepo creates a MySQL/GORM resource repository.
func NewResourceRepo(data *Data, logger log.Logger) biz.ResourceRepo {
	if data == nil || data.db == nil {
		panic("NewResourceRepo: Data.db is nil, database config required")
	}
	return &resourceRepo{db: data.db, log: log.NewHelper(logger)}
}

func resourceModelToBiz(m *model.Resource) *biz.Resource {
	return &biz.Resource{
		ID:           m.ID,
		Type:         biz.ResourceType(m.Type),
		Name:         m.Name,
		OwnerUserID:  m.OwnerUserID,
		Visibility:   biz.Visibility(m.Visibility),
		HomeOrgID:    m.HomeOrgID,
		BoundAgentID: m.BoundAgentID,
		PayloadRef:   m.PayloadRef,
	}
}

func resourceGrantModelToBiz(m model.ResourceGrant) biz.ResourceGrant {
	return biz.ResourceGrant{
		ResourceID:  m.ResourceID,
		GranteeType: m.GranteeType,
		GranteeID:   m.GranteeID,
		Perm:        biz.Perm(m.Perm),
	}
}

func (r *resourceRepo) CreateResource(ctx context.Context, resource *biz.Resource) (*biz.Resource, error) {
	m := &model.Resource{
		ID:           resource.ID,
		Type:         string(resource.Type),
		Name:         resource.Name,
		OwnerUserID:  resource.OwnerUserID,
		Visibility:   string(resource.Visibility),
		HomeOrgID:    resource.HomeOrgID,
		BoundAgentID: resource.BoundAgentID,
		PayloadRef:   resource.PayloadRef,
	}
	if m.ID == "" {
		m.ID = uuid.NewString()
	}
	if err := r.db.WithContext(ctx).Create(m).Error; err != nil {
		return nil, err
	}
	return resourceModelToBiz(m), nil
}

func (r *resourceRepo) UpdateResource(ctx context.Context, resource *biz.Resource) error {
	res := r.db.WithContext(ctx).Model(&model.Resource{}).Where("id = ?", resource.ID).Updates(map[string]any{
		"name":           resource.Name,
		"owner_user_id":  resource.OwnerUserID,
		"visibility":     string(resource.Visibility),
		"home_org_id":    resource.HomeOrgID,
		"bound_agent_id": resource.BoundAgentID,
	})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *resourceRepo) DeleteResource(ctx context.Context, id string) error {
	res := r.db.WithContext(ctx).Delete(&model.Resource{}, "id = ?", id)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *resourceRepo) GetResource(ctx context.Context, id string) (*biz.Resource, error) {
	var m model.Resource
	if err := r.db.WithContext(ctx).Where("id = ?", id).First(&m).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return resourceModelToBiz(&m), nil
}

func (r *resourceRepo) GetByPayload(ctx context.Context, resourceType biz.ResourceType, payloadRef string) (*biz.Resource, error) {
	var m model.Resource
	if err := r.db.WithContext(ctx).
		Where("type = ? AND payload_ref = ?", string(resourceType), payloadRef).
		First(&m).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return resourceModelToBiz(&m), nil
}

func (r *resourceRepo) ListAllByType(ctx context.Context, resourceType biz.ResourceType) ([]*biz.Resource, error) {
	var rows []model.Resource
	if err := r.db.WithContext(ctx).
		Where("type = ?", string(resourceType)).
		Order("created_at DESC").
		Find(&rows).Error; err != nil {
		return nil, err
	}
	resources := make([]*biz.Resource, len(rows))
	for i := range rows {
		resources[i] = resourceModelToBiz(&rows[i])
	}
	return resources, nil
}

func (r *resourceRepo) CreateGrant(ctx context.Context, grant biz.ResourceGrant) error {
	m := &model.ResourceGrant{
		ResourceID:  grant.ResourceID,
		GranteeType: grant.GranteeType,
		GranteeID:   grant.GranteeID,
		Perm:        string(grant.Perm),
	}
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{
			{Name: "resource_id"},
			{Name: "grantee_type"},
			{Name: "grantee_id"},
		},
		DoUpdates: clause.AssignmentColumns([]string{"perm"}),
	}).Create(m).Error
}

func (r *resourceRepo) ListGrants(ctx context.Context, resourceID string) ([]biz.ResourceGrant, error) {
	byID, err := r.ListGrantsByResourceIDs(ctx, []string{resourceID})
	if err != nil {
		return nil, err
	}
	return byID[resourceID], nil
}

func (r *resourceRepo) ListGrantsByResourceIDs(ctx context.Context, resourceIDs []string) (map[string][]biz.ResourceGrant, error) {
	out := make(map[string][]biz.ResourceGrant, len(resourceIDs))
	if len(resourceIDs) == 0 {
		return out, nil
	}
	var rows []model.ResourceGrant
	if err := r.db.WithContext(ctx).
		Where("resource_id IN ?", resourceIDs).
		Order("resource_id ASC, grantee_type ASC, grantee_id ASC").
		Find(&rows).Error; err != nil {
		return nil, err
	}
	for _, row := range rows {
		out[row.ResourceID] = append(out[row.ResourceID], resourceGrantModelToBiz(row))
	}
	return out, nil
}

func (r *resourceRepo) UserOrgIDs(ctx context.Context, userID string) ([]string, error) {
	var orgIDs []string
	err := r.db.WithContext(ctx).Model(&model.OrgMember{}).
		Where("user_id = ?", userID).
		Order("org_id ASC").
		Pluck("org_id", &orgIDs).Error
	return orgIDs, err
}
