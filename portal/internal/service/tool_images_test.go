package service

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

var tinyPNG = []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n', 0, 0, 0, 0x0d, 'I', 'H', 'D', 'R'}

func TestExtractToolImages_MCPInlineImage(t *testing.T) {
	resp := map[string]any{
		"content": []any{
			map[string]any{"type": "text", "text": "Took a screenshot of the current page."},
			map[string]any{"type": "image", "mimeType": "image/png", "data": base64.StdEncoding.EncodeToString(tinyPNG)},
		},
	}
	b, _ := json.Marshal(resp)
	got := extractToolImages(string(b), "")
	if len(got) != 1 || string(got[0].Data) != string(tinyPNG) || got[0].Name != "screenshot.png" {
		t.Fatalf("got %#v", got)
	}
}

func TestExtractToolImages_MCPSavedToTempFile(t *testing.T) {
	p := filepath.Join(os.TempDir(), "chrome-devtools-mcp-test", "screenshot.png")
	resp := map[string]any{
		"content": []any{
			map[string]any{"type": "text", "text": "# take_screenshot response\nSaved screenshot to " + p + "."},
		},
	}
	b, _ := json.Marshal(resp)
	got := extractToolImages(string(b), "")
	if len(got) != 1 || got[0].Path != p {
		t.Fatalf("got %#v", got)
	}
}

func TestExtractToolImages_BrowserVisionPath(t *testing.T) {
	ws := t.TempDir()
	got := extractToolImages(map[string]any{"ok": true, "screenshot_path": ".browser/screenshots/a.png"}, ws)
	want := filepath.Join(ws, ".browser", "screenshots", "a.png")
	if len(got) != 1 || got[0].Path != want {
		t.Fatalf("got %#v want %s", got, want)
	}
}

func TestExtractToolImages_OffloadedImageInMultilineText(t *testing.T) {
	ws := t.TempDir()
	text := "Took a screenshot.\n" + `{"type":"image","mimeType":"image/png","screenshot_path":".browser/screenshots/x.png"}`
	got := extractToolImages(text, ws)
	want := filepath.Join(ws, ".browser", "screenshots", "x.png")
	if len(got) != 1 || got[0].Path != want {
		t.Fatalf("got %#v want %s", got, want)
	}
}

func TestExtractToolImages_PlainTextIgnored(t *testing.T) {
	if got := extractToolImages("page loaded, 3 results", ""); len(got) != 0 {
		t.Fatalf("got %#v", got)
	}
}

func TestReadToolImageFile_RejectsOutsideRoots(t *testing.T) {
	ws := t.TempDir()
	if _, err := readToolImageFile(`C:\Windows\win.ini`, ws); err == nil {
		t.Fatal("expected rejection for file outside workspace/temp")
	}
	inside := filepath.Join(ws, "shot.png")
	if err := os.WriteFile(inside, tinyPNG, 0o644); err != nil {
		t.Fatal(err)
	}
	b, err := readToolImageFile(inside, ws)
	if err != nil || !isImageBytes(b) {
		t.Fatalf("err=%v image=%v", err, isImageBytes(b))
	}
	notImage := filepath.Join(ws, "notes.txt")
	_ = os.WriteFile(notImage, []byte("x"), 0o644)
	if _, err := readToolImageFile(notImage, ws); err == nil {
		t.Fatal("expected rejection for non-image extension")
	}
}
