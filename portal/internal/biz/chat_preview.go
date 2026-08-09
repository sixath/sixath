package biz

import "strings"

const sessionPreviewMaxRunes = 120

func TruncatePreview(s string) string {
	return truncatePreview(s, sessionPreviewMaxRunes)
}

func truncatePreview(s string, max int) string {
	r := []rune(strings.TrimSpace(s))
	if len(r) <= max {
		return string(r)
	}
	return string(r[:max]) + "…"
}
