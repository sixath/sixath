package memory

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync/atomic"
	"time"
)

// hybridEmbedTimeout is the read-path Embed budget. Tests may shrink it temporarily.
var hybridEmbedTimeout = 800 * time.Millisecond

// FacadeConfig defines the storage backends used by Facade.
type FacadeConfig struct {
	Session              SessionUnitsBackend
	Agent                AgentWorkspaceBackend
	Transcript           TranscriptBackend
	Conflicts            ConflictResolver
	SemanticConflicts    SemanticConflictResolver
	SemanticConflictK    int  // Recall peer limit; default 8 if <= 0
	ToolSemanticConflict bool // tool-path add semantic gate; extract uses Metadata source=turn_extract

	// Vectors / Embed / VectorAsync / Graph* configure the Qdrant units sidecar (P2-H)
	// and the Neo4j graph sidecar (P2-I).
	Vectors VectorIndex
	Embed   EmbedFunc
	// VectorAsync: nil or true = async index writes; false = sync (tests).
	VectorAsync  *bool
	Graph        GraphStore
	GraphMaxHops int // Expand hops; default 1 if <= 0
	GraphRRFK    int // RRF k; default 60 if <= 0
	// GraphAsync: nil or true = async invalidate; false = sync (tests).
	GraphAsync *bool

	// UnitVectors / UnitEmbedder configure the pluggable SQLite/in-memory units hybrid
	// (LIKE ∪ vector RRF) sidecar (P2-E1).
	UnitVectors  UnitVectorIndex
	UnitEmbedder UnitEmbedder
	// HybridRecall gates units hybrid recall per agent. nil = always allow.
	HybridRecall func(ctx context.Context, agentID string) bool
	// EmbedTripped is the process-level embed breaker. nil → NewFacade allocates one.
	// When shared (e.g. with UnitBackfiller), rebuilding a Facade does not reset it.
	EmbedTripped *atomic.Bool
}

// Facade routes memory operations to their scope-specific backend.
type Facade struct {
	session              SessionUnitsBackend
	agent                AgentWorkspaceBackend
	transcript           TranscriptBackend
	conflicts            ConflictResolver
	semanticConflicts    SemanticConflictResolver
	semanticConflictK    int
	toolSemanticConflict bool

	vectors      VectorIndex
	embed        EmbedFunc
	vectorAsync  bool
	graph        GraphStore
	graphMaxHops int
	graphRRFK    int
	graphAsync   bool

	unitVectors  UnitVectorIndex
	unitEmbedder UnitEmbedder
	hybridRecall func(ctx context.Context, agentID string) bool
	queryCache   *queryEmbedCache
	// Shared when EmbedTripped is injected; otherwise owned by this Facade instance.
	embedTripped *atomic.Bool
}

var (
	_ MemoryStore = (*Facade)(nil)
	_ UnitPatcher = (*Facade)(nil)
)

func NewFacade(cfg FacadeConfig) *Facade {
	if cfg.Conflicts == nil {
		cfg.Conflicts = StructuralReplaceResolver{}
	}
	k := cfg.SemanticConflictK
	if k <= 0 {
		k = 8
	}
	async := true
	if cfg.VectorAsync != nil {
		async = *cfg.VectorAsync
	}
	hops := cfg.GraphMaxHops
	if hops <= 0 {
		hops = 1
	}
	rrfK := cfg.GraphRRFK
	if rrfK <= 0 {
		rrfK = 60
	}
	graphAsync := true
	if cfg.GraphAsync != nil {
		graphAsync = *cfg.GraphAsync
	}
	tripped := cfg.EmbedTripped
	if tripped == nil {
		tripped = &atomic.Bool{}
	}
	return &Facade{
		session:              cfg.Session,
		agent:                cfg.Agent,
		transcript:           cfg.Transcript,
		conflicts:            cfg.Conflicts,
		semanticConflicts:    cfg.SemanticConflicts,
		semanticConflictK:    k,
		toolSemanticConflict: cfg.ToolSemanticConflict,
		vectors:              cfg.Vectors,
		embed:                cfg.Embed,
		vectorAsync:          async,
		graph:                cfg.Graph,
		graphMaxHops:         hops,
		graphRRFK:            rrfK,
		graphAsync:           graphAsync,
		unitVectors:          cfg.UnitVectors,
		unitEmbedder:         cfg.UnitEmbedder,
		hybridRecall:         cfg.HybridRecall,
		queryCache:           newQueryEmbedCache(64),
		embedTripped:         tripped,
	}
}

