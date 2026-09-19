package tool

import (
	"context"
	"encoding/json"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/sixath/framework/datasource"
	"github.com/sixath/framework/executor"
	"github.com/sixath/framework/metadata"
)

const (
	esIndexCatalogTTL         = 5 * time.Minute
	esIndexSuggestLimit       = 15
	esIndexErrorUnresolved    = "unresolved"
	esIndexStar               = "*"
	esIndexAll                = "_all"
)

// ESIndexCatalog lists physical index names for a cluster (injected in tests).
// Empty indexPattern means the full non-system catalog; a concrete pattern is
// passed through to _cat/indices/{pattern}.
type ESIndexCatalog interface {
	ListIndexNames(ctx context.Context, clusterID, indexPattern string) ([]string, error)
}

type indexResolveStatus struct {
	Resolved bool
}

func indexPatternAlwaysResolved(index string) bool {
	s := strings.TrimSpace(index)
	return s == esIndexStar || strings.EqualFold(s, esIndexAll)
}

func resolveIndexPhysical(index string, physical []string) indexResolveStatus {
	if indexPatternAlwaysResolved(index) {
		return indexResolveStatus{Resolved: true}
	}
	for _, n := range filterPhysicalIndexNames(physical) {
		if n != "" {
			return indexResolveStatus{Resolved: true}
		}
	}
	return indexResolveStatus{Resolved: false}
}

func filterPhysicalIndexNames(names []string) []string {
	out := make([]string, 0, len(names))
	for _, n := range names {
		n = strings.TrimSpace(n)
		if n == "" || strings.HasPrefix(n, ".") || strings.ContainsAny(n, "%{[") {
			continue
		}
		out = append(out, n)
	}
	return out
}

func suggestIndexPatterns(requested string, catalog []string, defaultIndex string) []string {
	def := strings.TrimSpace(defaultIndex)
	seen := map[string]struct{}{}
	var out []string
	add := func(p string) {
		p = strings.TrimSpace(p)
		if p == "" {
			return
		}
		if _, ok := seen[p]; ok {
			return
		}
		seen[p] = struct{}{}
		out = append(out, p)
	}
	add(def)

	type scored struct {
		p     string
		score int
	}
	var rest []scored
	reqBase := indexPatternBase(requested)
	for _, p := range catalog {
		p = strings.TrimSpace(p)
		if p == "" || p == def {
			continue
		}
		rest = append(rest, scored{p: p, score: scoreIndexPattern(reqBase, p)})
	}
	sort.SliceStable(rest, func(i, j int) bool {
		if rest[i].score != rest[j].score {
			return rest[i].score > rest[j].score
		}
		return rest[i].p < rest[j].p
	})
	hasPositive := false
	for _, s := range rest {
		if s.score > 0 {
			hasPositive = true
			break
		}
	}
	if !hasPositive {
		for _, s := range rest {
			add(s.p)
			if len(out) >= esIndexSuggestLimit {
				return out
			}
		}
		return out
	}
	for _, s := range rest {
		if s.score <= 0 {
			continue
		}
		add(s.p)
		if len(out) >= esIndexSuggestLimit {
			break
		}
	}
	if len(out) < esIndexSuggestLimit {
		for _, s := range rest {
			add(s.p)
			if len(out) >= esIndexSuggestLimit {
				break
			}
		}
	}
	return out
}

func indexPatternBase(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.Index(s, ","); i >= 0 {
		s = s[:i]
	}
	s = strings.TrimSuffix(s, "-*")
	s = strings.TrimSuffix(s, "*")
	s = strings.TrimSuffix(s, "-")
	return strings.ToLower(s)
}

func scoreIndexPattern(reqBase, pattern string) int {
	pb := indexPatternBase(pattern)
	if reqBase == "" || pb == "" {
		return 0
	}
	if reqBase == pb {
		return 100
	}
	if strings.Contains(pb, reqBase) || strings.Contains(reqBase, pb) {
		return 80
	}
	rt := splitIndexTokens(reqBase)
	pt := splitIndexTokens(pb)
	if len(rt) == 0 || len(pt) == 0 {
		if levenshtein(reqBase, pb) <= 3 {
			return 20
		}
		return 0
	}
	inter := 0
	pset := map[string]struct{}{}
	for _, t := range pt {
		pset[t] = struct{}{}
	}
	for _, t := range rt {
		if _, ok := pset[t]; ok {
			inter++
		}
	}
	if inter == 0 {
		if levenshtein(reqBase, pb) <= 3 {
			return 20
		}
		return 0
	}
	return 40 + (20 * inter)
}

func splitIndexTokens(s string) []string {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" {
		return nil
	}
	parts := strings.FieldsFunc(s, func(r rune) bool {
		return r == '-' || r == '_' || r == '.'
	})
	var out []string
	for _, p := range parts {
		if p == "" || p == "*" {
			continue
		}
		out = append(out, p)
	}
	return out
}

