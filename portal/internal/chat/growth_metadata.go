package chat

import "github.com/sixath/framework/growth"

// MergeGrowthReviewMetadata 浅拷贝 md 并打上复盘标志；供复盘内子 Agent Run 与主对话隔离成长计数（spec §4.3 / A3）。
func MergeGrowthReviewMetadata(md map[string]any) map[string]any {
	return growth.MergeReviewMetadata(md)
}