func userScopeIDEmpty(id string) bool { return strings.TrimSpace(id) == "" }

func (f *Facade) Remember(ctx context.Context, in RememberInput) (MemoryHit, error) {
	if kind, _ := in.Metadata["kind"].(string); strings.EqualFold(strings.TrimSpace(kind), "procedural") {
		return MemoryHit{}, ErrProceduralRememberBlocked
	}
	switch in.Scope {
	case ScopeUser:
		if userScopeIDEmpty(in.ScopeID) {
			return MemoryHit{}, nil
		}
		if f.session == nil {
			return MemoryHit{}, errors.New("memory: session backend not configured")
		}
		return f.rememberUnits(ctx, in)
	case ScopeSession:
		if f.session == nil {
			return MemoryHit{}, errors.New("memory: session backend not configured")
		}
		return f.rememberUnits(ctx, in)
	case ScopeAgent:
		if f.agent == nil {
			return MemoryHit{}, errors.New("memory: agent backend not configured")
		}
		return f.agent.Remember(ctx, in)
	default:
		return MemoryHit{}, ErrNotSupported
	}
}

// rememberUnits gates ActionReplace through ConflictResolver and ActionAdd through
// optional SemanticConflictResolver. remove skips both. Delete remains a direct backend call.
func (f *Facade) rememberUnits(ctx context.Context, in RememberInput) (MemoryHit, error) {
	switch in.Action {
	case ActionRemove:
		hit, err := f.session.Remember(ctx, in)
		if err != nil {
			return MemoryHit{}, err
		}
		f.deleteVector(ctx, in.UnitID)
		f.invalidateGraph(ctx, in.UnitID)
		f.syncDelete(ctx, in.Scope, in.ScopeID, in.UnitID)
		return hit, nil
	case ActionReplace:
		return f.rememberReplace(ctx, in)
	default: // ActionAdd and unknown → treat as add path
		return f.rememberAdd(ctx, in)
	}
}

func (f *Facade) rememberReplace(ctx context.Context, in RememberInput) (MemoryHit, error) {
	existing, err := f.session.Get(ctx, GetRef{Scope: in.Scope, ID: in.UnitID, ScopeID: in.ScopeID})
	if err != nil {
		return MemoryHit{}, err
	}
	if st, _ := existing.Metadata["status"].(string); st != "" && st != "active" {
		return MemoryHit{}, fmt.Errorf("memory: unit %q not found", in.UnitID)
	}

	d, err := f.conflicts.Resolve(ctx, existing, in)
	if err != nil {
		return MemoryHit{}, err
	}
	switch d {
	case ConflictSupersede:
		hit, err := f.session.Remember(ctx, in)
		if err != nil {
			return MemoryHit{}, err
		}
		f.deleteVector(ctx, in.UnitID)
		f.invalidateGraph(ctx, in.UnitID)
		f.syncDelete(ctx, in.Scope, in.ScopeID, in.UnitID)
		f.indexHit(ctx, hit, in.ScopeID, in.AgentID)
		f.syncUpsert(ctx, in, hit.ID) // 熔断 / 未装配时内部跳过（E2：不再按 D2 门控）
		return hit, nil
	case ConflictIgnore:
		return MemoryHit{}, nil
	default: // KeepBoth or unknown
		return MemoryHit{}, fmt.Errorf("memory: conflict decision %v not allowed for replace", d)
	}
}

