package service

import (
	"context"
	"time"

	"backend/internal/biz"

	"github.com/go-kratos/kratos/v2/log"
)

// McpServerService exposes MCP server CRUD, test, and agent bind helpers.
type McpServerService struct {
	uc  *biz.McpServerUsecase
	log *log.Helper
}

// NewMcpServerService creates a McpServerService.
func NewMcpServerService(uc *biz.McpServerUsecase, logger log.Logger) *McpServerService {
	return &McpServerService{uc: uc, log: log.NewHelper(logger)}
}

// McpServerDTO is the JSON shape for MCP server APIs.
type McpServerDTO struct {
	ID          string            `json:"id"`
	Name        string            `json:"name"`
	Description string            `json:"description"`
	Transport   string            `json:"transport"`
	Endpoint    string            `json:"endpoint,omitempty"`
	Backend     string            `json:"backend,omitempty"`
	Command     string            `json:"command,omitempty"`
	Args        []string          `json:"args,omitempty"`
	Env         map[string]string `json:"env,omitempty"`
	TimeoutSec  int               `json:"timeout_sec,omitempty"`
	CreatedAt   string            `json:"created_at,omitempty"`
	UpdatedAt   string            `json:"updated_at,omitempty"`
}

// McpServerDTOFromMeta maps biz meta to API DTO.
func McpServerDTOFromMeta(m *biz.McpServerMeta) McpServerDTO {
	if m == nil {
		return McpServerDTO{}
	}
	return McpServerDTO{
		ID:          m.ID,
		Name:        m.Name,
		Description: m.Description,
		Transport:   m.Transport,
		Endpoint:    m.Endpoint,
		Backend:     m.Backend,
		Command:     m.Command,
		Args:        m.Args,
		Env:         m.Env,
		TimeoutSec:  m.TimeoutSec,
		CreatedAt:   m.CreatedAt.Format(time.RFC3339),
		UpdatedAt:   m.UpdatedAt.Format(time.RFC3339),
	}
}

func (s *McpServerService) Create(ctx context.Context, meta *biz.McpServerMeta) (*biz.McpServerMeta, error) {
	out, err := s.uc.Create(ctx, meta)
	if err != nil {
		s.log.Errorf("CreateMcpServer failed: id=%s err=%v", meta.ID, err)
		return nil, err
	}
	return out, nil
}

func (s *McpServerService) Get(ctx context.Context, id string) (*biz.McpServerMeta, error) {
	out, err := s.uc.Get(ctx, id)
	if err != nil {
		s.log.Errorf("GetMcpServer failed: id=%s err=%v", id, err)
		return nil, err
	}
	return out, nil
}

func (s *McpServerService) List(ctx context.Context, page, pageSize int32, name string) ([]*biz.McpServerMeta, int, error) {
	items, total, err := s.uc.List(ctx, page, pageSize, name)
	if err != nil {
		s.log.Errorf("ListMcpServers failed: page=%d page_size=%d err=%v", page, pageSize, err)
		return nil, 0, err
	}
	return items, total, nil
}

func (s *McpServerService) Update(ctx context.Context, meta *biz.McpServerMeta) (*biz.McpServerMeta, error) {
	out, err := s.uc.Update(ctx, meta)
	if err != nil {
		s.log.Errorf("UpdateMcpServer failed: id=%s err=%v", meta.ID, err)
		return nil, err
	}
	return out, nil
}

func (s *McpServerService) Delete(ctx context.Context, id string) error {
	if err := s.uc.Delete(ctx, id); err != nil {
		s.log.Errorf("DeleteMcpServer failed: id=%s err=%v", id, err)
		return err
	}
	return nil
}

func (s *McpServerService) Test(ctx context.Context, id string) ([]string, error) {
	names, err := s.uc.TestConnection(ctx, id)
	if err != nil {
		s.log.Errorf("TestMcpServer failed: id=%s err=%v", id, err)
		return nil, err
	}
	return names, nil
}

func (s *McpServerService) BindToAgent(ctx context.Context, agentID string, serverIDs []string) error {
	if err := s.uc.BindToAgent(ctx, agentID, serverIDs); err != nil {
		s.log.Errorf("BindMcpServers failed: agent_id=%s server_ids=%v err=%v", agentID, serverIDs, err)
		return err
	}
	return nil
}

func (s *McpServerService) UnbindFromAgent(ctx context.Context, agentID string, serverIDs []string) error {
	if err := s.uc.UnbindFromAgent(ctx, agentID, serverIDs); err != nil {
		s.log.Errorf("UnbindMcpServers failed: agent_id=%s server_ids=%v err=%v", agentID, serverIDs, err)
		return err
	}
	return nil
}
