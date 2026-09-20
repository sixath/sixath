package biz

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/go-kratos/kratos/v2/log"
)

type fakeProxyRepo struct {
	proxies map[string]*ProxyMeta
	refs    []ProxyReference
}

func (f *fakeProxyRepo) Create(_ context.Context, meta *ProxyMeta) (*ProxyMeta, error) {
	cp := cloneProxyMeta(meta)
	f.proxies[cp.ID] = cp
	return cloneProxyMeta(cp), nil
}

func (f *fakeProxyRepo) GetByID(_ context.Context, id string) (*ProxyMeta, error) {
	m, ok := f.proxies[id]
	if !ok {
		return nil, errNotFound
	}
	return cloneProxyMeta(m), nil
}

func (f *fakeProxyRepo) List(_ context.Context, opts ListOptions) ([]*ProxyMeta, int, error) {
	items := make([]*ProxyMeta, 0, len(f.proxies))
	for _, p := range f.proxies {
		items = append(items, cloneProxyMeta(p))
	}
	if opts.IDs != nil {
		allow := make(map[string]struct{}, len(opts.IDs))
		for _, id := range opts.IDs {
			allow[id] = struct{}{}
		}
		filtered := make([]*ProxyMeta, 0, len(items))
		for _, p := range items {
			if _, ok := allow[p.ID]; ok {
				filtered = append(filtered, p)
			}
		}
		items = filtered
	}
	return items, len(items), nil
}

func (f *fakeProxyRepo) Update(_ context.Context, meta *ProxyMeta) (*ProxyMeta, error) {
	if _, ok := f.proxies[meta.ID]; !ok {
		return nil, errNotFound
	}
	cp := cloneProxyMeta(meta)
	f.proxies[cp.ID] = cp
	return cloneProxyMeta(cp), nil
}

func (f *fakeProxyRepo) Delete(_ context.Context, id string) error {
	if _, ok := f.proxies[id]; !ok {
		return errNotFound
	}
	delete(f.proxies, id)
	return nil
}

func (f *fakeProxyRepo) ListReferences(_ context.Context, _ string, limit int) ([]ProxyReference, bool, error) {
	if limit <= 0 {
		limit = 20
	}
	refs := append([]ProxyReference(nil), f.refs...)
	truncated := len(refs) > limit
	if truncated {
		refs = refs[:limit]
	}
	return refs, truncated, nil
}

func cloneProxyMeta(m *ProxyMeta) *ProxyMeta {
	if m == nil {
		return nil
	}
	cp := *m
	if m.NoProxy != nil {
		cp.NoProxy = append([]string(nil), m.NoProxy...)
	}
	return &cp
}

func newProxyACLUsecase() (*ProxyUsecase, *fakeProxyRepo, *fakeMcpResourceRepo) {
	proxies := &fakeProxyRepo{proxies: map[string]*ProxyMeta{}}
	resources := &fakeMcpResourceRepo{
		fakeResourceReader: fakeResourceReader{
			resources: map[string]*Resource{},
			grants:    map[string][]ResourceGrant{},
			userOrgs:  map[string][]string{},
		},
		byPayload: map[string]*Resource{},
	}
	return NewProxyUsecase(proxies, resources, NewAccessChecker(resources), log.NewStdLogger(io.Discard)), proxies, resources
}

func seedProxyResource(resources *fakeMcpResourceRepo, proxyID, owner string) *Resource {
	resource := &Resource{
		ID:          "resource-" + proxyID,
		Type:        ResourceTypeProxy,
		Name:        proxyID,
		OwnerUserID: owner,
		Visibility:  VisibilityPrivate,
		PayloadRef:  proxyID,
	}
	resources.resources[resource.ID] = resource
	resources.byPayload[string(ResourceTypeProxy)+":"+proxyID] = resource
	return resource
}