func (f *Facade) rememberAdd(ctx context.Context, in RememberInput) (MemoryHit, error) {
	if f.skipIfActiveContentHash(ctx, in) {
		return MemoryHit{}, nil
	}
	if !f.semanticEnabled(in) || f.semanticConflicts == nil {
		hit, err := f.session.Remember(ctx, in)
		if err != nil {
			return MemoryHit{}, err
		}
		f.syncUpsert(ctx, in, hit.ID)
		f.indexHit(ctx, hit, in.ScopeID, in.AgentID)
		return hit, nil
	}

	// Peer discovery for semantic conflict resolution: prefer the SQLite hybrid
	// sidecar (reuses the computed vector for upsert below), else the Qdrant
	// sidecar, else LIKE.
	var peers []MemoryHit
	var peerVec []float32
	usedVector := false
	if f.vectorReady() {
		if vectorPeers, vec, ok := f.vectorPeers(ctx, in); ok {
			peers = vectorPeers
			peerVec = vec
			usedVector = true
		}
	}
	if !usedVector {
		if qPeers, ok := f.recallUnitsVector(ctx, f.peerRecallQuery(in)); ok && len(qPeers) > 0 {
			peers = qPeers
		} else {
			likePeers, err := f.likePeers(ctx, in)
			if err != nil {
				return MemoryHit{}, nil
			}
			peers = likePeers
		}
	}
	if len(peers) == 0 {
		hit, err := f.session.Remember(ctx, in)
		if err != nil {
			return MemoryHit{}, err
		}
		if usedVector {
			f.syncUpsertVec(ctx, in, hit.ID, peerVec)
		} else {
			f.syncUpsert(ctx, in, hit.ID)
		}
		f.indexHit(ctx, hit, in.ScopeID, in.AgentID)
		return hit, nil
	}

	verdict, err := f.semanticConflicts.ResolveAdd(ctx, in, peers)
	if err != nil || verdict.Decision == ConflictIgnore {
		return MemoryHit{}, nil
	}
	switch verdict.Decision {
	case ConflictKeepBoth:
		hit, err := f.session.Remember(ctx, in)
		if err != nil {
			return MemoryHit{}, err
		}
		if usedVector {
			f.syncUpsertVec(ctx, in, hit.ID, peerVec)
		} else {
			f.syncUpsert(ctx, in, hit.ID)
		}
		f.indexHit(ctx, hit, in.ScopeID, in.AgentID)
		return hit, nil
	case ConflictSupersede:
		if !peerIDActive(peers, verdict.TargetUnitID) {
			return MemoryHit{}, nil
		}
		// Bypass D1 ConflictResolver: semantic verdict already decided supersede.
		replaceIn := RememberInput{
			Scope:    in.Scope,
			ScopeID:  in.ScopeID,
			AgentID:  in.AgentID,
			Action:   ActionReplace,
			UnitID:   verdict.TargetUnitID,
			Content:  in.Content,
			Metadata: in.Metadata,
		}
		hit, err := f.session.Remember(ctx, replaceIn)
		if err != nil {
			return MemoryHit{}, err
		}
		f.deleteVector(ctx, verdict.TargetUnitID)
		f.invalidateGraph(ctx, verdict.TargetUnitID)
		f.syncDelete(ctx, in.Scope, in.ScopeID, verdict.TargetUnitID)
		if usedVector {
			f.syncUpsertVec(ctx, replaceIn, hit.ID, peerVec)
		} else {
			f.syncUpsert(ctx, replaceIn, hit.ID)
		}
		f.indexHit(ctx, hit, in.ScopeID, in.AgentID)
		return hit, nil
	default:
		return MemoryHit{}, nil
	}
}

func (f *Facade) semanticEnabled(in RememberInput) bool {
	if f.semanticConflicts == nil {
		return false
	}
	if src, _ := in.Metadata["source"].(string); src == "turn_extract" {
		return true
	}
	return f.toolSemanticConflict
}

func (f *Facade) skipIfActiveContentHash(ctx context.Context, in RememberInput) bool {
	kindFilter := KindFilterFactOnly
	if UnitKindFromMetadata(in.Metadata) == KindProcedural {
		kindFilter = KindProcedural
	}
	list, err := f.session.List(ctx, ListFilter{Scope: in.Scope, ScopeID: in.ScopeID, Status: "active", Kind: kindFilter})
	if err != nil {
		return false
	}
	want := ContentHash(in.Content)
	for _, hit := range list {
		if hit.Metadata == nil {
			continue
		}
		if h, ok := hit.Metadata["content_hash"].(string); ok && h == want {
			return true
		}
	}
	return false
}

func peerIDActive(peers []MemoryHit, id string) bool {
	if strings.TrimSpace(id) == "" {
		return false
	}
	for _, p := range peers {
		if p.ID != id {
			continue
		}
		if st, _ := p.Metadata["status"].(string); st != "" && st != "active" {
			return false
		}
		return true
	}
	return false
}

