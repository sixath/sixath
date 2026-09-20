package service

import (
	"context"
	"time"

	"backend/internal/biz"

	"github.com/go-kratos/kratos/v2/log"
)

// ProxyService exposes proxy CRUD and test-connection helpers.
type ProxyService struct {
	uc  *biz.ProxyUsecase
	log *log.Helper
}

// NewProxyService creates a ProxyService.
func NewProxyService(uc *biz.ProxyUsecase, logger log.Logger) *ProxyService {
	return &ProxyService{uc: uc, log: log.NewHelper(logger)}
}

// ProxyDTO is the JSON shape for proxy APIs. Password is never included.
type ProxyDTO struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Type        string   `json:"type"`
	Host        string   `json:"host"`
	Port        int      `json:"port"`
	User        string   `json:"user"`
	HasPassword bool     `json:"has_password"`
	NoProxy     []string `json:"no_proxy,omitempty"`
	CreatedAt   string   `json:"created_at,omitempty"`
	UpdatedAt   string   `json:"updated_at,omitempty"`
}

// ProxyDTOFromMeta maps biz meta to API DTO.
func ProxyDTOFromMeta(m *biz.ProxyMeta) ProxyDTO {
	if m == nil {
		return ProxyDTO{}
	}
	return ProxyDTO{
		ID:          m.ID,
		Name:        m.Name,
		Description: m.Description,
		Type:        m.Type,
		Host:        m.Host,
		Port:        m.Port,
		User:        m.User,
		HasPassword: m.HasPassword,
		NoProxy:     m.NoProxy,
		CreatedAt:   formatProxyTime(m.CreatedAt),
		UpdatedAt:   formatProxyTime(m.UpdatedAt),
	}
}

func formatProxyTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format(time.RFC3339)
}

func (s *ProxyService) Create(ctx context.Context, meta *biz.ProxyMeta) (*biz.ProxyMeta, error) {
	out, err := s.uc.Create(ctx, meta)
	if err != nil {
		s.log.Errorf("CreateProxy failed: id=%s err=%v", meta.ID, err)
		return nil, err
	}
	return out, nil
}

func (s *ProxyService) Get(ctx context.Context, id string) (*biz.ProxyMeta, error) {
	out, err := s.uc.Get(ctx, id)
	if err != nil {
		s.log.Errorf("GetProxy failed: id=%s err=%v", id, err)
		return nil, err
	}
	return out, nil
}

func (s *ProxyService) List(ctx context.Context, page, pageSize int32, name string, bindable bool) ([]*biz.ProxyMeta, int, error) {
	items, total, err := s.uc.List(ctx, page, pageSize, name, bindable)
	if err != nil {
		s.log.Errorf("ListProxies failed: page=%d page_size=%d err=%v", page, pageSize, err)
		return nil, 0, err
	}
	return items, total, nil
}

func (s *ProxyService) Update(ctx context.Context, meta *biz.ProxyMeta) (*biz.ProxyMeta, error) {
	out, err := s.uc.Update(ctx, meta)
	if err != nil {
		s.log.Errorf("UpdateProxy failed: id=%s err=%v", meta.ID, err)
		return nil, err
	}
	return out, nil
}

func (s *ProxyService) Delete(ctx context.Context, id string) error {
	if err := s.uc.Delete(ctx, id); err != nil {
		s.log.Errorf("DeleteProxy failed: id=%s err=%v", id, err)
		return err
	}
	return nil
}

func (s *ProxyService) Test(ctx context.Context, id string) error {
	if err := s.uc.TestConnection(ctx, id); err != nil {
		s.log.Errorf("TestProxy failed: id=%s err=%v", id, err)
		return err
	}
	return nil
}
