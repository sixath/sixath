package toolskill

import (
	"fmt"
	"strings"
)

// injectionMarkers 涓哄啓鐩樺墠鎷掔粷鐨?prompt-injection 妯″紡锛堜笌 runtime memory/skill_manage 鍏辩敤锛夈€?
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