func (f *Facade) Recall(ctx context.Context, q RecallQuery) ([]MemoryHit, error) {
	if q.Scope == ScopeUser && userScopeIDEmpty(q.ScopeID) {
		return []MemoryHit{}, nil
	}

	source := q.Source
	if source == "" {
		if q.Scope == ScopeAgent {
			source = SourceFiles
		} else {
			source = SourceUnits
		}
	}
	switch source {
	case SourceUnits:
		if f.session == nil {
			return []MemoryHit{}, nil
		}
		// Graph/Qdrant hybrid (P2-H/I) takes priority when configured; it fails
		// open (ok=false) to the SQLite hybrid and finally to LIKE.
		if f.graph != nil || f.qdrantReady() {
			if hits, ok := f.recallUnitsHybrid(ctx, q); ok {
				return filterHitsByKind(hits, q.Kind), nil
			}
		}
		if f.hybridReadable(ctx, q) {
			hits, err := f.hybridUnitsRecall(ctx, q)
			return filterHitsByKind(hits, q.Kind), err
		}
		return f.session.Recall(ctx, q)
	case SourceFiles:
		if f.agent == nil {
			return []MemoryHit{}, nil
		}
		return f.agent.Recall(ctx, q)
	case SourceTranscript:
		if f.transcript == nil {
			return []MemoryHit{}, nil
		}
		return f.transcript.Recall(ctx, q)
	default:
		return nil, ErrNotSupported
	}
}

// hybridReadable reports whether units Recall may run the LIKE∪vector hybrid path
// against the SQLite/in-memory units sidecar (P2-E1).
// Blank AgentID defaults to allow (skip gate); non-blank AgentID consults HybridRecall when set.
func (f *Facade) hybridReadable(ctx context.Context, q RecallQuery) bool {
	if !f.vectorReady() || strings.TrimSpace(q.Query) == "" {
		return false
	}
	if strings.TrimSpace(q.AgentID) == "" {
		return true
	}
	return f.hybridRecall == nil || f.hybridRecall(ctx, q.AgentID)
}

// hybridUnitsRecall runs LIKE then optional vector branch and RRF-merges to effLimit.
// q.MinScore is intentionally ignored on this path.
func (f *Facade) hybridUnitsRecall(ctx context.Context, q RecallQuery) ([]MemoryHit, error) {
	effLimit := q.Limit
	if effLimit <= 0 {
		effLimit = 5
	}
	n := 2 * effLimit

	likeQ := q
	likeQ.Limit = n
	likeHits, err := f.session.Recall(ctx, likeQ)
	if err != nil {
		return nil, err
	}

	vecHits := f.tryVectorUnits(ctx, q, n)
	if vecHits == nil {
		return truncateMemoryHits(likeHits, effLimit), nil
	}
	return rrfMerge(likeHits, vecHits, effLimit), nil
}

// tryVectorUnits embeds the query, searches the sidecar, and hydrates active units.
// Any failure returns nil so the caller can fail-open to LIKE.
func (f *Facade) tryVectorUnits(ctx context.Context, q RecallQuery, limit int) []MemoryHit {
	vec := f.embedQuery(ctx, q.AgentID, q.Query)
	if vec == nil {
		return nil
	}
	hits, err := f.unitVectors.Search(ctx, UnitVectorQuery{
		Scope:   q.Scope,
		ScopeID: q.ScopeID,
		Vector:  vec,
		Limit:   limit,
	})
	if err != nil {
		return nil
	}
	return f.hydrateActive(ctx, q.Scope, q.ScopeID, hits)
}

// embedQuery embeds a recall query with LRU cache and a short timeout.
// DeadlineExceeded / Canceled do not trip the process breaker; other errors do.
func (f *Facade) embedQuery(ctx context.Context, agentID, query string) []float32 {
	key := agentID + "\x00" + query
	if cached := f.queryCache.get(key); cached != nil {
		return cached
	}
	ctx2, cancel := context.WithTimeout(ctx, hybridEmbedTimeout)
	defer cancel()
	vecs, err := f.unitEmbedder.Embed(ctx2, agentID, []string{query})
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
			return nil
		}
		f.embedTripped.Store(true)
		return nil
	}
	if len(vecs) == 0 || len(vecs[0]) == 0 {
		f.embedTripped.Store(true)
		return nil
	}
	f.queryCache.put(key, vecs[0])
	return vecs[0]
}

func truncateMemoryHits(hits []MemoryHit, limit int) []MemoryHit {
	if limit <= 0 || len(hits) <= limit {
		return hits
	}
	return hits[:limit]
}

func filterHitsByKind(hits []MemoryHit, kindFilter string) []MemoryHit {
	if len(hits) == 0 {
		return hits
	}
	out := make([]MemoryHit, 0, len(hits))
	for _, h := range hits {
		if KindMatchesFilter(UnitKindFromMetadata(h.Metadata), kindFilter) {
			out = append(out, h)
		}
	}
	return out
}

