package cron

import (
	"context"
	"testing"

	"backend/internal/biz"
)

func TestExecutor_InternalContextCarriesServicePrincipal(t *testing.T) {
	executor := &Executor{servicePrincipalUserID: "service-principal"}

	got, ok := biz.CallerUserID(executor.internalContext(context.Background()))
	if !ok || got != "service-principal" {
		t.Fatalf("caller user ID = %q, present = %t; want service principal", got, ok)
	}
}
