package memory

import "sort"

const rrfK = 60

// rrfMerge fuses two ranked lists. Rank is 1-based index in each input slice
// (caller must pass post-hydrate vector hits). Ties: like present/rank → vector
// present/rank → unit_id. Dual-hit rows keep LIKE Content/Metadata/Scope/Source;
// Score is overwritten with the RRF sum.
func rrfMerge(like, vector []MemoryHit, limit int) []MemoryHit {
	if limit <= 0 {
		return nil
	}
	type acc struct {
		hit      MemoryHit
		score    float64
		likeRank int // 0 = absent
		vecRank  int
	}
	m := map[string]*acc{}
	order := make([]string, 0)
	add := func(list []MemoryHit, isLike bool) {
		for i, h := range list {
			if h.ID == "" {
				continue
			}
			a, ok := m[h.ID]
			if !ok {
				cp := h
				a = &acc{hit: cp}
				m[h.ID] = a
				order = append(order, h.ID)
			}
			rank := i + 1
			a.score += 1.0 / float64(rrfK+rank)
			if isLike {
				if a.likeRank == 0 {
					a.likeRank = rank
					a.hit.Content = h.Content
					a.hit.Metadata = h.Metadata
					a.hit.Scope = h.Scope
					a.hit.Source = h.Source
				}
			} else if a.vecRank == 0 {
				a.vecRank = rank
				if a.likeRank == 0 {
					a.hit = h
				}
			}
		}
	}
	add(like, true)
	add(vector, false)

	ids := append([]string{}, order...)
	sort.SliceStable(ids, func(i, j int) bool {
		ai, aj := m[ids[i]], m[ids[j]]
		if ai.score != aj.score {
			return ai.score > aj.score
		}
		li, lj := ai.likeRank, aj.likeRank
		if (li == 0) != (lj == 0) {
			return li != 0
		}
		if li != 0 && li != lj {
			return li < lj
		}
		vi, vj := ai.vecRank, aj.vecRank
		if (vi == 0) != (vj == 0) {
			return vi != 0
		}
		if vi != 0 && vi != vj {
			return vi < vj
		}
		return ids[i] < ids[j]
	})
	if len(ids) > limit {
		ids = ids[:limit]
	}
	out := make([]MemoryHit, 0, len(ids))
	for _, id := range ids {
		h := m[id].hit
		h.Score = m[id].score
		out = append(out, h)
	}
	return out
}
