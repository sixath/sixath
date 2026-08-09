package browser

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"
)

// skipIfNoChrome skips when neither a usable local Chrome nor BROWSER_CDP_URL is available.
func skipIfNoChrome(t *testing.T) Backend {
	t.Helper()
	b, err := NewChromedpBackend(context.Background())
	if err != nil {
		if os.Getenv("BROWSER_CDP_URL") == "" {
			t.Skipf("no Chrome / no BROWSER_CDP_URL: %v", err)
		}
		t.Skipf("BROWSER_CDP_URL set but CDP unavailable: %v", err)
	}
	t.Cleanup(func() {
		_ = b.Close(context.Background())
	})
	return b
}

func TestChromedpBackend_Healthy(t *testing.T) {
	b := skipIfNoChrome(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := b.Healthy(ctx); err != nil {
		t.Fatalf("Healthy: %v", err)
	}
}

func TestChromedpBackend_Navigate_exampleCom(t *testing.T) {
	b := skipIfNoChrome(t)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	snap, err := b.Navigate(ctx, "https://example.com")
	if err != nil {
		t.Fatalf("Navigate: %v", err)
	}
	if !strings.Contains(snap.URL, "example.com") {
		t.Fatalf("URL = %q, want example.com", snap.URL)
	}
	if snap.Title == "" && snap.Text == "" {
		t.Fatal("expected non-empty title or text from example.com")
	}

	snap2, err := b.Snapshot(ctx, false)
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if snap2.Refs == nil {
		t.Fatal("expected Refs map (may be empty)")
	}
}
