package service

import (
	"context"
	"sort"
	"strings"
	"time"

	agent "github.com/sixath/framework/harness"
	kratosErrors "github.com/go-kratos/kratos/v2/errors"
)

// InsightsReport is the read-only aggregation for GET .../insights.
type InsightsReport struct {
	AgentID      string           `json:"agent_id"`
	From         time.Time        `json:"from"`
	To           time.Time        `json:"to"`
	Turns        int              `json:"turns"`
	ToolCalls    int              `json:"tool_calls"`
	ErrorCalls   int              `json:"error_calls"`
	ErrorRate    float64          `json:"error_rate"`
	BlockedCalls int              `json:"blocked_calls"`
	TopTools     []InsightsTool   `json:"top_tools"`
	TopSessions  []InsightsSession `json:"top_sessions"`
	Truncated    bool             `json:"truncated,omitempty"`
}

type InsightsTool struct {
	Name   string `json:"name"`
	Calls  int    `json:"calls"`
	Errors int    `json:"errors"`
}

type InsightsSession struct {
	SessionID string `json:"session_id"`
	Turns     int    `json:"turns"`
	Errors    int    `json:"errors"`
}

const insightsScanCap = 5000

// AggregateInsights builds InsightsReport from TurnTraceStore (active traces only).
func AggregateInsights(traces []agent.TurnTrace, agentID string, from, to time.Time, truncated bool) InsightsReport {
	rep := InsightsReport{
		AgentID:   agentID,
		From:      from,
		To:        to,
		Truncated: truncated,
	}
	toolStats := map[string]*InsightsTool{}
	sessStats := map[string]*InsightsSession{}

	for _, tr := range traces {
		rep.Turns++
		sid := tr.SessionID
		ss := sessStats[sid]
		if ss == nil {
			ss = &InsightsSession{SessionID: sid}
			sessStats[sid] = ss
		}
		ss.Turns++
		for _, c := range tr.Calls {
			rep.ToolCalls++
			name := c.ToolName
			if name == "" {
				name = "(unknown)"
			}
			ts := toolStats[name]
			if ts == nil {
				ts = &InsightsTool{Name: name}
				toolStats[name] = ts
			}
			ts.Calls++
			if c.Blocked {
				rep.BlockedCalls++
			}
			if strings.TrimSpace(c.Error) != "" {
				rep.ErrorCalls++
				ts.Errors++
				ss.Errors++
			}
		}
	}
	if rep.ToolCalls > 0 {
		rep.ErrorRate = float64(rep.ErrorCalls) / float64(rep.ToolCalls)
	}
	for _, ts := range toolStats {
		rep.TopTools = append(rep.TopTools, *ts)
	}
	sort.Slice(rep.TopTools, func(i, j int) bool {
		if rep.TopTools[i].Calls != rep.TopTools[j].Calls {
			return rep.TopTools[i].Calls > rep.TopTools[j].Calls
		}
		return rep.TopTools[i].Name < rep.TopTools[j].Name
	})
	if len(rep.TopTools) > 20 {
		rep.TopTools = rep.TopTools[:20]
	}
	for _, ss := range sessStats {
		rep.TopSessions = append(rep.TopSessions, *ss)
	}
	sort.Slice(rep.TopSessions, func(i, j int) bool {
		if rep.TopSessions[i].Errors != rep.TopSessions[j].Errors {
			return rep.TopSessions[i].Errors > rep.TopSessions[j].Errors
		}
		if rep.TopSessions[i].Turns != rep.TopSessions[j].Turns {
			return rep.TopSessions[i].Turns > rep.TopSessions[j].Turns
		}
		return rep.TopSessions[i].SessionID < rep.TopSessions[j].SessionID
	})
	if len(rep.TopSessions) > 20 {
		rep.TopSessions = rep.TopSessions[:20]
	}
	return rep
}

func (s *ChatService) GetInsights(ctx context.Context, agentID string, from, to time.Time) (*InsightsReport, error) {
	agentID = strings.TrimSpace(agentID)
	if agentID == "" {
		return nil, kratosErrors.BadRequest("INVALID_ARGUMENT", "agent_id required")
	}
	if s.agentUC != nil {
		if _, err := s.agentUC.Get(ctx, agentID); err != nil {
			return nil, err
		}
	}
	if to.IsZero() {
		to = time.Now().UTC()
	}
	if from.IsZero() {
		from = to.Add(-7 * 24 * time.Hour)
	}
	var traces []agent.TurnTrace
	truncated := false
	if s.turnTraceStore != nil {
		var err error
		traces, err = s.turnTraceStore.ListByAgent(ctx, agentID, from, to, insightsScanCap)
		if err != nil {
			return nil, err
		}
		if len(traces) >= insightsScanCap {
			truncated = true
		}
	}
	rep := AggregateInsights(traces, agentID, from, to, truncated)
	return &rep, nil
}