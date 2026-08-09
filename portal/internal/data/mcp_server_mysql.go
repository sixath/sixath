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

var _ biz.McpServerRepo = (*mcpServerRepo)(nil)

type mcpServerRepo struct {
	db  *gorm.DB
	log *log.Helper
}

// NewMcpServerRepo creates a new McpServerRepo (MySQL/GORM impl).
func NewMcpServerRepo(data *Data, logger log.Logger) biz.McpServerRepo {
	if data == nil || data.db == nil {
		panic("NewMcpServerRepo: Data.db is nil, database config required")
	}
	return &mcpServerRepo{db: data.db, log: log.NewHelper(logger)}
}

func toMcpServerMeta(m *model.McpServer) *biz.McpServerMeta {
	if m == nil {
		return nil
	}
	meta := &biz.McpServerMeta{
		ID:          m.ID,
		Name:        m.Name,
		Description: m.Description,
		Transport:   m.Transport,
		Endpoint:    m.Endpoint,
		Backend:     m.Backend,
		Command:     m.Command,
		TimeoutSec:  m.TimeoutSec,
		CreatedAt:   m.CreatedAt,
		UpdatedAt:   m.UpdatedAt,
	}
	if m.ArgsJSON != nil {
		meta.Args = append([]string(nil), m.ArgsJSON...)
	}
	if m.EnvJSON != nil {
		meta.Env = make(map[string]string, len(m.EnvJSON))
		for k, v := range m.EnvJSON {
			meta.Env[k] = v
		}
	}
	return meta
}

func (r *mcpServerRepo) Create(ctx context.Context, meta *biz.McpServerMeta) (*biz.McpServerMeta, error) {
	now := time.Now()
	m := &model.McpServer{
		ID:          meta.ID,
		Name:        meta.Name,
		Description: meta.Description,
		Transport:   meta.Transport,
		Endpoint:    meta.Endpoint,
		Backend:     meta.Backend,
		Command:     meta.Command,
		ArgsJSON:    model.McpServerArgs(meta.Args),
		EnvJSON:     model.McpServerEnv(meta.Env),
		TimeoutSec:  meta.TimeoutSec,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if m.TimeoutSec == 0 {
		m.TimeoutSec = 60
	}
	if err := r.db.WithContext(ctx).Create(m).Error; err != nil {
		if isDuplicateKey(err) {
			return nil, ErrDuplicateName
		}
		return nil, err
	}
	return toMcpServerMeta(m), nil
}

func (r *mcpServerRepo) GetByID(ctx context.Context, id string) (*biz.McpServerMeta, error) {
	var m model.McpServer
	if err := r.db.WithContext(ctx).Where("id = ?", id).First(&m).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return toMcpServerMeta(&m), nil
}

func (r *mcpServerRepo) List(ctx context.Context, opts biz.ListOptions) ([]*biz.McpServerMeta, int, error) {
	if opts.IDs != nil && len(opts.IDs) == 0 {
		return []*biz.McpServerMeta{}, 0, nil
	}
	var total int64
	q := r.db.WithContext(ctx).Model(&model.McpServer{})
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

	var rows []model.McpServer
	if err := q.Order("created_at DESC").Offset(offset).Limit(int(pageSize)).Find(&rows).Error; err != nil {
		return nil, 0, err
	}

	items := make([]*biz.McpServerMeta, len(rows))
	for i := range rows {
		items[i] = toMcpServerMeta(&rows[i])
	}
	return items, int(total), nil
}

func (r *mcpServerRepo) Update(ctx context.Context, meta *biz.McpServerMeta) (*biz.McpServerMeta, error) {
	var m model.McpServer
	if err := r.db.WithContext(ctx).Where("id = ?", meta.ID).First(&m).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, ErrNotFound
		}
		return nil, err
	}

	m.Name = meta.Name
	m.Description = meta.Description
	m.Transport = meta.Transport
	m.Endpoint = meta.Endpoint
	m.Backend = meta.Backend
	m.Command = meta.Command
	m.ArgsJSON = model.McpServerArgs(meta.Args)
	m.EnvJSON = model.McpServerEnv(meta.Env)
	m.TimeoutSec = meta.TimeoutSec
	m.UpdatedAt = time.Now()

	if err := r.db.WithContext(ctx).Save(&m).Error; err != nil {
		if isDuplicateKey(err) {
			return nil, ErrDuplicateName
		}
		return nil, err
	}
	return toMcpServerMeta(&m), nil
}

func (r *mcpServerRepo) Delete(ctx context.Context, id string) error {
	res := r.db.WithContext(ctx).Where("id = ?", id).Delete(&model.McpServer{})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *mcpServerRepo) ListByAgent(ctx context.Context, agentID string) ([]*biz.McpServerMeta, error) {
	var rows []model.McpServer
	err := r.db.WithContext(ctx).
		Table("mcp_servers").
		Joins("JOIN agent_mcp_servers ON mcp_servers.id = agent_mcp_servers.server_id").
		Where("agent_mcp_servers.agent_id = ?", agentID).
		Order("agent_mcp_servers.sort_order ASC, agent_mcp_servers.created_at ASC").
		Find(&rows).Error
	if err != nil {
		return nil, err
	}
	items := make([]*biz.McpServerMeta, len(rows))
	for i := range rows {
		items[i] = toMcpServerMeta(&rows[i])
	}
	return items, nil
}

// BindServers replaces the agent's MCP server bindings (full set, sorted by input order).
func (r *mcpServerRepo) BindServers(ctx context.Context, agentID string, serverIDs []string) error {
	var m model.Agent
	if err := r.db.WithContext(ctx).Where("id = ?", agentID).First(&m).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return ErrNotFound
		}
		return err
	}

	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("agent_id = ?", agentID).Delete(&model.AgentMcpServer{}).Error; err != nil {
			return err
		}
		now := time.Now()
		for i, sid := range serverIDs {
			row := &model.AgentMcpServer{
				AgentID:   agentID,
				ServerID:  sid,
				SortOrder: i,
				CreatedAt: now,
			}
			if err := tx.Create(row).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

// UnbindServers removes specific MCP server bindings from an agent.
func (r *mcpServerRepo) UnbindServers(ctx context.Context, agentID string, serverIDs []string) error {
	if len(serverIDs) == 0 {
		return nil
	}
	var m model.Agent
	if err := r.db.WithContext(ctx).Where("id = ?", agentID).First(&m).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return ErrNotFound
		}
		return err
	}
	return r.db.WithContext(ctx).
		Where("agent_id = ? AND server_id IN ?", agentID, serverIDs).
		Delete(&model.AgentMcpServer{}).Error
}
