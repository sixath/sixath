package service

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"

	"backend/internal/biz"
	"backend/internal/chat"

	"github.com/google/uuid"
	agent "github.com/sixath/framework/harness"
)

// maxToolImagesPerTurn caps how many tool screenshots one turn may attach to the reply.
const maxToolImagesPerTurn = 8

// savedScreenshotRE matches chrome-devtools-mcp take_screenshot output for large or filePath screenshots.
var savedScreenshotRE = regexp.MustCompile(`(?i)saved screenshot to\s+(.+?\.(?:png|jpe?g|webp))`)

type toolImage struct {
	Name string
	Data []byte // inline image bytes (MCP image content)
	Path string // absolute file path on the portal host
}

// toolImageCapturer turns screenshots produced by tools in one turn into session attachments.
type toolImageCapturer struct {
	s         *ChatService
	sessionID string
	workspace string
	seen      map[[32]byte]struct{}
	count     int
}

func (s *ChatService) newToolImageCapturer(sessionID, workspace string) *toolImageCapturer {
	if s == nil || s.chatUC == nil || strings.TrimSpace(workspace) == "" || strings.TrimSpace(sessionID) == "" {
		return nil
	}
	return &toolImageCapturer{s: s, sessionID: sessionID, workspace: workspace, seen: map[[32]byte]struct{}{}}
}

// Capture stores images found in a completed tool call and returns their attachment metadata.
// Failures are logged and skipped: rendering screenshots must never break the turn.
func (c *toolImageCapturer) Capture(ctx context.Context, rec agent.ToolCallRecord) []*AttachmentUploadReply {
	if c == nil || rec.Error != "" || c.count >= maxToolImagesPerTurn {
		return nil
	}
	var out []*AttachmentUploadReply
	for _, img := range extractToolImages(rec.Result, c.workspace) {
		if c.count >= maxToolImagesPerTurn {
			break
		}
		data := img.Data
		if len(data) == 0 && img.Path != "" {
			b, err := readToolImageFile(img.Path, c.workspace)
			if err != nil {
				c.s.log.Warnf("tool image skipped: session_id=%s tool=%s path=%s err=%v", c.sessionID, rec.ToolName, img.Path, err)
				continue
			}
			data = b
		}
		if !isImageBytes(data) || len(data) > chat.MaxImageAttachmentBytes {
			continue
		}
		sum := sha256.Sum256(data)
		if _, dup := c.seen[sum]; dup {
			continue
		}
		att, err := c.s.storeToolImage(ctx, c.sessionID, c.workspace, img.Name, data)
		if err != nil {
			c.s.log.Warnf("tool image store failed: session_id=%s tool=%s err=%v", c.sessionID, rec.ToolName, err)
			continue
		}
		c.seen[sum] = struct{}{}
		c.count++
		out = append(out, att)
	}
	return out
}

func (s *ChatService) storeToolImage(ctx context.Context, sessionID, workspace, name string, data []byte) (*AttachmentUploadReply, error) {
	safeName := chat.SafeFilename(name)
	if safeName == "" {
		safeName = "screenshot.png"
	}
	kind, ok := chat.ClassifyAttachment(safeName, "")
	if !ok || kind != chat.KindImage {
		safeName = "screenshot.png"
	}
	mime := chat.DefaultMIMEForKind(chat.KindImage, safeName)
	attID := uuid.NewString()
	rel := path.Join("sessions", sessionID, biz.ToolImageDir, attID+"_"+safeName)
	abs := filepath.Join(workspace, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		return nil, fmt.Errorf("mkdir uploads: %w", err)
	}
	if err := os.WriteFile(abs, data, 0o644); err != nil {
		return nil, fmt.Errorf("write tool image: %w", err)
	}
	row := &biz.ChatAttachment{
		ID:           attID,
		SessionID:    sessionID,
		Kind:         string(chat.KindImage),
		Mime:         mime,
		Name:         safeName,
		Size:         int64(len(data)),
		RelativePath: rel,
	}
	if err := s.chatUC.CreateAttachment(ctx, row); err != nil {
		_ = os.Remove(abs)
		return nil, err
	}
	return &AttachmentUploadReply{
		ID:           attID,
		Kind:         row.Kind,
		Mime:         mime,
		Name:         safeName,
		Size:         row.Size,
		RelativePath: rel,
	}, nil
}

