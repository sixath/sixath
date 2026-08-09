package tool

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// VisionAnalyzer analyzes an image (PNG/JPEG bytes) with an optional question.
// Implementations typically call a multimodal LLM with a data: URL.
type VisionAnalyzer interface {
	Analyze(ctx context.Context, imageBytes []byte, mimeType, question string) (string, error)
}

// VisionAnalyzeFunc adapts a function to VisionAnalyzer.
type VisionAnalyzeFunc func(ctx context.Context, imageBytes []byte, mimeType, question string) (string, error)

// Analyze implements VisionAnalyzer.
func (f VisionAnalyzeFunc) Analyze(ctx context.Context, imageBytes []byte, mimeType, question string) (string, error) {
	if f == nil {
		return "", errors.New("vision analyzer is nil")
	}
	return f(ctx, imageBytes, mimeType, question)
}

// ImageDataURL builds a data: URL for multimodal chat APIs.
func ImageDataURL(imageBytes []byte, mimeType string) string {
	if strings.TrimSpace(mimeType) == "" {
		mimeType = "image/png"
	}
	return "data:" + mimeType + ";base64," + base64.StdEncoding.EncodeToString(imageBytes)
}

// DefaultVisionQuestion is used when the caller omits a question.
const DefaultVisionQuestion = "Describe this screenshot briefly. Note any errors, dialogs, or key UI text."

// RegisterVisionAnalyzeTool registers Hermes-aligned vision_analyze (workspace image path).
func RegisterVisionAnalyzeTool(reg *Registry, analyzer VisionAnalyzer) error {
	if reg == nil {
		return errors.New("vision_analyze: registry is nil")
	}
	if analyzer == nil {
		return errors.New("vision_analyze: analyzer is nil")
	}
	return reg.Register(Tool{
		Name: "vision_analyze",
		Description: "Analyze an image file under the workspace with a vision LLM. " +
			"Prefer browser_vision(question=...) for live page screenshots.",
		Toolset: ToolsetBrowser,
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path": map[string]any{
					"type":        "string",
					"description": "Workspace-relative image path (png/jpeg/webp).",
				},
				"question": map[string]any{
					"type":        "string",
					"description": "What to look for / ask about the image.",
				},
			},
			"required": []string{"path"},
		},
		Execute: func(ctx context.Context, params map[string]any) (any, error) {
			rel, _ := params["path"].(string)
			rel = strings.TrimSpace(rel)
			if rel == "" {
				return map[string]any{"ok": false, "error": "path is required", "error_code": ErrorPermanent}, nil
			}
			question, _ := params["question"].(string)
			ws, _ := ctx.Value(ContextKeyWorkspaceRoot).(string)
			abs, err := ResolveWorkspacePath(ws, rel)
			if err != nil {
				return map[string]any{"ok": false, "error": err.Error(), "error_code": ErrorPermanent}, nil
			}
			raw, err := os.ReadFile(abs)
			if err != nil {
				return map[string]any{"ok": false, "error": err.Error(), "error_code": ErrorTransient}, nil
			}
			mime := mimeFromImagePath(abs)
			q := strings.TrimSpace(question)
			if q == "" {
				q = DefaultVisionQuestion
			}
			analysis, err := analyzer.Analyze(ctx, raw, mime, q)
			if err != nil {
				return map[string]any{
					"ok":         false,
					"error":      err.Error(),
					"error_code": ErrorTransient,
					"path":       rel,
				}, nil
			}
			return map[string]any{
				"ok":       true,
				"path":     rel,
				"question": q,
				"analysis": analysis,
			}, nil
		},
	})
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

func analyzeScreenshot(ctx context.Context, analyzer VisionAnalyzer, png []byte, question string) (string, error) {
	if analyzer == nil {
		return "", fmt.Errorf("vision analyzer not configured")
	}
	q := strings.TrimSpace(question)
	if q == "" {
		q = DefaultVisionQuestion
	}
	return analyzer.Analyze(ctx, png, "image/png", q)
}
