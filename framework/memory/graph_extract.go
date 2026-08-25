package memory

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/sixath/framework/model"
)

// ErrGraphExtractParse is returned when the model output is not valid graph JSON
// (including truncated completions that cannot be salvaged).
var ErrGraphExtractParse = errors.New("memory: parse graph extraction JSON")

const (
	maxGraphExtractInputRunes = 4000
	maxGraphExtractOutTokens  = 4096
	defaultGraphMaxEntities   = 32
	defaultGraphMinConfidence = 0.7

	llmGraphExtractSystemPrompt = `You extract entities and relations for a knowledge graph from one chat turn.
Reply with ONLY valid JSON (no markdown fences):
{"entities":[{"name":"...","type":"...","scope":"user|session","confidence":0.0}],"relations":[{"subject":"...","predicate":"...","object":"...","scope":"user|session","confidence":0.0}]}
Rules:
- Extract durable, named entities when clearly present: people, organizations, places, products, concepts, systems/components, and other concrete things
- Prefer specific names as written in the turn; keep original spelling
- entity.type: short open label (e.g. person, org, place, product, concept, system, component)
- Relations: short snake_case predicates for clear factual edges (e.g. works_at, owns, related_to, depends_on, uses, contains, located_at). Use the most precise verb that fits; do not invent weak links
- Cap output: at most %d entities and %d relations; skip duplicates; keep JSON compact so it finishes
- When many high-confidence edges are stated, extract them rather than summarizing into fewer
- Pure preferences or chatter with no extractable entities → {"entities":[],"relations":[]}
- scope "user": should persist across sessions (stable identity, lasting affiliations, durable preferences tied to named entities)
- scope "session": tied to this conversation or provisional working knowledge that should not outlive the session
- Prefer "session" when unsure whether a fact should follow the user across conversations
- confidence is 0..1; omit weak guesses`
)

// EntityDraft is a GraphExtractor proposal before StableEntityID assignment.
type EntityDraft struct {
	Name       string
	Type       string
	Scope      Scope
	Confidence float64
}

// RelationDraft references entities by name within the same extract.
type RelationDraft struct {
	Subject    string
	Predicate  string
	Object     string
	Scope      Scope
	Confidence float64
}

// GraphExtract is the result of GraphExtractor.
type GraphExtract struct {
	Entities  []EntityDraft
	Relations []RelationDraft
}

// GraphExtractor turns a conversation turn into graph drafts (independent of fact Extractor).
type GraphExtractor interface {
	Extract(ctx context.Context, in TurnInput) (GraphExtract, error)
}

// Graph extract result kinds for logs / metrics.
const (
	GraphResultDisabled   = "disabled"
	GraphResultEmptyInput = "empty_input"
	GraphResultModelFail  = "model_fail"
	GraphResultParseFail  = "parse_fail"
	GraphResultError      = "error"
	GraphResultSuccess    = "success"
)

// Graph drop reasons (finite enum).
const (
	GraphDropEmpty         = "empty"
	GraphDropInvalidScope  = "invalid_scope"
	GraphDropMissingScope  = "missing_scope_id"
	GraphDropLowConfidence = "low_confidence"
	GraphDropMaxEntities   = "max_entities"
	GraphDropUpsertFail    = "upsert_fail"
)

// GraphStats is the per-turn graph extraction funnel for observability.
type GraphStats struct {
	Result            string
	CandidateEntities int
	CandidateRels     int
	WrittenEntities   int
	WrittenRels       int
	Drops             map[string]int
	Duration          time.Duration
}

// GraphPipeline writes graph drafts into GraphStore after an independent LLM extract.
type GraphPipeline struct {
	Graph                 GraphStore
	Extractor             GraphExtractor
	Enabled               bool
	MaxEntities           int
	MinRelationConfidence float64
}

// AddGraphFromTurn extracts entities/relations and upserts into GraphStore.
// Returns number of relations written (entities may be written without relations).
func (p *GraphPipeline) AddGraphFromTurn(ctx context.Context, in TurnInput) (int, error) {
	st, err := p.AddGraphFromTurnWithStats(ctx, in)
	return st.WrittenRels, err
}

