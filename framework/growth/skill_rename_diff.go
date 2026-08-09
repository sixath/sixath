package growth

// DetectOneToOneRename 比较复盘前后技能名集合，仅在恰好移除一个且新增一个时返回 old→new。
// 无变更、多删多增或非对称变更时 ok=false。
func DetectOneToOneRename(before, after []string) (renames map[string]string, ok bool) {
	beforeSet := stringSet(before)
	afterSet := stringSet(after)

	var removed, added []string
	for name := range beforeSet {
		if _, hit := afterSet[name]; !hit {
			removed = append(removed, name)
		}
	}
	for name := range afterSet {
		if _, hit := beforeSet[name]; !hit {
			added = append(added, name)
		}
	}
	if len(removed) != 1 || len(added) != 1 {
		return nil, false
	}
	return map[string]string{removed[0]: added[0]}, true
}

func stringSet(names []string) map[string]struct{} {
	out := make(map[string]struct{}, len(names))
	for _, n := range names {
		if n == "" {
			continue
		}
		out[n] = struct{}{}
	}
	return out
}
