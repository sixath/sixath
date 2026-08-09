package service

import (
	"context"
	"testing"

	"backend/internal/biz"
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

func TestGrowthWorker_InternalContextCarriesServicePrincipal(t *testing.T) {
	worker := &GrowthWorker{servicePrincipalUserID: "service-principal"}

	got, ok := biz.CallerUserID(worker.internalContext(context.Background()))
	if !ok || got != "service-principal" {
		t.Fatalf("caller user ID = %q, present = %t; want service principal", got, ok)
	}
}
