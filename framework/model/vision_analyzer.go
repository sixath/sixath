package model

import (
	"context"
	"errors"
	"strings"

	"github.com/sixath/framework/tool"
)

// NewVisionAnalyzer wraps a multimodal Model as tool.VisionAnalyzer (data-URL images).
func NewVisionAnalyzer(m Model, opts ...Option) tool.VisionAnalyzer {
	if m == nil {
		return nil
	}
	return tool.VisionAnalyzeFunc(func(ctx context.Context, imageBytes []byte, mimeType, question string) (string, error) {
		if len(imageBytes) == 0 {
			return "", errors.New("image is empty")
		}
		q := strings.TrimSpace(question)
		if q == "" {
			q = tool.DefaultVisionQuestion
		}
		url := tool.ImageDataURL(imageBytes, mimeType)
		gen, err := m.Chat(ctx, []Message{{
			Role: "user",
			Parts: []ContentPart{
				{Type: ContentTypeText, Text: q},
				{Type: ContentTypeImageURL, URL: url},
			},
		}}, opts...)
		if err != nil {
			msg := err.Error()
			low := strings.ToLower(msg)
			if strings.Contains(low, "modality") || strings.Contains(low, "image_url") || strings.Contains(low, "vision") {
				return "", errors.New("vision model rejected image input (set SATH_VISION_MODEL to a vision-capable model): " + msg)
			}
			return "", err
		}
		if gen == nil {
			return "", errors.New("empty vision generation")
		}
		text := strings.TrimSpace(gen.Text)
		if text == "" {
			return "", errors.New("empty vision response")
		}
		return text, nil
	})
}
