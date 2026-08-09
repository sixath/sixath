package memory

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/neo4j/neo4j-go-driver/v5/neo4j"
)

// Neo4jConfig configures the Neo4j graph sidecar (P2-I).
type Neo4jConfig struct {
	URI      string
	Username string
	Password string
	Database string
}

// Neo4jGraphStore stores entity/relation sidecars in Neo4j.
type Neo4jGraphStore struct {
	driver   neo4j.DriverWithContext
	database string
	mu       sync.Mutex
	ensured  bool
}

// NewNeo4jGraphStore connects via the official Neo4j Go driver.
func NewNeo4jGraphStore(cfg Neo4jConfig) (*Neo4jGraphStore, error) {
	uri := strings.TrimSpace(cfg.URI)
	if uri == "" {
		return nil, fmt.Errorf("memory: neo4j uri required")
	}
	auth := neo4j.BasicAuth(strings.TrimSpace(cfg.Username), cfg.Password, "")
	d, err := neo4j.NewDriverWithContext(uri, auth)
	if err != nil {
		return nil, err
	}
	ctx := context.Background()
	if err := d.VerifyConnectivity(ctx); err != nil {
		_ = d.Close(ctx)
		return nil, err
	}
	return &Neo4jGraphStore{driver: d, database: strings.TrimSpace(cfg.Database)}, nil
}

func (s *Neo4jGraphStore) session(ctx context.Context) neo4j.SessionWithContext {
	cfg := neo4j.SessionConfig{AccessMode: neo4j.AccessModeWrite}
	if s.database != "" {
		cfg.DatabaseName = s.database
	}
	return s.driver.NewSession(ctx, cfg)
}

func (s *Neo4jGraphStore) ensureConstraints(ctx context.Context) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ensured {
		return
	}
	sess := s.session(ctx)
	defer sess.Close(ctx)
	_, _ = sess.Run(ctx, `
CREATE CONSTRAINT memory_entity_id IF NOT EXISTS
FOR (n:MemoryEntity) REQUIRE n.entity_id IS UNIQUE`, nil)
	s.ensured = true
}

func (s *Neo4jGraphStore) UpsertEntity(ctx context.Context, e Entity) error {
	if strings.TrimSpace(e.ID) == "" {
		e.ID = StableEntityID(e.Scope, e.ScopeID, e.Name)
	}
	if strings.TrimSpace(e.Name) == "" || strings.TrimSpace(e.ScopeID) == "" {
		return fmt.Errorf("memory: entity name and scope_id required")
	}
	s.ensureConstraints(ctx)
	sess := s.session(ctx)
	defer sess.Close(ctx)
	_, err := sess.Run(ctx, `
MERGE (n:MemoryEntity {entity_id: $id})
SET n.name = $name,
    n.entity_type = $etype,
    n.scope_type = $scope,
    n.scope_id = $scope_id,
    n.source_memory_id = $source,
    n.confidence = $conf,
    n.name_norm = $name_norm`, map[string]any{
		"id":        e.ID,
		"name":      strings.TrimSpace(e.Name),
		"etype":     strings.TrimSpace(e.Type),
		"scope":     string(e.Scope),
		"scope_id":  strings.TrimSpace(e.ScopeID),
		"source":    strings.TrimSpace(e.SourceMemoryID),
		"conf":      e.Confidence,
		"name_norm": NormalizeEntityName(e.Name),
	})
	return err
}

func (s *Neo4jGraphStore) UpsertRelation(ctx context.Context, r Relation) error {
	if strings.TrimSpace(r.SubjectID) == "" || strings.TrimSpace(r.ObjectID) == "" || strings.TrimSpace(r.Predicate) == "" {
		return fmt.Errorf("memory: relation subject, object, predicate required")
	}
	if strings.TrimSpace(r.ScopeID) == "" {
		return fmt.Errorf("memory: relation scope_id required")
	}
	s.ensureConstraints(ctx)
	sess := s.session(ctx)
	defer sess.Close(ctx)
	_, err := sess.Run(ctx, `
MATCH (a:MemoryEntity {entity_id: $sid}), (b:MemoryEntity {entity_id: $oid})
WHERE a.scope_type = $scope AND a.scope_id = $scope_id
  AND b.scope_type = $scope AND b.scope_id = $scope_id
MERGE (a)-[rel:REL {predicate: $pred, scope_type: $scope, scope_id: $scope_id}]->(b)
SET rel.source_memory_id = $source, rel.confidence = $conf`, map[string]any{
		"sid":      r.SubjectID,
		"oid":      r.ObjectID,
		"pred":     strings.TrimSpace(r.Predicate),
		"scope":    string(r.Scope),
		"scope_id": strings.TrimSpace(r.ScopeID),
		"source":   strings.TrimSpace(r.SourceMemoryID),
		"conf":     r.Confidence,
	})
	return err
}

func (s *Neo4jGraphStore) InvalidateByMemoryID(ctx context.Context, memoryUnitID string) error {
	memoryUnitID = strings.TrimSpace(memoryUnitID)
	if memoryUnitID == "" {
		return nil
	}
	sess := s.session(ctx)
	defer sess.Close(ctx)
	_, err := sess.Run(ctx, `
MATCH ()-[r:REL]->() WHERE r.source_memory_id = $id DELETE r`, map[string]any{"id": memoryUnitID})
	if err != nil {
		return err
	}
	_, err = sess.Run(ctx, `
MATCH (n:MemoryEntity) WHERE n.source_memory_id = $id DETACH DELETE n`, map[string]any{"id": memoryUnitID})
	return err
}

