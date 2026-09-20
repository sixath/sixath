package biz

import (
	"context"
	"testing"
)

func TestValidateToolEgress_IllegalMode(t *testing.T) {
	err := ValidateToolEgress(context.Background(), "user-1", mustStruct(t, map[string]any{
		"egress_mode": "wireguard",
	}), nil, nil, nil)
	if !isReason(err, "TOOL_EGRESS_CONFIG") {
		t.Fatalf("illegal mode error = %v, want TOOL_EGRESS_CONFIG", err)
	}
}

func TestValidateToolEgress_ProxyWithoutID(t *testing.T) {
	err := ValidateToolEgress(context.Background(), "user-1", mustStruct(t, map[string]any{
		"egress_mode": "proxy",
	}), nil, nil, nil)
	if !isReason(err, "TOOL_EGRESS_CONFIG") {
		t.Fatalf("proxy without id error = %v, want TOOL_EGRESS_CONFIG", err)
	}
}

func TestValidateToolEgress_EditorLacksUse(t *testing.T) {
	proxies, resources := seedToolEgressProxy(t, "office", "http", "proxy-owner")
	resources.grants["resource-office"] = []ResourceGrant{{ResourceID: "resource-office", GranteeType: "user", GranteeID: "editor", Perm: PermView}}
	err := ValidateToolEgress(context.Background(), "editor", mustStruct(t, map[string]any{
		"egress_mode": "proxy",
		"proxy_id":    "office",
	}), proxies, resources, NewAccessChecker(resources))
	if !isReason(err, "FORBIDDEN_PERM") {
		t.Fatalf("editor without use error = %v, want FORBIDDEN_PERM", err)
	}
}

func TestValidateToolEgress_MySQLHTTPProxyRejected(t *testing.T) {
	proxies, resources := seedToolEgressProxy(t, "office", "http", "editor")
	err := ValidateToolEgress(context.Background(), "editor", mustStruct(t, map[string]any{
		"egress_mode": "proxy",
		"proxy_id":    "office",
		"datasource":  map[string]any{"type": "mysql", "dsn": "u:p@tcp(h:3306)/db"},
	}), proxies, resources, NewAccessChecker(resources))
	if !isReason(err, "TOOL_EGRESS_CONFIG") {
		t.Fatalf("mysql+http proxy error = %v, want TOOL_EGRESS_CONFIG", err)
	}
}

func TestValidateToolEgress_MySQLInheritNotRejectedAtSave(t *testing.T) {
	err := ValidateToolEgress(context.Background(), "editor", mustStruct(t, map[string]any{
		"egress_mode": "inherit",
		"datasource":  map[string]any{"type": "mysql", "dsn": "u:p@tcp(h:3306)/db"},
	}), nil, nil, nil)
	if err != nil {
		t.Fatalf("mysql inherit must not be rejected at save: %v", err)
	}
}

func TestValidateToolEgress_HiveProxyRejected(t *testing.T) {
	proxies, resources := seedToolEgressProxy(t, "lab", "socks5", "editor")
	err := ValidateToolEgress(context.Background(), "editor", mustStruct(t, map[string]any{
		"egress_mode": "proxy",
		"proxy_id":    "lab",
		"datasource":  map[string]any{"type": "hive"},
	}), proxies, resources, NewAccessChecker(resources))
	if !isReason(err, "TOOL_EGRESS_CONFIG") {
		t.Fatalf("hive egress_mode=proxy error = %v, want TOOL_EGRESS_CONFIG", err)
	}
}

func TestValidateToolEgress_MongoSOCKS5Allowed(t *testing.T) {
	proxies, resources := seedToolEgressProxy(t, "lab", "socks5", "editor")
	for _, typ := range []string{"mongo", "mongodb"} {
		err := ValidateToolEgress(context.Background(), "editor", mustStruct(t, map[string]any{
			"egress_mode": "proxy",
			"proxy_id":    "lab",
			"datasource":  map[string]any{"type": typ},
		}), proxies, resources, NewAccessChecker(resources))
		if err != nil {
			t.Fatalf("%s+socks5 must be allowed: %v", typ, err)
		}
	}
}

func TestValidateToolEgress_MongoHTTPProxyRejected(t *testing.T) {
	proxies, resources := seedToolEgressProxy(t, "office", "http", "editor")
	err := ValidateToolEgress(context.Background(), "editor", mustStruct(t, map[string]any{
		"egress_mode": "proxy",
		"proxy_id":    "office",
		"datasource":  map[string]any{"type": "mongodb"},
	}), proxies, resources, NewAccessChecker(resources))
	if !isReason(err, "TOOL_EGRESS_CONFIG") {
		t.Fatalf("mongodb+http proxy error = %v, want TOOL_EGRESS_CONFIG", err)
	}
}

func TestValidateToolEgress_MySQLSOCKS5Allowed(t *testing.T) {
	proxies, resources := seedToolEgressProxy(t, "lab", "socks5", "editor")
	err := ValidateToolEgress(context.Background(), "editor", mustStruct(t, map[string]any{
		"egress_mode": "proxy",
		"proxy_id":    "lab",
		"datasource":  map[string]any{"type": "mysql"},
	}), proxies, resources, NewAccessChecker(resources))
	if err != nil {
		t.Fatalf("mysql+socks5 must be allowed: %v", err)
	}
}

func seedToolEgressProxy(t *testing.T, id, typ, owner string) (*fakeProxyRepo, *fakeMcpResourceRepo) {
	t.Helper()
	proxies := &fakeProxyRepo{proxies: map[string]*ProxyMeta{
		id: {ID: id, Name: id, Type: typ, Host: "127.0.0.1", Port: 1080},
	}}
	resources := &fakeMcpResourceRepo{
		fakeResourceReader: fakeResourceReader{
			resources: map[string]*Resource{},
			grants:    map[string][]ResourceGrant{},
			userOrgs:  map[string][]string{},
		},
		byPayload: map[string]*Resource{},
	}
	res := &Resource{ID: "resource-" + id, Type: ResourceTypeProxy, PayloadRef: id, OwnerUserID: owner, Visibility: VisibilityPrivate}
	resources.resources[res.ID] = res
	resources.byPayload[string(ResourceTypeProxy)+":"+id] = res
	return proxies, resources
}
