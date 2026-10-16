package tool

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOffloadMCPImages_SavesToWorkspace(t *testing.T) {
	ws := t.TempDir()
	img := []byte("\x89PNG\r\n\x1a\nfake")
	raw, _ := json.Marshal(map[string]any{
		"content": []any{
			map[string]any{"type": "text", "text": "Took a screenshot."},
			map[string]any{"type": "image", "mimeType": "image/png", "data": base64.StdEncoding.EncodeToString(img)},
		},
	})
	ctx := context.WithValue(context.Background(), ContextKeyWorkspaceRoot, ws)
	out := offloadMCPImages(ctx, raw)
	if strings.Contains(string(out), base64.StdEncoding.EncodeToString(img)) {
		t.Fatalf("base64 still present: %s", out)
	}
	var obj struct {
		Content []map[string]any `json:"content"`
	}
	if err := json.Unmarshal(out, &obj); err != nil {
		t.Fatal(err)
	}
	rel, _ := obj.Content[1]["screenshot_path"].(string)
	if rel == "" || !strings.HasSuffix(rel, ".png") {
		t.Fatalf("missing screenshot_path: %s", out)
	}
	got, err := os.ReadFile(filepath.Join(ws, filepath.FromSlash(rel)))
	if err != nil || string(got) != string(img) {
		t.Fatalf("saved file mismatch err=%v", err)
	}
	if obj.Content[0]["text"] != "Took a screenshot." {
		t.Fatalf("text content changed: %s", out)
	}
}

func TestOffloadMCPImages_NoWorkspaceOmits(t *testing.T) {
	raw, _ := json.Marshal(map[string]any{
		"content": []any{map[string]any{"type": "image", "mimeType": "image/png", "data": base64.StdEncoding.EncodeToString([]byte("abc"))}},
	})
	out := offloadMCPImages(context.Background(), raw)
	if strings.Contains(string(out), `"data"`) || !strings.Contains(string(out), `"omitted":true`) {
		t.Fatalf("got %s", out)
	}
}

func TestOffloadMCPImages_TextOnlyUnchanged(t *testing.T) {
	raw := []byte(`{"content":[{"type":"text","text":"hi"}]}`)
	if out := offloadMCPImages(context.Background(), raw); string(out) != string(raw) {
		t.Fatalf("got %s", out)
	}
}
