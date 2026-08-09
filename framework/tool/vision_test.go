package tool

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sixath/framework/tool/browser"
)

func TestBrowserVision_WithAnalyzer(t *testing.T) {
	root := t.TempDir()
	reg := NewRegistry()
	fb := browser.NewFakeBackend()
	fb.ScreenshotPNG = []byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a}
	analyzer := VisionAnalyzeFunc(func(_ context.Context, imageBytes []byte, mimeType, question string) (string, error) {
		if len(imageBytes) == 0 {
			t.Fatal("empty image")
		}
		if mimeType != "image/png" {
			t.Fatalf("mime=%s", mimeType)
		}
		if question != "what is this?" && question != "again?" {
			t.Fatalf("question=%q", question)
		}
		return "a blank page", nil
	})
	if err := RegisterBrowserToolsWithConfig(reg, browser.NewSessionStore(), func() (browser.Backend, error) {
		return fb, nil
	}, &BrowserToolsConfig{Vision: analyzer}); err != nil {
		t.Fatal(err)
	}
	if _, ok := reg.Get("vision_analyze"); !ok {
		t.Fatal("vision_analyze should be registered")
	}
	ctx := context.WithValue(context.Background(), ContextKeySessionID, "s")
	ctx = context.WithValue(ctx, ContextKeyWorkspaceRoot, root)

	vis, _ := reg.Get("browser_vision")
	out, err := vis.Execute(ctx, map[string]any{"question": "what is this?"})
	if err != nil {
		t.Fatal(err)
	}
	m := out.(map[string]any)
	if m["analysis"] != "a blank page" {
		t.Fatalf("%#v", m)
	}
	rel, _ := m["screenshot_path"].(string)
	abs := filepath.Join(root, filepath.FromSlash(rel))
	if _, err := os.Stat(abs); err != nil {
		t.Fatal(err)
	}

	va, _ := reg.Get("vision_analyze")
	vout, err := va.Execute(ctx, map[string]any{"path": rel, "question": "again?"})
	if err != nil {
		t.Fatal(err)
	}
	vm := vout.(map[string]any)
	if vm["ok"] != true || vm["analysis"] == nil {
		t.Fatalf("%#v", vm)
	}
}

func TestImageDataURL(t *testing.T) {
	u := ImageDataURL([]byte{1, 2, 3}, "image/png")
	if !strings.HasPrefix(u, "data:image/png;base64,") {
		t.Fatalf("%s", u)
	}
}
