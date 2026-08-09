package reply_test

import (
	"strings"
	"testing"

	"github.com/sixath/gateway/internal/reply"
)

func TestValidateReplyURL_EmptyOK(t *testing.T) {
	if err := reply.ValidateReplyURL(""); err != nil {
		t.Fatalf("empty: %v", err)
	}
}

func TestValidateReplyURL_RejectNonHTTP(t *testing.T) {
	for _, raw := range []string{
		"ftp://example.com/cb",
		"file:///etc/passwd",
		"gopher://127.0.0.1/",
		"javascript:alert(1)",
	} {
		if err := reply.ValidateReplyURL(raw); err == nil {
			t.Fatalf("expected reject for %q", raw)
		}
	}
}

func TestValidateReplyURL_RejectPrivateAndLinkLocal(t *testing.T) {
	t.Setenv("GATEWAY_ALLOW_LOOPBACK_REPLY", "")
	for _, raw := range []string{
		"http://10.0.0.1/cb",
		"http://192.168.1.1/cb",
		"http://172.16.0.5/cb",
		"http://169.254.169.254/latest/meta-data/",
		"http://[fe80::1]/cb",
		"http://[fd00::1]/cb",
	} {
		if err := reply.ValidateReplyURL(raw); err == nil {
			t.Fatalf("expected reject for %q", raw)
		}
	}
}

func TestValidateReplyURL_RejectLoopbackByDefault(t *testing.T) {
	t.Setenv("GATEWAY_ALLOW_LOOPBACK_REPLY", "0")
	for _, raw := range []string{
		"http://127.0.0.1:9/cb",
		"http://localhost:9/cb",
		"http://[::1]/cb",
	} {
		if err := reply.ValidateReplyURL(raw); err == nil {
			t.Fatalf("expected reject for %q", raw)
		}
	}
}

func TestValidateReplyURL_AllowLoopbackWhenEnvSet(t *testing.T) {
	t.Setenv("GATEWAY_ALLOW_LOOPBACK_REPLY", "1")
	for _, raw := range []string{
		"http://127.0.0.1:9/cb",
		"http://localhost:9/cb",
		"http://[::1]:9/cb",
	} {
		if err := reply.ValidateReplyURL(raw); err != nil {
			t.Fatalf("allow loopback %q: %v", raw, err)
		}
	}
	// Private still blocked even with loopback allow.
	if err := reply.ValidateReplyURL("http://10.1.2.3/cb"); err == nil {
		t.Fatal("expected private IP still rejected")
	}
}

func TestValidateReplyURL_AllowPublicHTTP(t *testing.T) {
	t.Setenv("GATEWAY_ALLOW_LOOPBACK_REPLY", "")
	// Use an IP that is not private/loopback so we avoid DNS flakiness.
	if err := reply.ValidateReplyURL("https://8.8.8.8/callback"); err != nil {
		t.Fatalf("public IP: %v", err)
	}
}

func TestPostReplyURL_RejectsSSRF(t *testing.T) {
	t.Setenv("GATEWAY_ALLOW_LOOPBACK_REPLY", "")
	d := reply.NewDispatcher(nil)
	err := d.PostReplyURL(t.Context(), "http://127.0.0.1/cb", reply.FinalPayload{
		CorrelationID: "c1",
		Status:        "ok",
	})
	if err == nil {
		t.Fatal("expected SSRF error")
	}
	if !strings.Contains(err.Error(), "not allowed") && !strings.Contains(err.Error(), "scheme") {
		t.Fatalf("unexpected err: %v", err)
	}
}
