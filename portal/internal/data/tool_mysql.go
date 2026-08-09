package data

import (
	"context"
	"strings"

	"backend/internal/biz"
	"backend/internal/data/model"

	"github.com/go-kratos/kratos/v2/log"
	"github.com/google/uuid"
	"google.golang.org/protobuf/types/known/structpb"
	"gorm.io/gorm"
)

var _ biz.ToolRepo = (*toolRepo)(nil)

type toolRepo struct {
	db  *gorm.DB
	log *log.Helper
}

// NewToolRepo creates a new ToolRepo (MySQL/GORM impl)
func NewToolRepo(data *Data, logger log.Logger) biz.ToolRepo {
	if data == nil || data.db == nil {
		panic("NewToolRepo: Data.db is nil, database config required")
	}
	return &toolRepo{db: data.db, log: log.NewHelper(logger)}
}

func configToStruct(c model.ToolConfig) *structpb.Struct {
	if c == nil {
		return &structpb.Struct{Fields: make(map[string]*structpb.Value)}
	}
	fields := make(map[string]*structpb.Value, len(c))
	for k, raw := range c {
		if raw == nil {
			// JSON 中的 null 对 structpb.NewValue 不合法，直接跳过即可。
			continue
		}
		v, err := structpb.NewValue(raw)
		if err != nil {
			// 忽略异常字段，避免单个坏值导致整份配置反序列化失败。
			continue
		}
		fields[k] = v
	}
	return &structpb.Struct{Fields: fields}
}

func structToConfig(s *structpb.Struct) model.ToolConfig {
	if s == nil || s.Fields == nil {
		return make(model.ToolConfig)
	}
	m := make(map[string]interface{})
	for k, v := range s.Fields {
		m[k] = v.AsInterface()
	}
	return model.ToolConfig(m)
}

func (r *toolRepo) Create(ctx context.Context, name, description string, toolType biz.ToolType, config *structpb.Struct) (*biz.ToolMeta, error) {
	m := &model.Tool{
		ID:          uuid.New().String(),
		Name:        name,
		Description: description,
		Type:        string(toolType),
		Config:      structToConfig(config),
	}
	if err := r.db.WithContext(ctx).Create(m).Error; err != nil {
		if isDuplicateKey(err) {
			return nil, ErrDuplicateName
		}
		return nil, err
	}
	return &biz.ToolMeta{
		ID:          m.ID,
		Name:        m.Name,
		Description: m.Description,
		Type:        biz.ToolType(m.Type),
		Config:      configToStruct(m.Config),
		CreatedAt:   m.CreatedAt,
		UpdatedAt:   m.UpdatedAt,
	}, nil
}

func (r *toolRepo) GetByID(ctx context.Context, id string) (*biz.ToolMeta, error) {
	var m model.Tool
	if err := r.db.WithContext(ctx).Where("id = ?", id).First(&m).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &biz.ToolMeta{
		ID:          m.ID,
		Name:        m.Name,
		Description: m.Description,
		Type:        biz.ToolType(m.Type),
		Config:      configToStruct(m.Config),
		CreatedAt:   m.CreatedAt,
		UpdatedAt:   m.UpdatedAt,
	}, nil
}

func (r *toolRepo) GetByName(ctx context.Context, name string) (*biz.ToolMeta, error) {
	var m model.Tool
	if err := r.db.WithContext(ctx).Where("name = ?", name).First(&m).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &biz.ToolMeta{
		ID:          m.ID,
		Name:        m.Name,
		Description: m.Description,
		Type:        biz.ToolType(m.Type),
		Config:      configToStruct(m.Config),
		CreatedAt:   m.CreatedAt,
		UpdatedAt:   m.UpdatedAt,
	}, nil
}