func TestCreateProxy_HidesPassword(t *testing.T) {
	uc, repo, resources := newProxyACLUsecase()
	got, err := uc.Create(WithCallerUserID(context.Background(), "user-1"), &ProxyMeta{
		ID:       "office",
		Name:     "Office",
		Type:     "http",
		Host:     "127.0.0.1",
		Port:     8080,
		User:     "alice",
		Password: "s3cret",
		NoProxy:  []string{"es.local"},
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if got.Password != "" {
		t.Fatalf("Create leaked password %q", got.Password)
	}
	if !got.HasPassword {
		t.Fatal("Create should set has_password")
	}
	raw, _ := json.Marshal(got)
	if strings.Contains(string(raw), "s3cret") {
		t.Fatalf("password in JSON: %s", raw)
	}
	stored := repo.proxies["office"]
	if stored == nil || stored.Password != "s3cret" {
		t.Fatalf("stored password = %#v", stored)
	}
	if len(resources.created) != 1 {
		t.Fatalf("created resources = %d, want 1", len(resources.created))
	}
	resource := resources.created[0]
	if resource.Type != ResourceTypeProxy || resource.PayloadRef != "office" || resource.OwnerUserID != "user-1" || resource.Visibility != VisibilityPrivate {
		t.Fatalf("resource = %#v", resource)
	}
}

func TestUpdateProxy_EmptyPasswordKeeps(t *testing.T) {
	uc, repo, resources := newProxyACLUsecase()
	repo.proxies["office"] = &ProxyMeta{
		ID: "office", Name: "Office", Type: "http",
		Host: "127.0.0.1", Port: 8080, Password: "keep-me",
	}
	seedProxyResource(resources, "office", "owner")

	got, err := uc.Update(WithCallerUserID(context.Background(), "owner"), &ProxyMeta{
		ID: "office", Name: "Office-2", Type: "http",
		Host: "10.0.0.1", Port: 9090, Password: "",
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if repo.proxies["office"].Password != "keep-me" {
		t.Fatalf("stored password = %q, want keep-me", repo.proxies["office"].Password)
	}
	if repo.proxies["office"].Host != "10.0.0.1" || repo.proxies["office"].Name != "Office-2" {
		t.Fatalf("stored = %#v", repo.proxies["office"])
	}
	if got.Password != "" {
		t.Fatalf("Update leaked password %q", got.Password)
	}
	if !got.HasPassword {
		t.Fatal("Update should keep has_password")
	}
}

func TestDeleteProxy_ConflictWhenAgentRefs(t *testing.T) {
	uc, repo, resources := newProxyACLUsecase()
	repo.proxies["office"] = &ProxyMeta{ID: "office", Name: "Office", Type: "http", Host: "127.0.0.1", Port: 8080}
	repo.refs = []ProxyReference{{Kind: "agent", ID: "agt-1", Name: "Bot"}}
	seedProxyResource(resources, "office", "owner")

	err := uc.Delete(WithCallerUserID(context.Background(), "owner"), "office")
	if !isReason(err, "PROXY_IN_USE") {
		t.Fatalf("Delete error = %v, want PROXY_IN_USE", err)
	}
	var inUse *ProxyInUseError
	if !errors.As(err, &inUse) {
		t.Fatalf("error type %T, want *ProxyInUseError", err)
	}
	if len(inUse.References) != 1 || inUse.References[0].ID != "agt-1" || inUse.References[0].Kind != "agent" {
		t.Fatalf("references = %#v", inUse.References)
	}
	if inUse.Truncated {
		t.Fatal("truncated should be false")
	}
	if _, ok := repo.proxies["office"]; !ok {
		t.Fatal("proxy should remain when referenced")
	}
}

func TestProxyGetHidesFromStranger(t *testing.T) {
	uc, repo, resources := newProxyACLUsecase()
	repo.proxies["office"] = &ProxyMeta{ID: "office", Name: "Office", Type: "http", Host: "127.0.0.1", Port: 8080}
	seedProxyResource(resources, "office", "owner")

	if _, err := uc.Get(WithCallerUserID(context.Background(), "stranger"), "office"); !isReason(err, "PROXY_NOT_FOUND") {
		t.Fatalf("Get without view error = %v, want PROXY_NOT_FOUND", err)
	}
}

func TestProxyTestConnectionRequiresUse(t *testing.T) {
	uc, repo, resources := newProxyACLUsecase()
	repo.proxies["office"] = &ProxyMeta{ID: "office", Name: "Office", Type: "http", Host: "127.0.0.1", Port: 1}
	resource := seedProxyResource(resources, "office", "owner")
	resources.grants[resource.ID] = []ResourceGrant{{
		ResourceID: resource.ID, GranteeType: "user", GranteeID: "viewer", Perm: PermView,
	}}

	if err := uc.TestConnection(WithCallerUserID(context.Background(), "stranger"), "office"); !isReason(err, "PROXY_NOT_FOUND") {
		t.Fatalf("stranger TestConnection = %v, want PROXY_NOT_FOUND", err)
	}
	if err := uc.TestConnection(WithCallerUserID(context.Background(), "viewer"), "office"); !isReason(err, "FORBIDDEN_PERM") {
		t.Fatalf("view-only TestConnection = %v, want FORBIDDEN_PERM", err)
	}
}

func TestProxyList_BindableRequiresUse(t *testing.T) {
	uc, repo, resources := newProxyACLUsecase()
	repo.proxies["office"] = &ProxyMeta{ID: "office", Name: "Office", Type: "http", Host: "127.0.0.1", Port: 8080}
	resource := seedProxyResource(resources, "office", "owner")
	resources.grants[resource.ID] = []ResourceGrant{{
		ResourceID: resource.ID, GranteeType: "user", GranteeID: "viewer", Perm: PermView,
	}}

	viewItems, _, err := uc.List(WithCallerUserID(context.Background(), "viewer"), 1, 10, "", false)
	if err != nil {
		t.Fatalf("default List: %v", err)
	}
	if len(viewItems) != 1 || viewItems[0].ID != "office" {
		t.Fatalf("view list = %#v, want office", viewItems)
	}

	bindItems, _, err := uc.List(WithCallerUserID(context.Background(), "viewer"), 1, 10, "", true)
	if err != nil {
		t.Fatalf("bindable List: %v", err)
	}
	if len(bindItems) != 0 {
		t.Fatalf("view-only caller must be absent from bindable list, got %#v", bindItems)
	}

	ownerBind, _, err := uc.List(WithCallerUserID(context.Background(), "owner"), 1, 10, "", true)
	if err != nil {
		t.Fatalf("owner bindable List: %v", err)
	}
	if len(ownerBind) != 1 || ownerBind[0].ID != "office" {
		t.Fatalf("owner bindable list = %#v, want office", ownerBind)
	}
}

func TestValidateProxyInput(t *testing.T) {
	valid := &ProxyMeta{ID: "office", Name: "Office", Type: "http", Host: "127.0.0.1", Port: 8080}
	if err := ValidateProxyInput(valid); err != nil {
		t.Fatalf("valid: %v", err)
	}
	if err := ValidateProxyID("Office"); err == nil {
		t.Fatal("uppercase id should fail")
	}
	if err := ValidateProxyInput(&ProxyMeta{ID: "office", Name: "x", Type: "https", Host: "h", Port: 80}); err == nil {
		t.Fatal("https type should fail")
	}
	if err := ValidateProxyInput(&ProxyMeta{ID: "office", Name: "x", Type: "http", Host: "user:pass@host", Port: 80}); err == nil {
		t.Fatal("embedded credentials should fail")
	}
	if err := ValidateProxyInput(&ProxyMeta{ID: "office", Name: "x", Type: "http", Host: "h", Port: 0}); err == nil {
		t.Fatal("port 0 should fail")
	}
}

func TestTestConnectionHTTP_200(t *testing.T) {
	sawCONNECT := false
	okSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodConnect {
			sawCONNECT = true
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusMethodNotAllowed)
	}))
	defer okSrv.Close()

	uc, repo, resources := newProxyACLUsecase()
	host, port := mustHostPort(t, okSrv.URL)
	repo.proxies["office"] = &ProxyMeta{ID: "office", Name: "Office", Type: "http", Host: host, Port: port}
	seedProxyResource(resources, "office", "owner")

	if err := uc.TestConnection(WithCallerUserID(context.Background(), "owner"), "office"); err != nil {
		t.Fatalf("CONNECT 200 should succeed: %v", err)
	}
	if !sawCONNECT {
		t.Fatal("proxy did not receive CONNECT")
	}

	failSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusProxyAuthRequired)
	}))
	defer failSrv.Close()
	failHost, failPort := mustHostPort(t, failSrv.URL)
	repo.proxies["office"].Host = failHost
	repo.proxies["office"].Port = failPort

	err := uc.TestConnection(WithCallerUserID(context.Background(), "owner"), "office")
	if err == nil {
		t.Fatal("CONNECT 407 must fail")
	}
	if strings.Contains(err.Error(), "keep-me") || strings.Contains(err.Error(), "s3cret") {
		t.Fatalf("error leaked password: %v", err)
	}
}