type esRegistryIndexCatalog struct {
	reg   *datasource.Registry
	mu    sync.Mutex
	cache map[string]cachedIndexNames
}

type cachedIndexNames struct {
	names []string
	at    time.Time
}

func catalogFromReader(reader executor.Reader) ESIndexCatalog {
	ex, ok := reader.(*executor.ESExecutor)
	if !ok || ex == nil || ex.Registry == nil {
		return nil
	}
	return &esRegistryIndexCatalog{reg: ex.Registry, cache: map[string]cachedIndexNames{}}
}

func (c *esRegistryIndexCatalog) ListIndexNames(ctx context.Context, clusterID, indexPattern string) ([]string, error) {
	if c == nil || c.reg == nil {
		return nil, nil
	}
	pattern := strings.TrimSpace(indexPattern)
	if pattern == "" || indexPatternAlwaysResolved(pattern) {
		if names, ok := c.cachedFull(clusterID); ok {
			return names, nil
		}
		names, err := c.catIndices(ctx, clusterID, "")
		if err != nil {
			return nil, err
		}
		c.storeFull(clusterID, names)
		return names, nil
	}
	return c.catIndices(ctx, clusterID, pattern)
}

func (c *esRegistryIndexCatalog) cachedFull(clusterID string) ([]string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	got, ok := c.cache[clusterID]
	if !ok || time.Since(got.at) > esIndexCatalogTTL {
		return nil, false
	}
	return append([]string(nil), got.names...), true
}

func (c *esRegistryIndexCatalog) storeFull(clusterID string, names []string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.cache == nil {
		c.cache = map[string]cachedIndexNames{}
	}
	c.cache[clusterID] = cachedIndexNames{names: append([]string(nil), names...), at: time.Now()}
}

func (c *esRegistryIndexCatalog) catIndices(ctx context.Context, clusterID, pattern string) ([]string, error) {
	ds, err := c.reg.Get(clusterID)
	if err != nil || ds == nil {
		return nil, err
	}
	ep, ok := ds.(datasource.ESHTTPProvider)
	if !ok || ep.ESHTTP() == nil {
		return nil, nil
	}
	path := "/_cat/indices?format=json&h=index"
	if p := strings.TrimSpace(pattern); p != "" && !indexPatternAlwaysResolved(p) {
		path = "/_cat/indices/" + url.PathEscape(p) + "?format=json&h=index"
	}
	status, body, err := ep.ESHTTP().Do(ctx, "GET", path, nil)
	if err != nil {
		return nil, err
	}
	if status >= 400 {
		return nil, nil
	}
	var rows []struct {
		Index string `json:"index"`
	}
	if err := json.Unmarshal(body, &rows); err != nil {
		return nil, err
	}
	names := make([]string, 0, len(rows))
	for _, r := range rows {
		names = append(names, r.Index)
	}
	return filterPhysicalIndexNames(names), nil
}

func groupedPatterns(physical []string) []string {
	return metadata.GroupIndicesByPattern(filterPhysicalIndexNames(physical))
}

func attachSuggestedPatterns(ctx context.Context, payload map[string]any, cat ESIndexCatalog, clusterID, requested, defaultIndex string) {
	if payload == nil || cat == nil {
		return
	}
	all, err := cat.ListIndexNames(ctx, clusterID, "")
	if err != nil || len(all) == 0 {
		return
	}
	sugs := suggestIndexPatterns(requested, groupedPatterns(all), defaultIndex)
	if len(sugs) == 0 {
		return
	}
	payload["suggested_index_patterns"] = sugs
}

func indexUnresolvedPayload(ctx context.Context, cat ESIndexCatalog, cl ESLogCluster, index string) map[string]any {
	if cat == nil || indexPatternAlwaysResolved(index) {
		return nil
	}
	resolved := false
	sawOK := false
	for _, tok := range splitIndexParam(index) {
		if indexPatternAlwaysResolved(tok) {
			resolved = true
			continue
		}
		physical, err := cat.ListIndexNames(ctx, cl.ID, tok)
		if err != nil {
			return nil // fail open: catalog errors must not fake unresolved
		}
		sawOK = true
		if resolveIndexPhysical(tok, physical).Resolved {
			resolved = true
		}
	}
	if !sawOK || resolved {
		return nil
	}
	out := rcaErr("es_log_query", "index pattern matched no physical indices; 0 hits are not evidence of missing logs. Use a real index pattern from suggested_index_patterns.", ErrorPermanent)
	out["index_error"] = esIndexErrorUnresolved
	attachSuggestedPatterns(ctx, out, cat, cl.ID, index, cl.DefaultIndex)
	return out
}

func splitIndexParam(index string) []string {
	var out []string
	for _, p := range strings.Split(index, ",") {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	if len(out) == 0 {
		return []string{strings.TrimSpace(index)}
	}
	return out
}
