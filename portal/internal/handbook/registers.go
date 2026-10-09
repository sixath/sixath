package handbook

import "sort"

const (
	RegTable    = "table"
	RegRoute    = "route"
	RegTopic    = "topic"
	RegCacheKey = "cache_key"

	AccessWrite = "write"
	AccessServe = "serve"
	AccessRead  = "read"
	AccessRef   = "ref"
)

// RegisterHit is one literal reference to shared state (table, route, topic, cache key).
type RegisterHit struct {
	Kind   string `json:"kind"`
	Name   string `json:"name"`
	Access string `json:"access"`
	Path   string `json:"path"`
	Line   int    `json:"line"`
}

func scanRegisters(rel, lang string, src []byte) []RegisterHit {
	return nil
}

var accessRank = map[string]int{AccessWrite: 0, AccessServe: 1, AccessRead: 2, AccessRef: 3}

// sortRegisters orders by kind and name, then writes before reads so renderers keep writers.
func sortRegisters(hs []RegisterHit) {
	sort.Slice(hs, func(i, j int) bool {
		a, b := hs[i], hs[j]
		if a.Kind != b.Kind {
			return a.Kind < b.Kind
		}
		if a.Name != b.Name {
			return a.Name < b.Name
		}
		if accessRank[a.Access] != accessRank[b.Access] {
			return accessRank[a.Access] < accessRank[b.Access]
		}
		if a.Path != b.Path {
			return a.Path < b.Path
		}
		return a.Line < b.Line
	})
}

func dedupeRegisters(hs []RegisterHit) []RegisterHit {
	seen := make(map[RegisterHit]bool, len(hs))
	out := hs[:0]
	for _, h := range hs {
		if !seen[h] {
			seen[h] = true
			out = append(out, h)
		}
	}
	return out
}