func TestProxyTestConnectionSOCKS5NoAuth(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	done := make(chan struct{})
	go func() {
		defer close(done)
		conn, acceptErr := ln.Accept()
		if acceptErr != nil {
			return
		}
		defer conn.Close()
		buf := make([]byte, 3)
		if _, readErr := io.ReadFull(conn, buf); readErr != nil {
			return
		}
		if buf[0] != 0x05 || buf[1] != 0x01 || buf[2] != 0x00 {
			return
		}
		_, _ = conn.Write([]byte{0x05, 0x00})
	}()

	uc, repo, resources := newProxyACLUsecase()
	host, portStr, _ := net.SplitHostPort(ln.Addr().String())
	port, _ := strconv.Atoi(portStr)
	repo.proxies["lab"] = &ProxyMeta{ID: "lab", Name: "Lab", Type: "socks5", Host: host, Port: port}
	seedProxyResource(resources, "lab", "owner")

	if err := uc.TestConnection(WithCallerUserID(context.Background(), "owner"), "lab"); err != nil {
		t.Fatalf("SOCKS5 no-auth handshake should succeed: %v", err)
	}
	<-done
}

func mustHostPort(t *testing.T, rawURL string) (string, int) {
	t.Helper()
	u, err := url.Parse(rawURL)
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(u.Port())
	if err != nil {
		t.Fatal(err)
	}
	return u.Hostname(), port
}