// extractToolImages finds screenshots in a tool result: native browser_vision
// (screenshot_path), MCP inline image content, and "Saved screenshot to <path>" text.
func extractToolImages(result any, workspace string) []toolImage {
	var out []toolImage
	var walk func(v any)
	walk = func(v any) {
		switch t := v.(type) {
		case string:
			if decoded, ok := decodeJSONText(t); ok {
				walk(decoded)
				return
			}
			for _, line := range strings.Split(t, "\n") {
				if decoded, ok := decodeJSONText(line); ok {
					walk(decoded)
				}
			}
			for _, m := range savedScreenshotRE.FindAllStringSubmatch(t, -1) {
				p := strings.TrimSpace(m[1])
				out = append(out, toolImage{Name: filepath.Base(p), Path: p})
			}
		case map[string]any:
			if typ, _ := t["type"].(string); typ == "image" {
				if data, _ := t["data"].(string); data != "" {
					if raw, err := base64.StdEncoding.DecodeString(data); err == nil {
						out = append(out, toolImage{Name: "screenshot" + extForMime(t["mimeType"]), Data: raw})
					}
					return
				}
			}
			if rel, _ := t["screenshot_path"].(string); strings.TrimSpace(rel) != "" && workspace != "" {
				p := filepath.Join(workspace, filepath.FromSlash(rel))
				out = append(out, toolImage{Name: filepath.Base(p), Path: p})
			}
			for k, child := range t {
				if k == "screenshot_path" {
					continue
				}
				walk(child)
			}
		case []any:
			for _, child := range t {
				walk(child)
			}
		default:
			if v == nil {
				return
			}
			b, err := json.Marshal(v)
			if err != nil {
				return
			}
			var generic any
			if json.Unmarshal(b, &generic) != nil {
				return
			}
			switch generic.(type) {
			case map[string]any, []any:
				walk(generic)
			}
		}
	}
	walk(result)
	return out
}

func decodeJSONText(s string) (any, bool) {
	s = strings.TrimSpace(s)
	if s == "" || (s[0] != '{' && s[0] != '[') {
		return nil, false
	}
	var v any
	if err := json.Unmarshal([]byte(s), &v); err != nil {
		return nil, false
	}
	return v, true
}

func extForMime(v any) string {
	m, _ := v.(string)
	switch strings.ToLower(strings.TrimSpace(m)) {
	case "image/jpeg", "image/jpg":
		return ".jpg"
	case "image/webp":
		return ".webp"
	default:
		return ".png"
	}
}

// readToolImageFile only reads image files under the agent workspace or the OS temp dir,
// so a tool result cannot be used to exfiltrate arbitrary host files.
func readToolImageFile(p, workspace string) ([]byte, error) {
	if !filepath.IsAbs(p) {
		return nil, fmt.Errorf("not an absolute path")
	}
	if !pathUnderWorkspace(workspace, p) && !pathUnderWorkspace(os.TempDir(), p) {
		return nil, fmt.Errorf("path outside workspace and temp dir")
	}
	if kind, ok := chat.ClassifyAttachment(filepath.Base(p), ""); !ok || kind != chat.KindImage {
		return nil, fmt.Errorf("not an image file")
	}
	st, err := os.Stat(p)
	if err != nil {
		return nil, err
	}
	if st.IsDir() || st.Size() > chat.MaxImageAttachmentBytes {
		return nil, fmt.Errorf("invalid image file")
	}
	return os.ReadFile(p)
}

func isImageBytes(data []byte) bool {
	if len(data) == 0 {
		return false
	}
	switch http.DetectContentType(data) {
	case "image/png", "image/jpeg", "image/webp", "image/gif":
		return true
	default:
		return false
	}
}