func (s *Neo4jGraphStore) Expand(ctx context.Context, q GraphExpandQuery) ([]GraphHit, error) {
	if len(q.SeedEntityIDs) == 0 || q.Hops <= 0 {
		return nil, nil
	}
	limit := q.Limit
	if limit <= 0 {
		limit = 20
	}
	hops := q.Hops
	if hops > 3 {
		hops = 3
	}
	sess := s.session(ctx)
	defer sess.Close(ctx)
	cypher := fmt.Sprintf(`
MATCH (seed:MemoryEntity)
WHERE seed.entity_id IN $seeds
  AND seed.scope_type = $scope AND seed.scope_id = $scope_id
MATCH path = (seed)-[:REL*1..%d]-(n:MemoryEntity)
WHERE n.scope_type = $scope AND n.scope_id = $scope_id
WITH DISTINCT n, length(path) AS dist
RETURN n.entity_id AS entity_id, n.name AS name, n.source_memory_id AS source,
       1.0 / (dist + 1.0) AS score
ORDER BY score DESC
LIMIT $limit`, hops)
	res, err := sess.Run(ctx, cypher, map[string]any{
		"seeds":    q.SeedEntityIDs,
		"scope":    string(q.Scope),
		"scope_id": strings.TrimSpace(q.ScopeID),
		"limit":    limit,
	})
	if err != nil {
		return nil, err
	}
	records, err := res.Collect(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]GraphHit, 0, len(records))
	for _, rec := range records {
		id, _ := rec.Get("entity_id")
		name, _ := rec.Get("name")
		src, _ := rec.Get("source")
		score, _ := rec.Get("score")
		hit := GraphHit{
			EntityID: asString(id),
			Name:     asString(name),
			Score:    asFloat64(score),
		}
		if sid := asString(src); sid != "" {
			hit.RelatedUnitIDs = []string{sid}
		}
		out = append(out, hit)
	}
	return out, nil
}

func (s *Neo4jGraphStore) MatchSeeds(ctx context.Context, scope Scope, scopeID, query string, limit int) ([]string, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, nil
	}
	if limit <= 0 {
		limit = 8
	}
	sess := s.session(ctx)
	defer sess.Close(ctx)
	res, err := sess.Run(ctx, `
MATCH (n:MemoryEntity)
WHERE n.scope_type = $scope AND n.scope_id = $scope_id
  AND ($q CONTAINS n.name_norm OR n.name_norm CONTAINS $q)
RETURN n.entity_id AS entity_id
LIMIT $limit`, map[string]any{
		"scope":    string(scope),
		"scope_id": strings.TrimSpace(scopeID),
		"q":        NormalizeEntityName(query),
		"limit":    limit,
	})
	if err != nil {
		return nil, err
	}
	records, err := res.Collect(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(records))
	for _, rec := range records {
		id, _ := rec.Get("entity_id")
		if s := asString(id); s != "" {
			out = append(out, s)
		}
	}
	return out, nil
}

func (s *Neo4jGraphStore) EntitiesBySourceMemoryIDs(ctx context.Context, scope Scope, scopeID string, unitIDs []string) ([]Entity, error) {
	if len(unitIDs) == 0 {
		return nil, nil
	}
	sess := s.session(ctx)
	defer sess.Close(ctx)
	res, err := sess.Run(ctx, `
MATCH (n:MemoryEntity)
WHERE n.scope_type = $scope AND n.scope_id = $scope_id
  AND n.source_memory_id IN $ids
RETURN n.entity_id AS entity_id, n.name AS name, n.entity_type AS etype,
       n.source_memory_id AS source, n.confidence AS conf`, map[string]any{
		"scope":    string(scope),
		"scope_id": strings.TrimSpace(scopeID),
		"ids":      unitIDs,
	})
	if err != nil {
		return nil, err
	}
	records, err := res.Collect(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]Entity, 0, len(records))
	for _, rec := range records {
		id, _ := rec.Get("entity_id")
		name, _ := rec.Get("name")
		etype, _ := rec.Get("etype")
		src, _ := rec.Get("source")
		conf, _ := rec.Get("conf")
		out = append(out, Entity{
			ID:             asString(id),
			Name:           asString(name),
			Type:           asString(etype),
			Scope:          scope,
			ScopeID:        scopeID,
			SourceMemoryID: asString(src),
			Confidence:     asFloat64(conf),
		})
	}
	return out, nil
}

func (s *Neo4jGraphStore) Close() error {
	if s == nil || s.driver == nil {
		return nil
	}
	return s.driver.Close(context.Background())
}

func asString(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case nil:
		return ""
	default:
		return fmt.Sprint(t)
	}
}

func asFloat64(v any) float64 {
	switch t := v.(type) {
	case float64:
		return t
	case float32:
		return float64(t)
	case int64:
		return float64(t)
	case int:
		return float64(t)
	default:
		return 0
	}
}