func (r *toolRepo) List(ctx context.Context, opts biz.ListOptions) ([]*biz.ToolMeta, int, error) {
	if opts.IDs != nil && len(opts.IDs) == 0 {
		return []*biz.ToolMeta{}, 0, nil
	}
	var total int64
	q := r.db.WithContext(ctx).Model(&model.Tool{})
	if len(opts.IDs) > 0 {
		q = q.Where("id IN ?", opts.IDs)
	}
	if opts.Name != "" {
		for _, tok := range strings.Fields(opts.Name) {
			if tok == "" {
				continue
			}
			pattern := "%" + tok + "%"
			q = q.Where("(name LIKE ? OR description LIKE ? OR type LIKE ?)", pattern, pattern, pattern)
		}
	}
	if opts.Type != "" {
		q = q.Where("type = ?", opts.Type)
	}
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	page := opts.Page
	if page < 1 {
		page = 1
	}
	pageSize := opts.PageSize
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}
	offset := int((page - 1) * pageSize)

	var rows []model.Tool
	if err := q.Order("created_at DESC").Offset(offset).Limit(int(pageSize)).Find(&rows).Error; err != nil {
		return nil, 0, err
	}

	items := make([]*biz.ToolMeta, len(rows))
	for i, m := range rows {
		items[i] = &biz.ToolMeta{
			ID:          m.ID,
			Name:        m.Name,
			Description: m.Description,
			Type:        biz.ToolType(m.Type),
			Config:      configToStruct(m.Config),
			CreatedAt:   m.CreatedAt,
			UpdatedAt:   m.UpdatedAt,
		}
	}
	return items, int(total), nil
}

func (r *toolRepo) Update(ctx context.Context, id string, updates map[string]any) (*biz.ToolMeta, error) {
	var m model.Tool
	if err := r.db.WithContext(ctx).Where("id = ?", id).First(&m).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, ErrNotFound
		}
		return nil, err
	}

	upd := make(map[string]interface{})
	for k, v := range updates {
		switch k {
		case "name":
			upd["name"] = v.(string)
		case "description":
			upd["description"] = v.(string)
		case "type":
			upd["type"] = v.(string)
		case "config":
			if s, ok := v.(*structpb.Struct); ok {
				cfg := structToConfig(s)
				// 单独更新 config 列，避免 GORM Updates 对 map/JSON 类型的处理问题
				if err := r.db.WithContext(ctx).Model(&m).Update("config", cfg).Error; err != nil {
					if isDuplicateKey(err) {
						return nil, ErrDuplicateName
					}
					return nil, err
				}
			}
		}
	}
	// 更新 name、description（config 已单独处理）
	if len(upd) > 0 {
		if err := r.db.WithContext(ctx).Model(&m).Updates(upd).Error; err != nil {
			if isDuplicateKey(err) {
				return nil, ErrDuplicateName
			}
			return nil, err
		}
	}

	// reload
	if err := r.db.WithContext(ctx).Where("id = ?", id).First(&m).Error; err != nil {
		return nil, err
	}
	return &biz.ToolMeta{
		ID:          m.ID,
		Name:        m.Name,
		Description: m.Description,
		Type:        biz.ToolType(m.Type),
		Config:      configToStruct(m.Config),
		CreatedAt:   m.CreatedAt,
		UpdatedAt:   m.UpdatedAt,
	}, nil
}

func (r *toolRepo) Delete(ctx context.Context, id string) error {
	res := r.db.WithContext(ctx).Where("id = ?", id).Delete(&model.Tool{})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *toolRepo) ListByAgent(ctx context.Context, agentID string) ([]*biz.ToolMeta, error) {
	var rows []model.Tool
	err := r.db.WithContext(ctx).
		Table("tools").
		Joins("JOIN agent_tools ON tools.id = agent_tools.tool_id").
		Where("agent_tools.agent_id = ?", agentID).
		Order("agent_tools.sort_order ASC, agent_tools.created_at ASC").
		Find(&rows).Error
	if err != nil {
		return nil, err
	}

	items := make([]*biz.ToolMeta, len(rows))
	for i, m := range rows {
		items[i] = &biz.ToolMeta{
			ID:          m.ID,
			Name:        m.Name,
			Description: m.Description,
			Type:        biz.ToolType(m.Type),
			Config:      configToStruct(m.Config),
			CreatedAt:   m.CreatedAt,
			UpdatedAt:   m.UpdatedAt,
		}
	}
	return items, nil
}

func (r *toolRepo) IsBoundToAgent(ctx context.Context, toolID string) (bool, error) {
	var count int64
	err := r.db.WithContext(ctx).Model(&model.AgentTool{}).Where("tool_id = ?", toolID).Count(&count).Error
	return count > 0, err
}

func isDuplicateKey(err error) bool {
	if err == nil {
		return false
	}
	// MySQL duplicate key: Error 1062 (23000): Duplicate entry
	return strings.Contains(err.Error(), "1062") || strings.Contains(err.Error(), "Duplicate entry")
}
