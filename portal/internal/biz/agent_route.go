package biz

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	pkgErrors "backend/internal/pkg/errors"

	kratosErrors "github.com/go-kratos/kratos/v2/errors"
)

const (
	RouteConfidenceHigh RouteConfidence = "high"
	RouteConfidenceLow  RouteConfidence = "low"

	RouteSourceClassifier RouteSource = "classifier"
	RouteSourceDefault    RouteSource = "default"
	RouteSourceCurrent    RouteSource = "current"

	defaultRouteTimeout           = 3 * time.Second
	routeCandidateDescriptionMax  = 200
)

// RouteConfidence is classifier confidence: high | low.
type RouteConfidence string

// RouteSource explains how agent_id was chosen: classifier | default | current.
type RouteSource string

// AgentRouteInput is the input for message-level agent routing.
type AgentRouteInput struct {
	ChannelID string
	PeerID    string // optional
	Text      string
}

// AgentRouteResult is the outcome of Route.
type AgentRouteResult struct {
	AgentID    string
	Confidence RouteConfidence
	Source     RouteSource
	Reason     string
}

// RouteCandidate is one allowlisted agent for the classifier prompt.
type RouteCandidate struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

// RouteCompleter runs a single LLM completion for the classifier.
type RouteCompleter interface {
	Complete(ctx context.Context, prompt string) (string, error)
}

// agentRouteAgentReader loads agent metadata for classifier candidates.
type agentRouteAgentReader interface {
	GetForSession(ctx context.Context, id string) (*AgentMeta, error)
}

// AgentRouteUsecase classifies which allowlisted agent should handle a message.
type AgentRouteUsecase struct {
	channels ChannelRepo
	peers    ChannelPeerSessionRepo
	agents   agentRouteAgentReader
	complete RouteCompleter // nil => always fail-open without LLM
	timeout  time.Duration  // default 3s
}

// NewAgentRouteUsecase creates an AgentRouteUsecase.
func NewAgentRouteUsecase(
	channels ChannelRepo,
	peers ChannelPeerSessionRepo,
	agents agentRouteAgentReader,
	complete RouteCompleter,
	timeout time.Duration,
) *AgentRouteUsecase {
	if timeout <= 0 {
		timeout = defaultRouteTimeout
	}
	return &AgentRouteUsecase{
		channels: channels,
		peers:    peers,
		agents:   agents,
		complete: complete,
		timeout:  timeout,
	}
}

type classifierReply struct {
	AgentID    string `json:"agent_id"`
	Confidence string `json:"confidence"`
}

