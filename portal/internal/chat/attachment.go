package chat

import (
	"encoding/base64"
	"path/filepath"
	"strings"

	"github.com/sixath/framework/model"
)

const (
	MaxImageAttachmentBytes = 10 * 1024 * 1024 // 10MB
	MaxTextAttachmentBytes  = 2 * 1024 * 1024  // 2MB
	MaxAttachmentsPerSession = 20
)

type AttachmentKind string

const (
	KindImage AttachmentKind = "image"
	KindText  AttachmentKind = "text"
)

type ContentBuildMode int

const (
	ContentForPersist ContentBuildMode = iota
	ContentForModelWithImageParts
)

type AttachmentMeta struct {
	Kind         AttachmentKind
	RelativePath string
	Mime         string
}

var (
	imageExtensions = map[string]struct{}{
		".png":  {},
		".jpg":  {},
		".jpeg": {},
		".webp": {},
		".gif":  {},
	}
	textExtensions = map[string]struct{}{
		".txt":  {},
		".log":  {},
		".md":   {},
		".json": {},
		".csv":  {},
	}
)

// ClassifyAttachment classifies attachments for upload validation.
// Whitelisted extension always wins (even when MIME disagrees); when extension
// is absent, MIME is used as fallback (exact image whitelist, text/plain,
// application/json). Non-whitelisted extensions such as .pdf are rejected
// without MIME fallback. Arbitrary image/* (e.g. image/svg+xml) is rejected.
func ClassifyAttachment(filename, mime string) (AttachmentKind, bool) {
	ext := strings.ToLower(filepath.Ext(filename))
	if ext != "" {
		if _, ok := imageExtensions[ext]; ok {
			return KindImage, true
		}
		if _, ok := textExtensions[ext]; ok {
			return KindText, true
		}
		return "", false
	}
	return classifyAttachmentByMIME(mime)
}

func classifyAttachmentByMIME(mime string) (AttachmentKind, bool) {
	m := strings.ToLower(strings.TrimSpace(mime))
	// Strip optional parameters: "image/png; charset=utf-8"
	if i := strings.IndexByte(m, ';'); i >= 0 {
		m = strings.TrimSpace(m[:i])
	}
	switch m {
	case "image/png", "image/jpeg", "image/jpg", "image/webp", "image/gif":
		return KindImage, true
	case "text/plain", "application/json":
		return KindText, true
	default:
		return "", false
	}
}

// DefaultMIMEForKind returns a server-derived Content-Type from kind + filename
// extension. Never trust client-supplied MIME for storage or responses.
func DefaultMIMEForKind(kind AttachmentKind, name string) string {
	ext := strings.ToLower(filepath.Ext(name))
	switch kind {
	case KindImage:
		switch ext {
		case ".jpg", ".jpeg":
			return "image/jpeg"
		case ".webp":
			return "image/webp"
		case ".gif":
			return "image/gif"
		default:
			return "image/png"
		}
	case KindText:
		switch ext {
		case ".json":
			return "application/json"
		case ".csv":
			return "text/csv"
		case ".md":
			return "text/markdown"
		default:
			return "text/plain"
		}
	default:
		return "application/octet-stream"
	}
}

// SafeFilename returns a basename safe for storage; empty when invalid.
func SafeFilename(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	base := filepath.Base(name)
	base = strings.TrimSpace(base)
	if base == "" || base == "." || base == ".." {
		return ""
	}
	if strings.Contains(base, "..") {
		return ""
	}
	return base
}

// BuildUserContentWithAttachments joins user text with attachment footnotes.
func BuildUserContentWithAttachments(content string, metas []AttachmentMeta, mode ContentBuildMode) string {
	var footnotes []string
	for _, meta := range metas {
		switch meta.Kind {
		case KindImage:
			if mode == ContentForModelWithImageParts {
				continue
			}
			footnotes = append(footnotes, "〔附件 image〕"+meta.RelativePath)
		case KindText:
			footnotes = append(footnotes, "〔附件 text〕"+meta.RelativePath+" — 可用 read_file 读取")
		}
	}
	if len(footnotes) == 0 {
		return content
	}
	joined := strings.Join(footnotes, "\n")
	if strings.TrimSpace(content) == "" {
		return joined
	}
	return content + "\n\n" + joined
}

// BuildImageParts reads image attachments and returns multimodal content parts.
// Failed or empty reads are skipped (no error returned); callers detect vision
// degrade by comparing image meta count to len(parts).
func BuildImageParts(metas []AttachmentMeta, readFile func(path string) ([]byte, error)) []model.ContentPart {
	if readFile == nil {
		return nil
	}
	var parts []model.ContentPart
	for _, meta := range metas {
		if meta.Kind != KindImage {
			continue
		}
		data, err := readFile(meta.RelativePath)
		if err != nil || len(data) == 0 {
			continue
		}
		mime := strings.TrimSpace(meta.Mime)
		if mime == "" {
			mime = mimeFromImagePath(meta.RelativePath)
		}
		parts = append(parts, model.ContentPart{
			Type: model.ContentTypeImageURL,
			URL:  "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(data),
		})
	}
	return parts
}

func mimeFromImagePath(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".webp":
		return "image/webp"
	case ".gif":
		return "image/gif"
	default:
		return "image/png"
	}
}

// UniqueNonEmpty deduplicates attachment ids, dropping blanks. Order of first
// occurrence is preserved.
func UniqueNonEmpty(ids []string) []string {
	if len(ids) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(ids))
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}

// StripImageParts returns a copy of m with Parts cleared so historical turns
// never re-inject multimodal image payloads.
func StripImageParts(m model.Message) model.Message {
	if len(m.Parts) == 0 {
		return m
	}
	out := m
	out.Parts = nil
	return out
}

// AssembleTurnMessages builds the model message list for one ReAct turn:
// history rows keep DB text (footnotes) with Parts stripped; only currentUser
// may carry image Parts.
func AssembleTurnMessages(history []model.Message, currentUser model.Message) []model.Message {
	out := make([]model.Message, 0, len(history)+1)
	for _, m := range history {
		out = append(out, StripImageParts(m))
	}
	out = append(out, currentUser)
	return out
}

// CountImageMetas returns how many attachment metas are images.
func CountImageMetas(metas []AttachmentMeta) int {
	n := 0
	for _, m := range metas {
		if m.Kind == KindImage {
			n++
		}
	}
	return n
}