// AddGraphFromTurnWithStats is like AddGraphFromTurn but returns funnel stats.
func (p *GraphPipeline) AddGraphFromTurnWithStats(ctx context.Context, in TurnInput) (st GraphStats, err error) {
	start := time.Now()
	st.Drops = map[string]int{}
	defer func() { st.Duration = time.Since(start) }()

	if p == nil || !p.Enabled || p.Graph == nil || p.Extractor == nil {
		st.Result = GraphResultDisabled
		return st, nil
	}
	userMsg := strings.TrimSpace(in.UserMessage)
	asstMsg := strings.TrimSpace(in.AssistantMessage)
	if userMsg == "" && asstMsg == "" {
		st.Result = GraphResultEmptyInput
		return st, nil
	}

	ex, err := p.Extractor.Extract(ctx, in)
	if err != nil {
		if errors.Is(err, ErrGraphExtractParse) {
			st.Result = GraphResultParseFail
			return st, err
		}
		st.Result = GraphResultModelFail
		return st, err
	}
	st.CandidateEntities = len(ex.Entities)
	st.CandidateRels = len(ex.Relations)

	maxEnt := p.MaxEntities
	if maxEnt <= 0 {
		maxEnt = defaultGraphMaxEntities
	}
	minConf := p.MinRelationConfidence
	if minConf <= 0 {
		minConf = defaultGraphMinConfidence
	}

	type key struct {
		scope Scope
		name  string
	}
	idByKey := map[key]string{}
	for _, d := range ex.Entities {
		if st.WrittenEntities >= maxEnt {
			st.Drops[GraphDropMaxEntities]++
			continue
		}
		name := strings.TrimSpace(d.Name)
		scope := d.Scope
		if name == "" {
			st.Drops[GraphDropEmpty]++
			continue
		}
		if scope != ScopeUser && scope != ScopeSession {
			st.Drops[GraphDropInvalidScope]++
			continue
		}
		if d.Confidence > 0 && d.Confidence < minConf {
			st.Drops[GraphDropLowConfidence]++
			continue
		}
		scopeID := ""
		switch scope {
		case ScopeSession:
			scopeID = strings.TrimSpace(in.SessionID)
		case ScopeUser:
			scopeID = strings.TrimSpace(in.UserID)
		}
		if scopeID == "" {
			st.Drops[GraphDropMissingScope]++
			continue
		}
		id := StableEntityID(scope, scopeID, name)
		e := Entity{
			ID: id, Name: name, Type: strings.TrimSpace(d.Type),
			Scope: scope, ScopeID: scopeID, Confidence: d.Confidence,
		}
		if err := p.Graph.UpsertEntity(ctx, e); err != nil {
			st.Drops[GraphDropUpsertFail]++
			continue
		}
		idByKey[key{scope: scope, name: NormalizeEntityName(name)}] = id
		st.WrittenEntities++
	}

	for _, r := range ex.Relations {
		if r.Confidence > 0 && r.Confidence < minConf {
			st.Drops[GraphDropLowConfidence]++
			continue
		}
		scope := r.Scope
		if scope != ScopeUser && scope != ScopeSession {
			st.Drops[GraphDropInvalidScope]++
			continue
		}
		scopeID := ""
		switch scope {
		case ScopeSession:
			scopeID = strings.TrimSpace(in.SessionID)
		case ScopeUser:
			scopeID = strings.TrimSpace(in.UserID)
		}
		if scopeID == "" {
			st.Drops[GraphDropMissingScope]++
			continue
		}
		subj := strings.TrimSpace(r.Subject)
		obj := strings.TrimSpace(r.Object)
		pred := strings.TrimSpace(r.Predicate)
		if subj == "" || obj == "" || pred == "" {
			st.Drops[GraphDropEmpty]++
			continue
		}
		sid, ok1 := idByKey[key{scope: scope, name: NormalizeEntityName(subj)}]
		oid, ok2 := idByKey[key{scope: scope, name: NormalizeEntityName(obj)}]
		if !ok1 {
			sid = StableEntityID(scope, scopeID, subj)
			if err := p.Graph.UpsertEntity(ctx, Entity{ID: sid, Name: subj, Scope: scope, ScopeID: scopeID, Confidence: r.Confidence}); err != nil {
				st.Drops[GraphDropUpsertFail]++
				continue
			}
			idByKey[key{scope: scope, name: NormalizeEntityName(subj)}] = sid
			st.WrittenEntities++
		}
		if !ok2 {
			oid = StableEntityID(scope, scopeID, obj)
			if err := p.Graph.UpsertEntity(ctx, Entity{ID: oid, Name: obj, Scope: scope, ScopeID: scopeID, Confidence: r.Confidence}); err != nil {
				st.Drops[GraphDropUpsertFail]++
				continue
			}
			idByKey[key{scope: scope, name: NormalizeEntityName(obj)}] = oid
			st.WrittenEntities++
		}
		if err := p.Graph.UpsertRelation(ctx, Relation{
			SubjectID: sid, Predicate: pred, ObjectID: oid,
			Scope: scope, ScopeID: scopeID, Confidence: r.Confidence,
		}); err != nil {
			st.Drops[GraphDropUpsertFail]++
			continue
		}
		st.WrittenRels++
	}
	st.Result = GraphResultSuccess
	return st, nil
}

// LLMGraphExtractor uses a chat model to propose graph drafts as JSON.
type LLMGraphExtractor struct {
	Model       model.Model
	MaxEntities int
}

type llmGraphExtractResponse struct {
	Entities []struct {
		Name       string  `json:"name"`
		Type       string  `json:"type"`
		Scope      string  `json:"scope"`
		Confidence float64 `json:"confidence"`
	} `json:"entities"`
	Relations []struct {
		Subject    string  `json:"subject"`
		Predicate  string  `json:"predicate"`
		Object     string  `json:"object"`
		Scope      string  `json:"scope"`
		Confidence float64 `json:"confidence"`
	} `json:"relations"`
}

