package adapter

import (
	"context"
	"errors"
	"log"
	"strings"

	"github.com/sixath/gateway/internal/mention"
	"github.com/sixath/gateway/internal/runtimeclient"
	"github.com/sixath/gateway/internal/session"
)

type autoRoutePlan struct {
	AgentID  string // empty => do not pin agent on Resolve
	Reason   string
	TurnText string
	Source   string // mention|classifier|none
}

func prepareAutoRoute(ctx context.Context, rt *runtimeclient.Client, channelID, peerID, text string) autoRoutePlan {
	plan := autoRoutePlan{TurnText: text, Source: "none"}
	if rt == nil {
		return plan
	}

	list, listErr := rt.ListChannelAgents(ctx, channelID)
	if listErr == nil && list != nil && !list.AutoRouteEnabled {
		return plan
	}

	if listErr == nil && list != nil && list.AutoRouteMention {
		if hit := mention.Parse(text, toMentionCandidates(list)); hit.Hit {
			plan.AgentID = hit.AgentID
			plan.TurnText = hit.Stripped
			plan.Reason = "auto_mention"
			plan.Source = "mention"
			return plan
		}
	}

	classifierOn := listErr != nil || (list != nil && list.AutoRouteClassifier)
	if !classifierOn {
		return plan
	}

	reply, err := rt.RouteChannel(ctx, channelID, runtimeclient.RouteRequest{
		Text:   text,
		PeerID: peerID,
	})
	if err != nil {
		log.Printf("auto-route classifier channel=%s: %v", channelID, err)
		return plan
	}
	if reply != nil && strings.EqualFold(strings.TrimSpace(reply.Confidence), "high") {
		if id := strings.TrimSpace(reply.AgentID); id != "" {
			plan.AgentID = id
			plan.Reason = "auto_classifier"
			plan.Source = "classifier"
		}
	}
	return plan
}

func toMentionCandidates(list *runtimeclient.ChannelAgentsReply) []mention.Candidate {
	if list == nil {
		return nil
	}
	out := make([]mention.Candidate, 0, len(list.Agents))
	for _, a := range list.Agents {
		out = append(out, mention.Candidate{ID: a.ID, Name: a.Name})
	}
	return out
}

func isAgentBound(err error) bool {
	var he *runtimeclient.HTTPError
	if !errors.As(err, &he) || he == nil {
		return false
	}
	return he.StatusCode == 409 && strings.Contains(string(he.Body), "AGENT_BOUND")
}

// resolveMaybeRebind resolves onto desiredAgent; on AGENT_BOUND conflict, force_new rebind.
func resolveMaybeRebind(
	ctx context.Context,
	sessions *session.Router,
	channelID, peerID, agentID, reason string,
) (*runtimeclient.ResolveReply, error) {
	if sessions == nil {
		return nil, errors.New("session router not configured")
	}
	sessions.Invalidate(channelID, peerID)
	resolved, err := sessions.Resolve(ctx, "", runtimeclient.ResolveRequest{
		ChannelID: channelID,
		PeerID:    peerID,
		AgentID:   agentID,
		ForceNew:  false,
		Reason:    reason,
	})
	if err == nil {
		return resolved, nil
	}
	if !isAgentBound(err) {
		return nil, err
	}
	sessions.Invalidate(channelID, peerID)
	resolved, err = sessions.Resolve(ctx, "", runtimeclient.ResolveRequest{
		ChannelID: channelID,
		PeerID:    peerID,
		AgentID:   agentID,
		ForceNew:  true,
		Reason:    reason,
	})
	if err != nil {
		return nil, err
	}
	sessions.Invalidate(channelID, peerID)
	return resolved, nil
}