func (f *Facade) Get(ctx context.Context, ref GetRef) (MemoryHit, error) {
	switch ref.Scope {
	case ScopeUser:
		if userScopeIDEmpty(ref.ScopeID) {
			return MemoryHit{}, fmt.Errorf("memory: unit %q not found", ref.ID)
		}
		if f.session == nil {
			return MemoryHit{}, errors.New("memory: session backend not configured")
		}
		return f.session.Get(ctx, ref)
	case ScopeSession:
		if f.session == nil {
			return MemoryHit{}, errors.New("memory: session backend not configured")
		}
		return f.session.Get(ctx, ref)
	case ScopeAgent:
		if f.agent == nil {
			return MemoryHit{}, errors.New("memory: agent backend not configured")
		}
		return f.agent.Get(ctx, ref)
	default:
		return MemoryHit{}, ErrNotSupported
	}
}

// PatchUnit forwards in-place metadata/content updates to the session units backend.
// Unlike Remember(ActionReplace), the unit ID is preserved (no supersede).
func (f *Facade) PatchUnit(ctx context.Context, ref GetRef, content *string, metadata map[string]any) error {
	switch ref.Scope {
	case ScopeUser:
		if userScopeIDEmpty(ref.ScopeID) {
			return fmt.Errorf("memory: unit %q not found", ref.ID)
		}
		if f.session == nil {
			return errors.New("memory: session backend not configured")
		}
		return f.patchSessionUnit(ctx, ref, content, metadata)
	case ScopeSession:
		if f.session == nil {
			return errors.New("memory: session backend not configured")
		}
		return f.patchSessionUnit(ctx, ref, content, metadata)
	default:
		return ErrNotSupported
	}
}

func (f *Facade) patchSessionUnit(ctx context.Context, ref GetRef, content *string, metadata map[string]any) error {
	if err := f.session.PatchUnit(ctx, ref, content, metadata); err != nil {
		return err
	}
	if content == nil {
		return nil
	}
	// Content changed on the same ID: refresh vector/graph sidecars (no supersede/delete).
	hit, err := f.session.Get(ctx, ref)
	if err != nil {
		return nil
	}
	f.invalidateGraph(ctx, ref.ID)
	f.indexHit(ctx, hit, ref.ScopeID, ref.AgentID)
	f.syncUpsert(ctx, RememberInput{
		Scope:   ref.Scope,
		ScopeID: ref.ScopeID,
		AgentID: ref.AgentID,
		Content: *content,
	}, ref.ID)
	return nil
}

func (f *Facade) List(ctx context.Context, filter ListFilter) ([]MemoryHit, error) {
	switch filter.Scope {
	case ScopeUser:
		if userScopeIDEmpty(filter.ScopeID) {
			return []MemoryHit{}, nil
		}
		if f.session == nil {
			return []MemoryHit{}, errors.New("memory: session backend not configured")
		}
		return f.session.List(ctx, filter)
	case ScopeSession:
		if f.session == nil {
			return []MemoryHit{}, errors.New("memory: session backend not configured")
		}
		return f.session.List(ctx, filter)
	case ScopeAgent:
		return nil, ErrNotSupported
	default:
		return nil, ErrNotSupported
	}
}

func (f *Facade) Delete(ctx context.Context, ref GetRef) error {
	switch ref.Scope {
	case ScopeUser:
		if userScopeIDEmpty(ref.ScopeID) {
			return nil
		}
		if f.session == nil {
			return errors.New("memory: session backend not configured")
		}
		if err := f.session.Delete(ctx, ref); err != nil {
			return err
		}
		f.deleteVector(ctx, ref.ID)
		f.invalidateGraph(ctx, ref.ID)
		f.syncDelete(ctx, ref.Scope, ref.ScopeID, ref.ID)
		return nil
	case ScopeSession:
		if f.session == nil {
			return errors.New("memory: session backend not configured")
		}
		if err := f.session.Delete(ctx, ref); err != nil {
			return err
		}
		f.deleteVector(ctx, ref.ID)
		f.invalidateGraph(ctx, ref.ID)
		f.syncDelete(ctx, ref.Scope, ref.ScopeID, ref.ID)
		return nil
	case ScopeAgent:
		return ErrNotSupported
	default:
		return ErrNotSupported
	}
}

// qdrantReady reports whether the Qdrant units vector sidecar (P2-H) is configured.
func (f *Facade) qdrantReady() bool {
	return f != nil && f.vectors != nil && f.embed != nil
}

