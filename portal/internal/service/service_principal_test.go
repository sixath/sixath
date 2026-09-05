package service

import (
	"testing"

	"backend/internal/conf"
)

func TestServicePrincipalUserID_PrefersConfiguredPrincipal(t *testing.T) {
	got := servicePrincipalUserID(&conf.Auth{
		BootstrapUserId:        "bootstrap-user",
		ServicePrincipalUserId: "service-principal",
	})
	if got != "service-principal" {
		t.Fatalf("service principal user ID = %q, want %q", got, "service-principal")
	}
}

func TestServicePrincipalUserID_FallsBackToBootstrapUser(t *testing.T) {
	got := servicePrincipalUserID(&conf.Auth{BootstrapUserId: "bootstrap-user"})
	if got != "bootstrap-user" {
		t.Fatalf("service principal user ID = %q, want %q", got, "bootstrap-user")
	}
}
