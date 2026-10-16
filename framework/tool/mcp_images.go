package tool

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"

	fwws "github.com/sixath/framework/workspace"
)

// offloadMCPImages rewrites inline image content in an MCP CallToolResult JSON so the
// base64 payload never reaches the model as text (a single screenshot is ~100k tokens).
// Returns raw unchanged when it has no image content or is not a JSON object.
func offloadMCPImages(ctx context.Context, raw []byte) []byte {
	var obj map[string]any
	if json.Unmarshal(raw, &obj) != nil {
		return raw
	}
	items, ok := obj["content"].([]any)
	if !ok {
		return raw
	}
	changed := false
	for i, item := range items {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if typ, _ := m["type"].(string); typ != "image" {
			continue
		}
		data, _ := m["data"].(string)
		if data == "" {
			continue
		}
		mime, _ := m["mimeType"].(string)
		items[i] = offloadMCPImage(ctx, mime, data)
		changed = true
	}
	if !changed {
		return raw
	}
	out, err := json.Marshal(obj)
	if err != nil {
		return raw
	}
	return out
}

// offloadMCPImage saves one base64 image under the workspace and returns a content item that
// references it by screenshot_path; without a workspace the image is dropped with a size note.
func offloadMCPImage(ctx context.Context, mime, b64 string) map[string]any {
	out := map[string]any{"type": "image", "mimeType": mime}
	img, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		out["omitted"] = true
		return out
	}
	out["bytes"] = len(img)
	ws, _ := ctx.Value(ContextKeyWorkspaceRoot).(string)
	rel, err := saveMCPImage(ws, mime, img)
	if err != nil {
		out["omitted"] = true
		return out
	}
	out["screenshot_path"] = rel
	out["note"] = "image saved to workspace; use vision_analyze(path) to inspect it"
	return out
}

func saveMCPImage(workspace, mime string, img []byte) (string, error) {
	workspace = strings.TrimSpace(workspace)
	if workspace == "" {
		return "", os.ErrNotExist
	}
	var suffix [4]byte
	_, _ = rand.Read(suffix[:])
	name := time.Now().Format("20060102-150405.000") + "-" + hex.EncodeToString(suffix[:]) + imageExtForMime(mime)
	rel := filepath.ToSlash(filepath.Join(".browser", "screenshots", name))
	full, err := fwws.ResolveWorkspacePath(workspace, rel)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		return "", err
	}
	if err := os.WriteFile(full, img, 0o644); err != nil {
		return "", err
	}
	return rel, nil
}

func imageExtForMime(mime string) string {
	switch strings.ToLower(strings.TrimSpace(mime)) {
	case "image/jpeg", "image/jpg":
		return ".jpg"
	case "image/webp":
		return ".webp"
	case "image/gif":
		return ".gif"
	default:
		return ".png"
	}
}
