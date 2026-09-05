package memory

import "strings"

// UnitKindFromMetadata returns kind from metadata (default fact).
func UnitKindFromMetadata(meta map[string]any) string {
	if meta == nil {
		return KindFact
	}
	if k, ok := meta["kind"].(string); ok {
		k = strings.ToLower(strings.TrimSpace(k))
		if k == KindProcedural {
			return KindProcedural
		}
		if k == KindFact || k == "" {
			return KindFact
		}
		return k
	}
	return KindFact
}

// KindMatchesFilter reports whether unitKind passes the recall/list Kind filter.
func KindMatchesFilter(unitKind, filter string) bool {
	unitKind = strings.ToLower(strings.TrimSpace(unitKind))
	if unitKind == "" {
		unitKind = KindFact
	}
	switch strings.ToLower(strings.TrimSpace(filter)) {
	case KindFilterAny:
		return true
	case KindProcedural: // KindFilterProcedural aliases the same value
		return unitKind == KindProcedural
	default: // fact-only
		return unitKind != KindProcedural
	}
}