// Route picks an allowlisted agent for the user message (fail-open on ambiguity).
func (uc *AgentRouteUsecase) Route(ctx context.Context, in AgentRouteInput) (*AgentRouteResult, error) {
	channelID := strings.TrimSpace(in.ChannelID)
	if channelID == "" {
		return nil, kratosErrors.BadRequest("INVALID_ARGUMENT", "channel_id is required")
	}
	if uc.channels == nil {
		return nil, kratosErrors.InternalServer("UNAVAILABLE", "channel store unavailable")
	}

	ch, err := uc.channels.GetByChannelID(ctx, channelID)
	if err != nil {
		if errors.Is(err, pkgErrors.ErrNotFound) {
			return nil, ErrChannelNotFound
		}
		return nil, err
	}

	ids := ch.AllowedAgents
	if len(ids) == 0 {
		if def := strings.TrimSpace(ch.DefaultAgent); def != "" {
			ids = []string{def}
		}
	}
	candidates := make([]RouteCandidate, 0, len(ids))
	idSet := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if _, ok := idSet[id]; ok {
			continue
		}
		idSet[id] = struct{}{}
		c := RouteCandidate{ID: id}
		if uc.agents != nil {
			meta, getErr := uc.agents.GetForSession(ctx, id)
			if getErr == nil && meta != nil {
				c.Name = meta.Name
				c.Description = truncateRouteDesc(meta.Description, routeCandidateDescriptionMax)
			}
		}
		candidates = append(candidates, c)
	}
	if len(candidates) == 0 {
		return nil, kratosErrors.BadRequest("INVALID_ARGUMENT", "channel has no default or allowed agents")
	}
	if len(candidates) == 1 {
		return &AgentRouteResult{
			AgentID:    candidates[0].ID,
			Confidence: RouteConfidenceHigh,
			Source:     RouteSourceDefault,
			Reason:     "single_candidate",
		}, nil
	}

	current := ""
	peerID := strings.TrimSpace(in.PeerID)
	if peerID != "" && uc.peers != nil {
		binding, getErr := uc.peers.Get(ctx, channelID, peerID)
		if getErr != nil && !errors.Is(getErr, pkgErrors.ErrNotFound) {
			return nil, getErr
		}
		if getErr == nil && binding != nil {
			current = strings.TrimSpace(binding.AgentID)
		}
	}

	failOpen := func(reason string) *AgentRouteResult {
		if current != "" {
			return &AgentRouteResult{
				AgentID:    current,
				Confidence: RouteConfidenceLow,
				Source:     RouteSourceCurrent,
				Reason:     reason,
			}
		}
		def := strings.TrimSpace(ch.DefaultAgent)
		if def == "" {
			def = candidates[0].ID
		}
		return &AgentRouteResult{
			AgentID:    def,
			Confidence: RouteConfidenceLow,
			Source:     RouteSourceDefault,
			Reason:     reason,
		}
	}

	if uc.complete == nil {
		return failOpen("completer_unavailable"), nil
	}

	prompt, err := buildRoutePrompt(candidates, in.Text)
	if err != nil {
		return failOpen("prompt_build_failed"), nil
	}

	cctx, cancel := context.WithTimeout(ctx, uc.timeout)
	defer cancel()
	raw, err := uc.complete.Complete(cctx, prompt)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) || cctx.Err() == context.DeadlineExceeded {
			return failOpen("classifier_timeout"), nil
		}
		return failOpen("classifier_error"), nil
	}

	parsed, ok := parseClassifierReply(raw)
	if !ok {
		return failOpen("bad_json"), nil
	}
	if parsed.Confidence != string(RouteConfidenceHigh) {
		return failOpen("low_confidence"), nil
	}
	agentID := strings.TrimSpace(parsed.AgentID)
	if _, ok := idSet[agentID]; !ok {
		return failOpen("agent_not_allowlisted"), nil
	}
	return &AgentRouteResult{
		AgentID:    agentID,
		Confidence: RouteConfidenceHigh,
		Source:     RouteSourceClassifier,
		Reason:     "classifier_high",
	}, nil
}

func buildRoutePrompt(candidates []RouteCandidate, text string) (string, error) {
	b, err := json.Marshal(candidates)
	if err != nil {
		return "", err
	}
	var sb strings.Builder
	sb.WriteString("Pick the best agent for the user message. Reply ONLY JSON: {\"agent_id\":\"<uuid>\",\"confidence\":\"high\"|\"low\"}\n")
	sb.WriteString("Candidates:\n")
	sb.Write(b)
	sb.WriteString("\nUser:\n")
	sb.WriteString(text)
	return sb.String(), nil
}

func parseClassifierReply(raw string) (classifierReply, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return classifierReply{}, false
	}
	// Tolerate optional markdown fences.
	if i := strings.Index(raw, "{"); i >= 0 {
		if j := strings.LastIndex(raw, "}"); j > i {
			raw = raw[i : j+1]
		}
	}
	var out classifierReply
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return classifierReply{}, false
	}
	if strings.TrimSpace(out.AgentID) == "" {
		return classifierReply{}, false
	}
	conf := strings.ToLower(strings.TrimSpace(out.Confidence))
	if conf != string(RouteConfidenceHigh) && conf != string(RouteConfidenceLow) {
		return classifierReply{}, false
	}
	out.Confidence = conf
	return out, true
}

func truncateRouteDesc(s string, max int) string {
	if max <= 0 || s == "" {
		return s
	}
	if utf8.RuneCountInString(s) <= max {
		return s
	}
	return string([]rune(s)[:max])
}
