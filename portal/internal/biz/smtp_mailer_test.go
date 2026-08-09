package biz

import (
	"testing"

	"backend/internal/conf"
)

func TestNewMailerNoopWhenHostEmpty(t *testing.T) {
	cases := []struct {
		name string
		auth *conf.Auth
	}{
		{"nil auth", nil},
		{"empty host", &conf.Auth{}},
		{"whitespace host", &conf.Auth{SmtpHost: "  "}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := NewMailer(tc.auth)
			if _, ok := m.(NoopMailer); !ok {
				t.Fatalf("NewMailer() = %T, want NoopMailer", m)
			}
		})
	}
}

func TestNewMailerSMTPWhenHostSet(t *testing.T) {
	m := NewMailer(&conf.Auth{SmtpHost: "smtp.example.com", SmtpPort: 587, SmtpFrom: "noreply@example.com"})
	if _, ok := m.(*SMTPMailer); !ok {
		t.Fatalf("NewMailer() = %T, want *SMTPMailer", m)
	}
}

func TestBuildVerifyEmailLinkRelativeWhenBaseUnset(t *testing.T) {
	got := buildVerifyEmailLink("", "abc123")
	want := "/verify-email?token=abc123"
	if got != want {
		t.Fatalf("buildVerifyEmailLink() = %q, want %q", got, want)
	}
}

func TestBuildVerifyEmailLinkAbsoluteWhenBaseSet(t *testing.T) {
	got := buildVerifyEmailLink("https://app.example.com/", "abc+def/ghi")
	want := "https://app.example.com/verify-email?token=abc%2Bdef%2Fghi"
	if got != want {
		t.Fatalf("buildVerifyEmailLink() = %q, want %q", got, want)
	}
}