// Extract implements GraphExtractor.
func (e *LLMGraphExtractor) Extract(ctx context.Context, in TurnInput) (GraphExtract, error) {
	if e == nil || e.Model == nil {
		return GraphExtract{}, fmt.Errorf("memory: LLMGraphExtractor requires a model")
	}
	maxEnt := e.MaxEntities
	if maxEnt <= 0 {
		maxEnt = defaultGraphMaxEntities
	}
	user := truncateRunes(strings.TrimSpace(in.UserMessage), maxGraphExtractInputRunes)
	asst := truncateRunes(strings.TrimSpace(in.AssistantMessage), maxGraphExtractInputRunes)
	prompt := fmt.Sprintf("User:\n%s\n\nAssistant:\n%s\n\nExtract graph JSON.", user, asst)
	sys := fmt.Sprintf(llmGraphExtractSystemPrompt, maxEnt, maxEnt)

	gen, err := e.Model.Chat(ctx, []model.Message{
		{Role: "system", Content: sys},
		{Role: "user", Content: prompt},
	}, model.WithTemperature(0), model.WithMaxTokens(maxGraphExtractOutTokens))
	if err != nil {
		return GraphExtract{}, err
	}
	if gen == nil {
		return GraphExtract{}, fmt.Errorf("memory: empty graph extraction generation")
	}
	raw := stripJSONFences(strings.TrimSpace(gen.Text))
	parsed, err := unmarshalGraphExtractJSON(raw)
	if err != nil {
		return GraphExtract{}, fmt.Errorf("%w: %v", ErrGraphExtractParse, err)
	}

	out := GraphExtract{}
	for _, ent := range parsed.Entities {
		if len(out.Entities) >= maxEnt {
			break
		}
		name := strings.TrimSpace(ent.Name)
		scope := Scope(strings.TrimSpace(strings.ToLower(ent.Scope)))
		if name == "" || (scope != ScopeUser && scope != ScopeSession) {
			continue
		}
		out.Entities = append(out.Entities, EntityDraft{
			Name: name, Type: strings.TrimSpace(ent.Type), Scope: scope, Confidence: ent.Confidence,
		})
	}
	for _, rel := range parsed.Relations {
		scope := Scope(strings.TrimSpace(strings.ToLower(rel.Scope)))
		if scope != ScopeUser && scope != ScopeSession {
			continue
		}
		out.Relations = append(out.Relations, RelationDraft{
			Subject: strings.TrimSpace(rel.Subject), Predicate: strings.TrimSpace(rel.Predicate),
			Object: strings.TrimSpace(rel.Object), Scope: scope, Confidence: rel.Confidence,
		})
	}
	return out, nil
}

func unmarshalGraphExtractJSON(raw string) (llmGraphExtractResponse, error) {
	var parsed llmGraphExtractResponse
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return parsed, fmt.Errorf("empty")
	}
	if err := json.Unmarshal([]byte(raw), &parsed); err == nil {
		return parsed, nil
	}
	// Completions often hit max_tokens mid-object. Walk back to a '}' and close open brackets.
	for i := len(raw) - 1; i >= 0; i-- {
		if raw[i] != '}' {
			continue
		}
		closed := closeTruncatedJSON(raw[:i+1])
		if err := json.Unmarshal([]byte(closed), &parsed); err == nil {
			return parsed, nil
		}
	}
	closed := closeTruncatedJSON(raw)
	if err := json.Unmarshal([]byte(closed), &parsed); err == nil {
		return parsed, nil
	}
	return parsed, json.Unmarshal([]byte(raw), &parsed)
}

// closeTruncatedJSON closes an unterminated string and any unclosed { / [ so a
// length-capped model reply can still Unmarshal.
func closeTruncatedJSON(s string) string {
	var stack []byte
	inStr := false
	esc := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if inStr {
			if esc {
				esc = false
				continue
			}
			if c == '\\' {
				esc = true
				continue
			}
			if c == '"' {
				inStr = false
			}
			continue
		}
		switch c {
		case '"':
			inStr = true
		case '{', '[':
			stack = append(stack, c)
		case '}':
			if n := len(stack); n > 0 && stack[n-1] == '{' {
				stack = stack[:n-1]
			}
		case ']':
			if n := len(stack); n > 0 && stack[n-1] == '[' {
				stack = stack[:n-1]
			}
		}
	}
	out := s
	if inStr {
		if esc && len(out) > 0 && out[len(out)-1] == '\\' {
			out = out[:len(out)-1]
		}
		out += `"`
	}
	for i := len(stack) - 1; i >= 0; i-- {
		if stack[i] == '{' {
			out += "}"
		} else {
			out += "]"
		}
	}
	return out
}