// recallUnitsVector returns (hits, true) when vector search against the Qdrant
// sidecar produced a usable result set. Empty query or vector failure → (nil, false)
// so the caller falls back to LIKE.
func (f *Facade) recallUnitsVector(ctx context.Context, q RecallQuery) ([]MemoryHit, bool) {
	if !f.qdrantReady() {
		return nil, false
	}
	query := strings.TrimSpace(q.Query)
	if query == "" {
		return nil, false
	}
	limit := q.Limit
	if limit <= 0 {
		limit = 5
	}
	embs, err := f.embed(ctx, []string{query})
	if err != nil || len(embs) == 0 || len(embs[0]) == 0 {
		return nil, false
	}
	scored, err := f.vectors.Search(ctx, VectorSearchQuery{
		Scope: q.Scope, ScopeID: q.ScopeID, Embedding: embs[0], Limit: limit,
	})
	if err != nil || len(scored) == 0 {
		return nil, false
	}
	hits := make([]MemoryHit, 0, len(scored))
	for _, s := range scored {
		hit, err := f.session.Get(ctx, GetRef{Scope: q.Scope, ScopeID: q.ScopeID, ID: s.UnitID})
		if err != nil {
			continue
		}
		if st, _ := hit.Metadata["status"].(string); st != "" && st != "active" {
			continue
		}
		hit.Score = s.Score
		hit.Source = SourceUnits
		hits = append(hits, hit)
	}
	if len(hits) == 0 {
		return nil, false
	}
	return hits, true
}

func (f *Facade) indexHit(ctx context.Context, hit MemoryHit, scopeID, agentID string) {
	if !f.qdrantReady() || hit.ID == "" || strings.TrimSpace(hit.Content) == "" {
		return
	}
	if strings.TrimSpace(scopeID) == "" {
		scopeID = scopeIDFromHit(hit)
	}
	if strings.TrimSpace(scopeID) == "" {
		return
	}
	if agentID == "" && hit.Metadata != nil {
		if aid, ok := hit.Metadata["agent_id"].(string); ok {
			agentID = aid
		}
	}
	run := func() {
		c := ctx
		if f.vectorAsync {
			c = context.Background()
		}
		if strings.TrimSpace(agentID) != "" {
			c = context.WithValue(c, "agent_id", agentID)
		}
		embs, err := f.embed(c, []string{hit.Content})
		if err != nil || len(embs) == 0 || len(embs[0]) == 0 {
			return
		}
		_ = f.vectors.Upsert(c, UnitVectorRecord{
			UnitID: hit.ID, Scope: hit.Scope, ScopeID: scopeID, Embedding: embs[0],
		})
	}
	if f.vectorAsync {
		go run()
		return
	}
	run()
}

func scopeIDFromHit(hit MemoryHit) string {
	if hit.Metadata != nil {
		if sid, ok := hit.Metadata["scope_id"].(string); ok && sid != "" {
			return sid
		}
		if hit.Scope == ScopeSession {
			if sid, ok := hit.Metadata["source_session_id"].(string); ok {
				return sid
			}
		}
		if hit.Scope == ScopeUser {
			if uid, ok := hit.Metadata["user_id"].(string); ok {
				return uid
			}
		}
	}
	return ""
}

func (f *Facade) deleteVector(ctx context.Context, unitID string) {
	if f.vectors == nil || strings.TrimSpace(unitID) == "" {
		return
	}
	run := func() {
		c := ctx
		if f.vectorAsync {
			c = context.Background()
		}
		_ = f.vectors.Delete(c, unitID)
	}
	if f.vectorAsync {
		go run()
		return
	}
	run()
}

func (f *Facade) invalidateGraph(ctx context.Context, unitID string) {
	if f.graph == nil || strings.TrimSpace(unitID) == "" {
		return
	}
	run := func() {
		c := ctx
		if f.graphAsync {
			c = context.Background()
		}
		_ = f.graph.InvalidateByMemoryID(c, unitID)
	}
	if f.graphAsync {
		go run()
		return
	}
	run()
}

