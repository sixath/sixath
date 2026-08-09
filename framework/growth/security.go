package growth

import (
	"fmt"
	"strings"
)

// injectionMarkers 为写盘前拒绝的 prompt-injection 模式（与 runtime memory/skill_manage 共用）。
var injectionMarkers = []string{
	"ignore previous instructions",
	"ignore all previous",
	"disregard prior instructions",
	"you are now",
	"system prompt override",
	"developer message:",
	"<!-- hidden",
}

// ScanUserContent rejects obvious prompt-injection patterns in user-controlled write payloads.
func ScanUserContent(content string) error {
	lower := strings.ToLower(content)
	for _, m := range injectionMarkers {
		if strings.Contains(lower, m) {
			return fmt.Errorf("growth: content rejected by security scan (matched %q)", m)
		}
	}
	return nil
}
