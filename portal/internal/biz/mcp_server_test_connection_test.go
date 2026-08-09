package biz

import (
	"context"
	"strings"
	"testing"

	"github.com/go-kratos/kratos/v2/log"
)

func TestMcpServerGetMasksEnv(t *testing.T) {
	uc, servers, resources := newMcpServerACLUsecase()
	servers.servers["srv-1"] = &McpServerMeta{
		ID:        "srv-1",
		Name:      "s",
		Transport: "stdio",
		Command:   "npx",
		Args:      []string{"-y", "x"},
		Env:       map[string]string{"CONFLUENCE_API_TOKEN": "super-secret", "HOST": "h"},
	}
	resource := &Resource{
		ID: "resource-1", Type: ResourceTypeMcpServer, PayloadRef: "srv-1",
		OwnerUserID: "owner", Visibility: VisibilityPrivate,
	}
	resources.resources[resource.ID] = resource
	resources.byPayload["mcp_server:srv-1"] = resource

	got, err := uc.Get(WithCallerUserID(context.Background(), "owner"), "srv-1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Env["CONFLUENCE_API_TOKEN"] != "***" {
		t.Fatalf("token not masked: %#v", got.Env)
	}
	if got.Env["HOST"] != "h" {
		t.Fatalf("non-sensitive env changed: %#v", got.Env)
	}
	if servers.servers["srv-1"].Env["CONFLUENCE_API_TOKEN"] != "super-secret" {
		t.Fatal("storage env should remain unmasked")
	}
}

func TestRedactMcpErr(t *testing.T) {
	_ = log.DefaultLogger
	msg := redactMcpErr(errString("spawn failed TOKEN=abc SECRET=xyz"))
	if strings.Contains(msg, "abc") || strings.Contains(msg, "xyz") || !strings.Contains(msg, "redacted") {
		t.Fatalf("expected redaction, got %q", msg)
	}
	plain := redactMcpErr(errString("connection refused"))
	if plain != "connection refused" {
		t.Fatalf("got %q", plain)
	}
}

type errString string

func (e errString) Error() string { return string(e) }
