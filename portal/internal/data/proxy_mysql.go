package data

import (
	"context"
	"strings"
	"time"

	"backend/internal/biz"
	"backend/internal/data/model"

	"github.com/go-kratos/kratos/v2/log"
	"gorm.io/gorm"
)

var _ biz.ProxyRepo = (*proxyRepo)(nil)

type proxyRepo struct {
	db  *gorm.DB
	log *log.Helper
}

// NewProxyRepo creates a new ProxyRepo (MySQL/GORM impl).
func NewProxyRepo(data *Data, logger log.Logger) biz.ProxyRepo {
	if data == nil || data.db == nil {
		panic("NewProxyRepo: Data.db is nil, database config required")
	}
	return &proxyRepo{db: data.db, log: log.NewHelper(logger)}
}

func toProxyMeta(m *model.Proxy) *biz.ProxyMeta {
	if m == nil {
		return nil
	}
	meta := &biz.ProxyMeta{
		ID:          m.ID,
		Name:        m.Name,
		Description: m.Description,
		Type:        m.Type,
		Host:        m.Host,
		Port:        m.Port,
		User:        m.User,
		Password:    m.Password,
		HasPassword: m.Password != "",
		CreatedAt:   m.CreatedAt,
		UpdatedAt:   m.UpdatedAt,
	}
	if m.NoProxy != nil {
		meta.NoProxy = append([]string(nil), m.NoProxy...)
	}
	return meta
}

func (r *proxyRepo) Create(ctx context.Context, meta *biz.ProxyMeta) (*biz.ProxyMeta, error) {
	now := time.Now()
	m := &model.Proxy{
		ID:          meta.ID,
		Name:        meta.Name,
		Description: meta.Description,
		Type:        meta.Type,
		Host:        meta.Host,
		Port:        meta.Port,
		User:        meta.User,
		Password:    meta.Password,
		NoProxy:     model.ProxyNoProxy(meta.NoProxy),
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := r.db.WithContext(ctx).Create(m).Error; err != nil {
		if isDuplicateKey(err) {
			return nil, ErrDuplicateName
		}
		return nil, err
	}
	return toProxyMeta(m), nil
}

func (r *proxyRepo) GetByID(ctx context.Context, id string) (*biz.ProxyMeta, error) {
	var m model.Proxy
	if err := r.db.WithContext(ctx).Where("id = ?", id).First(&m).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return toProxyMeta(&m), nil
}

func (r *proxyRepo) List(ctx context.Context, opts biz.ListOptions) ([]*biz.ProxyMeta, int, error) {
	if opts.IDs != nil && len(opts.IDs) == 0 {
		return []*biz.ProxyMeta{}, 0, nil
	}
	var total int64
	q := r.db.WithContext(ctx).Model(&model.Proxy{})
	if len(opts.IDs) > 0 {
		q = q.Where("id IN ?", opts.IDs)
	}
	if opts.Name != "" {
		for _, tok := range strings.Fields(opts.Name) {
			if tok == "" {
				continue
			}
			pattern := "%" + tok + "%"
			q = q.Where("(name LIKE ? OR description LIKE ? OR id LIKE ?)", pattern, pattern, pattern)
		}
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

	var rows []model.Proxy
	if err := q.Order("created_at DESC").Offset(offset).Limit(int(pageSize)).Find(&rows).Error; err != nil {
		return nil, 0, err
	}

	items := make([]*biz.ProxyMeta, len(rows))
	for i := range rows {
		items[i] = toProxyMeta(&rows[i])
	}
	return items, int(total), nil
}

func (r *proxyRepo) Update(ctx context.Context, meta *biz.ProxyMeta) (*biz.ProxyMeta, error) {
	var m model.Proxy
	if err := r.db.WithContext(ctx).Where("id = ?", meta.ID).First(&m).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, ErrNotFound
		}
		return nil, err
	}

	m.Name = meta.Name
	m.Description = meta.Description
	m.Type = meta.Type
	m.Host = meta.Host
	m.Port = meta.Port
	m.User = meta.User
	m.Password = meta.Password
	m.NoProxy = model.ProxyNoProxy(meta.NoProxy)
	m.UpdatedAt = time.Now()

	if err := r.db.WithContext(ctx).Save(&m).Error; err != nil {
		if isDuplicateKey(err) {
			return nil, ErrDuplicateName
		}
		return nil, err
	}
	return toProxyMeta(&m), nil
}

func (r *proxyRepo) Delete(ctx context.Context, id string) error {
	res := r.db.WithContext(ctx).Where("id = ?", id).Delete(&model.Proxy{})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *proxyRepo) ListReferences(ctx context.Context, proxyID string, limit int) ([]biz.ProxyReference, bool, error) {
	if limit <= 0 {
		limit = 20
	}
	capN := limit + 1
	refs := make([]biz.ProxyReference, 0, capN)

	var agents []struct {
		ID   string
		Name string
	}
	if err := r.db.WithContext(ctx).Model(&model.Agent{}).
		Select("id", "name").
		Where("proxy_id = ?", proxyID).
		Limit(capN).
		Find(&agents).Error; err != nil {
		return nil, false, err
	}
	for _, a := range agents {
		refs = append(refs, biz.ProxyReference{Kind: "agent", ID: a.ID, Name: a.Name})
	}

	if len(refs) < capN {
		remain := capN - len(refs)
		var tools []struct {
			ID   string
			Name string
		}
		// MySQL JSON paths: top-level proxy_id and nested egress.proxy_id.
		if err := r.db.WithContext(ctx).Model(&model.Tool{}).
			Select("id", "name").
			Where(
				"JSON_UNQUOTE(JSON_EXTRACT(config, '$.proxy_id')) = ? OR JSON_UNQUOTE(JSON_EXTRACT(config, '$.egress.proxy_id')) = ?",
				proxyID, proxyID,
			).
			Limit(remain).
			Find(&tools).Error; err != nil {
			return nil, false, err
		}
		for _, tl := range tools {
			refs = append(refs, biz.ProxyReference{Kind: "tool", ID: tl.ID, Name: tl.Name})
		}
	}

	truncated := len(refs) > limit
	if truncated {
		refs = refs[:limit]
	}
	return refs, truncated, nil
}
