package local

import (
	"fmt"
	"path/filepath"
	"strings"
)

const wikiDraftSuffix = ".draft.md"

func IsWikiDraftFile(name string) bool {
	return strings.HasSuffix(strings.ToLower(name), wikiDraftSuffix)
}

// CanonicalWikiID maps *.draft.md → *.md; ensures .md for bare names.
// Returns error for empty, path escape (..), or non-.md formal extensions.
func CanonicalWikiID(id string) (string, error) {
	id = strings.TrimSpace(strings.ReplaceAll(id, "\\", "/"))
	if id == "" {
		return "", fmt.Errorf("hub/local: empty wiki id")
	}
	if strings.Contains(id, "..") {
		return "", fmt.Errorf("hub/local: invalid wiki id")
	}
	lower := strings.ToLower(id)
	if strings.HasSuffix(lower, wikiDraftSuffix) {
		id = id[:len(id)-len(wikiDraftSuffix)] + ".md"
	}
	ext := filepath.Ext(id)
	if ext == "" {
		return id + ".md", nil
	}
	if strings.ToLower(ext) != ".md" {
		return "", fmt.Errorf("hub/local: wiki id must use .md extension")
	}
	return id, nil
}

// DraftPathForWikiID returns the draft file path for a canonical formal wiki id (*.md).
func DraftPathForWikiID(canonical string) string {
	return strings.TrimSuffix(canonical, ".md") + wikiDraftSuffix
}
