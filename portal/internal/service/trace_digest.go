package service

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/sixath/framework/agent"
	"github.com/sixath/framework/turntrace"
)

const defaultAsyncTraceTurnLimit = 5

// formatTraceDigest renders recent turn traces for async Growth review user content.
// Turns with any failed/blocked tool call are listed first; within a turn, failed
// calls precede successes. Returns "" when there is nothing to show.
func formatTraceDigest(traces []agent.TurnTrace) string {
	if len(traces) == 0 {
		return ""
	}
	failedTurns := make([]agent.TurnTrace, 0, len(traces))
	okTurns := make([]agent.TurnTrace, 0, len(traces))
	for _, tr := range traces {
		if turnHasFailure(tr) {
			failedTurns = append(failedTurns, tr)
		} else {
			okTurns = append(okTurns, tr)
		}
	}
	ordered := append(failedTurns, okTurns...)

	var b strings.Builder
	b.WriteString("# Turn traces\n")
	for _, tr := range ordered {
		failTag := ""
		if turnHasFailure(tr) {
			failTag = " (failed)"
		}
		fmt.Fprintf(&b, "## turn_seq=%d request_id=%s%s\n", tr.TurnSeq, tr.RequestID, failTag)
		calls := orderCallsFailuresFirst(tr.Calls)
		if len(calls) == 0 {
			b.WriteString("- (no tool calls)\n")
			continue
		}
		for _, c := range calls {
			if c.Error != "" || c.Blocked {
				errMsg := c.Error
				if errMsg == "" {
					errMsg = "blocked"
				}
				fmt.Fprintf(&b, "- tool=%s error=%s\n", c.ToolName, truncateDigestField(errMsg, 200))
			} else {
				preview := c.ResultPreview
				if preview == "" {
					preview = "ok"
				}
				fmt.Fprintf(&b, "- tool=%s result=%s\n", c.ToolName, truncateDigestField(preview, 120))
			}
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

func turnHasFailure(tr agent.TurnTrace) bool {
	for _, c := range tr.Calls {
		if c.Error != "" || c.Blocked {
			return true
		}
	}
	return false
}

func orderCallsFailuresFirst(calls []agent.TurnToolCall) []agent.TurnToolCall {
	if len(calls) <= 1 {
		return calls
	}
	failed := make([]agent.TurnToolCall, 0, len(calls))
	ok := make([]agent.TurnToolCall, 0, len(calls))
	for _, c := range calls {
		if c.Error != "" || c.Blocked {
			failed = append(failed, c)
		} else {
			ok = append(ok, c)
		}
	}
	return append(failed, ok...)
}

func truncateDigestField(s string, maxRunes int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.TrimSpace(s)
	if maxRunes <= 0 {
		return s
	}
	r := []rune(s)
	if len(r) <= maxRunes {
		return s
	}
	return string(r[:maxRunes]) + "…"
}

// asyncIncludeTurnTraces is growth.async_include_turn_traces (env stand-in); default true.
//
//	SATH_ASYNC_INCLUDE_TURN_TRACES — "0"/"false"/"no"/"off" disables.
func asyncIncludeTurnTraces() bool {
	v := strings.TrimSpace(os.Getenv("SATH_ASYNC_INCLUDE_TURN_TRACES"))
	if v == "" {
		return true
	}
	switch strings.ToLower(v) {
	case "0", "false", "no", "off":
		return false
	default:
		return true
	}
}

// asyncTraceTurnLimit is growth.async_trace_turn_limit (env stand-in); default 5.
//
//	SATH_ASYNC_TRACE_TURN_LIMIT — positive int; <=0 / unset → 5.
func asyncTraceTurnLimit() int {
	v := strings.TrimSpace(os.Getenv("SATH_ASYNC_TRACE_TURN_LIMIT"))
	if v == "" {
		return defaultAsyncTraceTurnLimit
	}
	n, err := strconv.Atoi(v)
	if err != nil || n <= 0 {
		return defaultAsyncTraceTurnLimit
	}
	return n
}

// fetchReviewTraceDigest loads recent active traces and formats them for review prompts.
func fetchReviewTraceDigest(ctx context.Context, store turntrace.Store, sessionID string) string {
	if store == nil || sessionID == "" || !asyncIncludeTurnTraces() {
		return ""
	}
	traces, err := store.ListBySession(ctx, sessionID, asyncTraceTurnLimit())
	if err != nil || len(traces) == 0 {
		return ""
	}
	return formatTraceDigest(traces)
}

func appendTraceDigest(transcript, digest string) string {
	digest = strings.TrimSpace(digest)
	if digest == "" {
		return transcript
	}
	transcript = strings.TrimRight(transcript, "\n")
	if transcript == "" {
		return digest
	}
	return transcript + "\n\n" + digest
}
