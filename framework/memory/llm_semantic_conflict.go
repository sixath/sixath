package memory

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/sixath/framework/model"
)

const llmSemanticConflictSystem = `You judge if a new memory fact conflicts with existing facts.
Reply with ONLY valid JSON (no markdown fences):
{"decision":"ignore"|"supersede"|"keep_both","target_unit_id":""}
Rules:
- ignore: new fact is noise or already covered
- supersede: new fact updates/replaces one existing fact; set target_unit_id to that peer id
- keep_both: related but both can remain
- target_unit_id required for supersede and must be one of the peer ids
- if unsure whether they conflict but both useful, keep_both`

// LLMSemanticConflictResolver uses a chat model to judge add conflicts as JSON.
type LLMSemanticConflictResolver struct {
	Model model.Model
}

type llmSemanticConflictResponse struct {
	Decision     string `json:"decision"`
	TargetUnitID string `json:"target_unit_id"`
}

// ResolveAdd implements SemanticConflictResolver.
func (r *LLMSemanticConflictResolver) ResolveAdd(ctx context.Context, candidate RememberInput, peers []MemoryHit) (SemanticConflictVerdict, error) {
	if r == nil || r.Model == nil {
		return SemanticConflictVerdict{}, fmt.Errorf("memory: LLMSemanticConflictResolver requires a model")
	}

	prompt := buildSemanticConflictPrompt(candidate, peers)
	gen, err := r.Model.Chat(ctx, []model.Message{
		{Role: "system", Content: llmSemanticConflictSystem},
		{Role: "user", Content: prompt},
	})
	if err != nil {
		return SemanticConflictVerdict{}, err
	}
	if gen == nil {
		return SemanticConflictVerdict{}, fmt.Errorf("memory: empty semantic conflict generation")
	}

	raw := strings.TrimSpace(gen.Text)
	raw = stripJSONFences(raw)
	var parsed llmSemanticConflictResponse
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		return SemanticConflictVerdict{}, fmt.Errorf("memory: parse semantic conflict JSON: %w", err)
	}

	decision, err := normalizeConflictDecision(parsed.Decision)
	if err != nil {
		return SemanticConflictVerdict{}, err
	}
	target := strings.TrimSpace(parsed.TargetUnitID)

	if decision == ConflictSupersede {
		if target == "" {
			return SemanticConflictVerdict{}, fmt.Errorf("memory: supersede requires target_unit_id")
		}
		if !peerIDIn(peers, target) {
			return SemanticConflictVerdict{}, fmt.Errorf("memory: supersede target_unit_id %q not in peers", target)
		}
	}

	return SemanticConflictVerdict{Decision: decision, TargetUnitID: target}, nil
}

func buildSemanticConflictPrompt(candidate RememberInput, peers []MemoryHit) string {
	var b strings.Builder
	b.WriteString("New fact:\n")
	b.WriteString(truncateRunes(strings.TrimSpace(candidate.Content), maxTurnFactBytes))
	b.WriteString("\n\nExisting facts:\n")
	for _, p := range peers {
		b.WriteString("- id=")
		b.WriteString(strings.TrimSpace(p.ID))
		b.WriteString(": ")
		b.WriteString(truncateRunes(strings.TrimSpace(p.Content), maxTurnFactBytes))
		b.WriteByte('\n')
	}
	b.WriteString("\nJudge conflict JSON.")
	return b.String()
}

func normalizeConflictDecision(s string) (ConflictDecision, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "ignore":
		return ConflictIgnore, nil
	case "supersede":
		return ConflictSupersede, nil
	case "keep_both":
		return ConflictKeepBoth, nil
	default:
		return 0, fmt.Errorf("memory: unknown conflict decision %q", s)
	}
}

func peerIDIn(peers []MemoryHit, id string) bool {
	for _, p := range peers {
		if p.ID == id {
			return true
		}
	}
	return false
}
