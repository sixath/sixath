package conf

import (
	"testing"
)

func TestEnrichAuthFromEnv(t *testing.T) {
	t.Setenv("SATH_BOOTSTRAP_EMAIL", "admin@example.com")
	t.Setenv("SATH_BOOTSTRAP_PASSWORD", "secret")
	t.Setenv("SATH_BOOTSTRAP_TOKEN", "tok")
	auth := EnrichAuthFromEnv(&Auth{})
	if auth.GetBootstrapEmail() != "admin@example.com" {
		t.Fatalf("email=%q", auth.GetBootstrapEmail())
	}
	if auth.GetBootstrapPassword() != "secret" {
		t.Fatalf("password=%q", auth.GetBootstrapPassword())
	}
	if auth.GetBootstrapToken() != "tok" {
		t.Fatalf("token=%q", auth.GetBootstrapToken())
	}
}

func TestEnrichAuthFromEnv_nilAuthAllocates(t *testing.T) {
	t.Setenv("SATH_BOOTSTRAP_EMAIL", "admin@example.com")
	t.Setenv("SATH_BOOTSTRAP_PASSWORD", "secret")
	t.Setenv("SATH_BOOTSTRAP_TOKEN", "")
	auth := EnrichAuthFromEnv(nil)
	if auth == nil {
		t.Fatal("expected allocated Auth")
	}
	if auth.GetBootstrapEmail() != "admin@example.com" {
		t.Fatalf("email=%q", auth.GetBootstrapEmail())
	}
	if auth.GetBootstrapPassword() != "secret" {
		t.Fatalf("password=%q", auth.GetBootstrapPassword())
	}
}

func TestEnrichAuthFromEnv_noEnvLeavesNil(t *testing.T) {
	t.Setenv("SATH_BOOTSTRAP_EMAIL", "")
	t.Setenv("SATH_BOOTSTRAP_PASSWORD", "")
	t.Setenv("SATH_BOOTSTRAP_TOKEN", "")
	if got := EnrichAuthFromEnv(nil); got != nil {
		t.Fatalf("got %#v, want nil", got)
	}
}
