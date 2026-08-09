package growth

// MetaGrowthReview 注入 agent.Request.Metadata 时，表示当前 Run 处于「成长复盘」子上下文，
// portal / hook 应跳过再次递增成长计数（spec §4.3）。取值推荐 bool true 或字符串 "1"/"true"。
const MetaGrowthReview = "sixath.growth_review"

// IsGrowthReviewMetadata 为真时表示请求来自复盘执行体，不得再触发成长工具/回合计数。
func IsGrowthReviewMetadata(md map[string]any) bool {
	if md == nil {
		return false
	}
	v, ok := md[MetaGrowthReview]
	if !ok || v == nil {
		return false
	}
	switch x := v.(type) {
	case bool:
		return x
	case string:
		return x == "1" || x == "true" || x == "yes" || x == "TRUE" || x == "YES"
	case float64:
		return x != 0
	case int:
		return x != 0
	case int64:
		return x != 0
	default:
		return false
	}
}

// MetaSkipGrowthReview 为 true 时跳过 Growth 复盘与用户画像计数（spec §5.7.1 cron 会话）。
const MetaSkipGrowthReview = "skip_growth_review"

// MetaSkipMemory 为 true 时跳过 memory 同步（spec §5.7.1 cron 会话）。
const MetaSkipMemory = "skip_memory"

// MetaRunKind 会话运行类型键（chat / cron）。
const MetaRunKind = "run_kind"

// MetaAllowCronCreate 是否允许 cronjob create。
const MetaAllowCronCreate = "allow_cron_create"

func metadataTruthy(md map[string]any, key string) bool {
	if md == nil {
		return false
	}
	v, ok := md[key]
	if !ok || v == nil {
		return false
	}
	switch x := v.(type) {
	case bool:
		return x
	case string:
		return x == "1" || x == "true" || x == "yes" || x == "TRUE" || x == "YES"
	case float64:
		return x != 0
	case int:
		return x != 0
	case int64:
		return x != 0
	default:
		return false
	}
}

// ShouldSkipGrowthReview 为真时不得触发成长工具/回合计数/复盘。
func ShouldSkipGrowthReview(md map[string]any) bool {
	return IsGrowthReviewMetadata(md) || metadataTruthy(md, MetaSkipGrowthReview)
}

// ShouldSkipMemory 为真时不得触发 session-delta memory 同步。
func ShouldSkipMemory(md map[string]any) bool {
	return metadataTruthy(md, MetaSkipMemory)
}

// MergeReviewMetadata 浅拷贝 md 并打上复盘标志，供复盘内子 Agent / ReAct Run 使用（A3）。
func MergeReviewMetadata(md map[string]any) map[string]any {
	out := make(map[string]any, len(md)+1)
	for k, v := range md {
		out[k] = v
	}
	out[MetaGrowthReview] = true
	return out
}
