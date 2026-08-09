package wecom

import "fmt"

// FormatReplyCard builds the markdown reply shown when a turn succeeds.
func FormatReplyCard(asker, question, answer string) string {
	return fmt.Sprintf("发起人：%s\n问题：%s\n\n%s", asker, question, answer)
}

// FormatFailureCard builds the markdown reply shown when a turn fails.
func FormatFailureCard(asker, question, errMsg string) string {
	return fmt.Sprintf("发起人：%s\n问题：%s\n\n%s", asker, question, errMsg)
}