// recallUnitsHybrid combines Qdrant vector Recall with optional graph Expand + RRF.
// Returns (hits, true) when a usable hybrid/vector/graph result exists; else (nil, false)
// so the caller can fall back to the SQLite hybrid or LIKE.
func (f *Facade) recallUnitsHybrid(ctx context.Context, q RecallQuery) ([]MemoryHit, bool) {
	var vectorHits []MemoryHit
	if hits, ok := f.recallUnitsVector(ctx, q); ok {
		vectorHits = hits
	}
	if f.graph == nil {
		if len(vectorHits) > 0 {
			return vectorHits, true
		}
		return nil, false
	}

	limit := q.Limit
	if limit <= 0 {
		limit = 5
	}

	seeds := f.graphSeeds(ctx, q, vectorHits)
	var graphUnitIDs []string
	if len(seeds) > 0 {
		ghits, err := f.graph.Expand(ctx, GraphExpandQuery{
			SeedEntityIDs: seeds,
			Hops:          f.graphMaxHops,
			Scope:         q.Scope,
			ScopeID:       q.ScopeID,
			Limit:         limit * 3,
		})
		if err == nil {
			seen := map[string]struct{}{}
			for _, gh := range ghits {
				for _, uid := range gh.RelatedUnitIDs {
					uid = strings.TrimSpace(uid)
					if uid == "" {
						continue
					}
					if _, ok := seen[uid]; ok {
						continue
					}
					seen[uid] = struct{}{}
					graphUnitIDs = append(graphUnitIDs, uid)
				}
			}
		}
	}

	if len(vectorHits) == 0 && len(graphUnitIDs) == 0 {
		return nil, false
	}
	if len(graphUnitIDs) == 0 {
		return vectorHits, true
	}

	vectorIDs := make([]string, 0, len(vectorHits))
	byID := map[string]MemoryHit{}
	for _, h := range vectorHits {
		vectorIDs = append(vectorIDs, h.ID)
		byID[h.ID] = h
	}
	for _, uid := range graphUnitIDs {
		if _, ok := byID[uid]; ok {
			continue
		}
		hit, err := f.session.Get(ctx, GetRef{Scope: q.Scope, ScopeID: q.ScopeID, ID: uid})
		if err != nil {
			continue
		}
		if st, _ := hit.Metadata["status"].(string); st != "" && st != "active" {
			continue
		}
		hit.Source = SourceUnits
		byID[uid] = hit
	}

	fusedIDs := rrfFuseUnitIDs([][]string{vectorIDs, graphUnitIDs}, f.graphRRFK, limit)
	out := make([]MemoryHit, 0, len(fusedIDs))
	for i, id := range fusedIDs {
		hit, ok := byID[id]
		if !ok {
			continue
		}
		hit.Score = 1.0 / float64(f.graphRRFK+i+1)
		out = append(out, hit)
	}
	if len(out) == 0 {
		return nil, false
	}
	return out, true
}

func (f *Facade) graphSeeds(ctx context.Context, q RecallQuery, vectorHits []MemoryHit) []string {
	seen := map[string]struct{}{}
	var seeds []string
	add := func(ids ...string) {
		for _, id := range ids {
			id = strings.TrimSpace(id)
			if id == "" {
				continue
			}
			if _, ok := seen[id]; ok {
				continue
			}
			seen[id] = struct{}{}
			seeds = append(seeds, id)
		}
	}
	if len(vectorHits) > 0 {
		unitIDs := make([]string, 0, len(vectorHits))
		for _, h := range vectorHits {
			unitIDs = append(unitIDs, h.ID)
		}
		ents, err := f.graph.EntitiesBySourceMemoryIDs(ctx, q.Scope, q.ScopeID, unitIDs)
		if err == nil {
			for _, e := range ents {
				add(e.ID)
			}
		}
	}
	if len(seeds) == 0 {
		matched, err := f.graph.MatchSeeds(ctx, q.Scope, q.ScopeID, q.Query, 8)
		if err == nil {
			add(matched...)
		}
	}
	return seeds
}

func rrfFuseUnitIDs(lists [][]string, k, limit int) []string {
	if k <= 0 {
		k = 60
	}
	if limit <= 0 {
		limit = 5
	}
	scores := map[string]float64{}
	for _, list := range lists {
		for rank, id := range list {
			id = strings.TrimSpace(id)
			if id == "" {
				continue
			}
			scores[id] += 1.0 / float64(k+rank+1)
		}
	}
	type pair struct {
		id    string
		score float64
	}
	pairs := make([]pair, 0, len(scores))
	for id, sc := range scores {
		pairs = append(pairs, pair{id: id, score: sc})
	}
	sort.Slice(pairs, func(i, j int) bool {
		if pairs[i].score == pairs[j].score {
			return pairs[i].id < pairs[j].id
		}
		return pairs[i].score > pairs[j].score
	})
	if len(pairs) > limit {
		pairs = pairs[:limit]
	}
	out := make([]string, len(pairs))
	for i, p := range pairs {
		out[i] = p.id
	}
	return out
}

