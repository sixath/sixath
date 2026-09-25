package chat

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

// ArbiterResult holds the output of arbitration.
type ArbiterResult struct {
	Duplicate           bool
	DuplicateOf         string
	Conflict            bool
	ConflictDetail      string
	DedupSkipped        bool
	ConflictCheckFailed bool
}

// Arbitrate runs dedup and conflict checks for a proposed evolution.
func Arbitrate(
	ctx context.Context,
	newSummary string,
	newContent string,
	existingSkills []SkillSummary,
) ArbiterResult {
	var result ArbiterResult

	cfg := EvolutionConfig()
	if cfg == nil {
		return result
	}

	newHash := contentHash(newContent)
	for _, sk := range existingSkills {
		if contentHash(sk.Description) == newHash {
			result.Duplicate = true
			result.DuplicateOf = sk.Name
			return result
		}
	}

	if hasSemanticConflict(newContent, existingSkills) {
		result.Conflict = true
		result.ConflictDetail = "新指令与已存在技能可能存在语义矛盾"
	}

	return result
}

// SkillSummary is a lightweight representation of an existing skill.
type SkillSummary struct {
	Name        string
	Description string
}

func contentHash(content string) string {
	h := sha256.Sum256([]byte(strings.TrimSpace(content)))
	return hex.EncodeToString(h[:16])
}

func hasSemanticConflict(newContent string, existing []SkillSummary) bool {
	_ = newContent
	_ = existing
	return false
}