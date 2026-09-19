package biz

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	pkgErrors "backend/internal/pkg/errors"

	kratosErrors "github.com/go-kratos/kratos/v2/errors"
	"github.com/go-kratos/kratos/v2/log"
	"github.com/sixath/framework/netx"
)

const maxProxyDeleteRefs = 20

var proxyIDRe = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,35}$`)

// ProxyMeta is a named HTTP/SOCKS5 egress proxy.
type ProxyMeta struct {
	ID          string
	Name        string
	Description string
	Type        string
	Host        string
	Port        int
	User        string
	Password    string
	HasPassword bool
	NoProxy     []string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// ProxyReference identifies an agent or tool that still points at a proxy.
type ProxyReference struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
	Name string `json:"name"`
}

// ProxyInUseError is returned when Delete is blocked by remaining references.
type ProxyInUseError struct {
	References []ProxyReference `json:"references"`
	Truncated  bool             `json:"truncated"`
	err        *kratosErrors.Error
}

func newProxyInUseError(refs []ProxyReference, truncated bool) *ProxyInUseError {
	if refs == nil {
		refs = []ProxyReference{}
	}
	return &ProxyInUseError{
		References: refs,
		Truncated:  truncated,
		err:        kratosErrors.Conflict("PROXY_IN_USE", "proxy is referenced by agents or tools"),
	}
}

func (e *ProxyInUseError) Error() string {
	if e == nil || e.err == nil {
		return "proxy is referenced by agents or tools"
	}
	return e.err.Error()
}

func (e *ProxyInUseError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.err
}

// ProxyRepo persists proxies and looks up delete-time references.
type ProxyRepo interface {
	Create(ctx context.Context, meta *ProxyMeta) (*ProxyMeta, error)
	GetByID(ctx context.Context, id string) (*ProxyMeta, error)
	List(ctx context.Context, opts ListOptions) ([]*ProxyMeta, int, error)
	Update(ctx context.Context, meta *ProxyMeta) (*ProxyMeta, error)
	Delete(ctx context.Context, id string) error
	ListReferences(ctx context.Context, proxyID string, limit int) ([]ProxyReference, bool, error)
}

var (
	ErrProxyNotFound     = kratosErrors.NotFound("PROXY_NOT_FOUND", "proxy not found")
	ErrProxyDuplicateID  = kratosErrors.Conflict("PROXY_DUPLICATE_ID", "proxy id already exists")
	ErrProxyInvalidInput = kratosErrors.BadRequest("INVALID_ARGUMENT", "invalid proxy input")
)

// ValidateProxyID checks the user-facing slug id.
func ValidateProxyID(id string) error {
	if !proxyIDRe.MatchString(id) {
		return fmt.Errorf("proxy id must match %s", proxyIDRe.String())
	}
	return nil
}

// ValidateProxyInput validates type, host, and port.
func ValidateProxyInput(m *ProxyMeta) error {
	if m == nil {
		return errors.New("proxy is required")
	}
	if err := ValidateProxyID(m.ID); err != nil {
		return err
	}
	if strings.TrimSpace(m.Name) == "" {
		return errors.New("name is required")
	}
	typ := strings.ToLower(strings.TrimSpace(m.Type))
	switch typ {
	case netx.TypeHTTP, netx.TypeSOCKS5:
	default:
		return fmt.Errorf("type must be http or socks5, got %q", m.Type)
	}
	host := strings.TrimSpace(m.Host)
	if host == "" {
		return errors.New("host is required")
	}
	if strings.Contains(host, "@") {
		return errors.New("host must not contain credentials")
	}
	if m.Port < 1 || m.Port > 65535 {
		return errors.New("port must be between 1 and 65535")
	}
	return nil
}

func maskProxyMeta(m *ProxyMeta) *ProxyMeta {
	if m == nil {
		return nil
	}
	cp := *m
	cp.HasPassword = strings.TrimSpace(m.Password) != ""
	cp.Password = ""
	if m.NoProxy != nil {
		cp.NoProxy = append([]string(nil), m.NoProxy...)
	}
	return &cp
}

func normalizeProxyMeta(m *ProxyMeta) {
	if m == nil {
		return
	}
	m.ID = strings.TrimSpace(m.ID)
	m.Name = strings.TrimSpace(m.Name)
	m.Type = strings.ToLower(strings.TrimSpace(m.Type))
	m.Host = strings.TrimSpace(m.Host)
	m.User = strings.TrimSpace(m.User)
}

// ProxyUsecase is the proxy use case.
type ProxyUsecase struct {
	repo      ProxyRepo
	resources ResourceRepo
	access    *AccessChecker
	log       *log.Helper
}

// NewProxyUsecase creates a ProxyUsecase.
func NewProxyUsecase(repo ProxyRepo, resources ResourceRepo, access *AccessChecker, logger log.Logger) *ProxyUsecase {
	return &ProxyUsecase{repo: repo, resources: resources, access: access, log: log.NewHelper(logger)}
}

// Create creates a proxy and a private ACL resource for the caller.
func (uc *ProxyUsecase) Create(ctx context.Context, meta *ProxyMeta) (*ProxyMeta, error) {
	caller, err := requireCaller(ctx)
	if err != nil {
		return nil, err
	}
	if meta == nil {
		return nil, ErrProxyInvalidInput
	}
	normalizeProxyMeta(meta)
	if err := ValidateProxyInput(meta); err != nil {
		return nil, kratosErrors.BadRequest("INVALID_ARGUMENT", err.Error())
	}
	created, err := uc.repo.Create(ctx, meta)
	if err != nil && errors.Is(err, pkgErrors.ErrDuplicateName) {
		return nil, ErrProxyDuplicateID
	}
	if err != nil {
		return nil, err
	}
	if _, err := uc.resources.CreateResource(ctx, &Resource{
		Type:        ResourceTypeProxy,
		Name:        created.Name,
		OwnerUserID: caller,
		Visibility:  VisibilityPrivate,
		PayloadRef:  created.ID,
	}); err != nil {
		_ = uc.repo.Delete(ctx, created.ID)
		return nil, err
	}
	return maskProxyMeta(created), nil
}

// Get returns a proxy by ID with password hidden.
func (uc *ProxyUsecase) Get(ctx context.Context, id string) (*ProxyMeta, error) {
	if _, err := uc.requireProxyPerm(ctx, id, PermView); err != nil {
		return nil, err
	}
	meta, err := uc.repo.GetByID(ctx, id)
	if err != nil && errors.Is(err, pkgErrors.ErrNotFound) {
		return nil, ErrProxyNotFound
	}
	if err != nil {
		return nil, err
	}
	return maskProxyMeta(meta), nil
}

// List lists proxies visible to the caller (password hidden).
// bindable=true requires PermUse (Agent/Tool dropdowns); default stays PermView.
func (uc *ProxyUsecase) List(ctx context.Context, page, pageSize int32, name string, bindable bool) ([]*ProxyMeta, int, error) {
	caller, err := requireCaller(ctx)
	if err != nil {
		return nil, 0, err
	}
	need := PermView
	if bindable {
		need = PermUse
	}
	allowed, err := VisiblePayloadRefs(ctx, uc.resources, caller, ResourceTypeProxy, need)
	if err != nil {
		return nil, 0, err
	}
	ids := make([]string, 0, len(allowed))
	for id := range allowed {
		ids = append(ids, id)
	}
	if len(ids) == 0 {
		return []*ProxyMeta{}, 0, nil
	}
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 10
	}
	items, total, err := uc.repo.List(ctx, ListOptions{Page: page, PageSize: pageSize, Name: name, IDs: ids})
	if err != nil {
		return nil, 0, err
	}
	out := make([]*ProxyMeta, len(items))
	for i, item := range items {
		out[i] = maskProxyMeta(item)
	}
	return out, total, nil
}

// Update updates a proxy. Empty password keeps the stored secret.
func (uc *ProxyUsecase) Update(ctx context.Context, meta *ProxyMeta) (*ProxyMeta, error) {
	if meta == nil {
		return nil, ErrProxyInvalidInput
	}
	normalizeProxyMeta(meta)
	resource, err := uc.requireProxyPerm(ctx, meta.ID, PermEdit)
	if err != nil {
		return nil, err
	}
	existing, err := uc.repo.GetByID(ctx, meta.ID)
	if err != nil && errors.Is(err, pkgErrors.ErrNotFound) {
		return nil, ErrProxyNotFound
	}
	if err != nil {
		return nil, err
	}
	merged := *meta
	if merged.Password == "" {
		merged.Password = existing.Password
	}
	if err := ValidateProxyInput(&merged); err != nil {
		return nil, kratosErrors.BadRequest("INVALID_ARGUMENT", err.Error())
	}
	updated, err := uc.repo.Update(ctx, &merged)
	if err != nil && errors.Is(err, pkgErrors.ErrNotFound) {
		return nil, ErrProxyNotFound
	}
	if err != nil {
		return nil, err
	}
	if resource.Name != updated.Name {
		resource.Name = updated.Name
		if err := uc.resources.UpdateResource(ctx, resource); err != nil {
			return nil, err
		}
	}
	return maskProxyMeta(updated), nil
}

// Delete deletes a proxy when no agent or tool still references it.
func (uc *ProxyUsecase) Delete(ctx context.Context, id string) error {
	resource, err := uc.requireProxyPerm(ctx, id, PermAdmin)
	if err != nil {
		return err
	}
	refs, truncated, err := uc.repo.ListReferences(ctx, id, maxProxyDeleteRefs)
	if err != nil {
		return err
	}
	if len(refs) > 0 {
		return newProxyInUseError(refs, truncated)
	}
	err = uc.repo.Delete(ctx, id)
	if err != nil && errors.Is(err, pkgErrors.ErrNotFound) {
		return ErrProxyNotFound
	}
	if err != nil {
		return err
	}
	if err := uc.resources.DeleteResource(ctx, resource.ID); err != nil && !errors.Is(err, pkgErrors.ErrNotFound) {
		return err
	}
	return nil
}

// TestConnection probes the proxy handshake. Requires PermUse (after view).
func (uc *ProxyUsecase) TestConnection(ctx context.Context, id string) error {
	if _, err := uc.requireProxyPerm(ctx, id, PermUse); err != nil {
		return err
	}
	meta, err := uc.repo.GetByID(ctx, id)
	if err != nil && errors.Is(err, pkgErrors.ErrNotFound) {
		return ErrProxyNotFound
	}
	if err != nil {
		return err
	}
	if err := testProxyConnection(ctx, meta); err != nil {
		safe := redactProxyTestErr(meta, err)
		uc.log.Warnf("proxy test connection failed: id=%s host=%s type=%s err=%v", meta.ID, meta.Host, meta.Type, safe)
		return kratosErrors.BadRequest("PROXY_TEST_FAILED", safe.Error())
	}
	return nil
}

func (uc *ProxyUsecase) requireProxyPerm(ctx context.Context, proxyID string, need Perm) (*Resource, error) {
	caller, err := requireCaller(ctx)
	if err != nil {
		return nil, err
	}
	resource, err := uc.resources.GetByPayload(ctx, ResourceTypeProxy, proxyID)
	if err != nil {
		return nil, ErrProxyNotFound
	}
	canView, err := uc.access.Can(ctx, caller, resource.ID, PermView, "")
	if err != nil {
		return nil, err
	}
	if !canView {
		return nil, ErrProxyNotFound
	}
	can, err := uc.access.Can(ctx, caller, resource.ID, need, "")
	if err != nil {
		return nil, err
	}
	if !can {
		return nil, ErrForbiddenPerm
	}
	return resource, nil
}