// vectorReady reports whether the SQLite/in-memory units hybrid sidecar (P2-E1) may
// be used for peer discovery / recall.
func (f *Facade) vectorReady() bool {
	return f.unitVectors != nil && f.unitEmbedder != nil && !f.embedTripped.Load()
}

// embedOne returns the candidate vector, tripping the Facade-lifetime breaker on failure
// so a gateway without /embeddings is only probed once (spec §2.2).
func (f *Facade) embedOne(ctx context.Context, agentID, text string) []float32 {
	vecs, err := f.unitEmbedder.Embed(ctx, agentID, []string{text})
	if err != nil || len(vecs) == 0 || len(vecs[0]) == 0 {
		f.embedTripped.Store(true)
		return nil
	}
	return vecs[0]
}

// vectorPeers returns hydrated active peers and the candidate vector used for search,
// or ok=false to fall back to LIKE.
func (f *Facade) vectorPeers(ctx context.Context, in RememberInput) ([]MemoryHit, []float32, bool) {
	vec := f.embedOne(ctx, in.AgentID, in.Content)
	if vec == nil {
		return nil, nil, false
	}
	hits, err := f.unitVectors.Search(ctx, UnitVectorQuery{
		Scope:   in.Scope,
		ScopeID: in.ScopeID,
		Vector:  vec,
		Limit:   f.semanticConflictK,
	})
	if err != nil {
		return nil, nil, false
	}
	return f.hydrateActive(ctx, in.Scope, in.ScopeID, hits), vec, true
}

// hydrateActive loads active units for vector hits; stale/missing/non-active entries are dropped.
func (f *Facade) hydrateActive(ctx context.Context, scope Scope, scopeID string, hits []UnitVectorHit) []MemoryHit {
	peers := make([]MemoryHit, 0, len(hits))
	for _, hit := range hits {
		got, err := f.session.Get(ctx, GetRef{Scope: scope, ID: hit.UnitID, ScopeID: scopeID})
		if err != nil || got.ID == "" {
			continue // stale sidecar entry; hydrate keeps correctness
		}
		if status, _ := got.Metadata["status"].(string); status != "" && status != "active" {
			continue
		}
		got.Score = hit.Score
		peers = append(peers, got)
	}
	return peers
}

func (f *Facade) likePeers(ctx context.Context, in RememberInput) ([]MemoryHit, error) {
	return f.session.Recall(ctx, f.peerRecallQuery(in))
}

func (f *Facade) peerRecallQuery(in RememberInput) RecallQuery {
	return RecallQuery{
		Scope:   in.Scope,
		ScopeID: in.ScopeID,
		Source:  SourceUnits,
		Query:   in.Content,
		Limit:   f.semanticConflictK,
	}
}

// syncUpsert embeds and indexes a freshly written unit when vectorReady.
// E2 decouples upsert from the D2 (ToolSemanticConflict) gate: successful units
// are indexed whenever the sidecar + embedder are available and the breaker is
// not tripped. Peer discovery / semantic conflict resolution remain D2-gated.
// Best-effort: index drift is tolerated because hydrate re-checks active status.
func (f *Facade) syncUpsert(ctx context.Context, in RememberInput, unitID string) {
	if unitID == "" || !f.vectorReady() {
		return
	}
	vec := f.embedOne(ctx, in.AgentID, in.Content)
	if vec == nil {
		return
	}
	f.syncUpsertVec(ctx, in, unitID, vec)
}

// syncUpsertVec indexes unitID using an already-computed vector, skipping a
// redundant embedding round-trip. Callers must have passed the same gates as syncUpsert.
func (f *Facade) syncUpsertVec(ctx context.Context, in RememberInput, unitID string, vec []float32) {
	if unitID == "" || len(vec) == 0 || f.unitVectors == nil {
		return
	}
	_ = f.unitVectors.Upsert(ctx, UnitVectorEntry{
		Scope:   in.Scope,
		ScopeID: in.ScopeID,
		UnitID:  unitID,
		Vector:  vec,
	})
}

// syncDelete drops vectors for ids that are no longer active. Independent of the
// D2 gate and the embed breaker (no embedding required).
func (f *Facade) syncDelete(ctx context.Context, scope Scope, scopeID string, unitIDs ...string) {
	if f.unitVectors == nil || len(unitIDs) == 0 {
		return
	}
	_ = f.unitVectors.Delete(ctx, scope, scopeID, unitIDs...)
}
