package runtime

import (
	"context"
	stderrors "errors"
	"strings"
	"time"
	"unicode/utf8"

	chatv1 "backend/api/chat/v1"
	"backend/api/common"
	"backend/internal/biz"
	"backend/internal/chatsse"
	portalchat "backend/internal/chat"
	pkgErrors "backend/internal/pkg/errors"
	"backend/internal/service"

	"github.com/go-kratos/kratos/v2/errors"
	"github.com/google/uuid"
	"google.golang.org/protobuf/types/known/structpb"
)

type chatBackend interface {
	CreateSession(ctx context.Context, agentID, title, parentSessionID string) (*biz.ChatSession, error)
	GetSession(ctx context.Context, id string) (*biz.ChatSession, error)
	ListSessions(ctx context.Context, agentID string, q string, page, pageSize int32, includePreview bool) ([]*biz.ChatSession, int, error)
	ListAllSessions(ctx context.Context, page, pageSize int32, includePreview bool) ([]*biz.ChatSession, int, error)
	UpdateSession(ctx context.Context, id string, title string) (*biz.ChatSession, error)
	DeleteSession(ctx context.Context, id string) error
	ListMessages(ctx context.Context, sessionID string, limit int) ([]*biz.ChatMessage, error)
	SearchSessions(ctx context.Context, query, agentIDFilter string, limit int) ([]biz.SearchHit, string, error)
}

type peerBackend interface {
	Resolve(ctx context.Context, in biz.ChannelPeerResolveInput) (*biz.ChannelPeerResolveResult, error)
	DeleteBinding(ctx context.Context, channelID, peerID string) error
}

type channelReader interface {
	GetByChannelID(ctx context.Context, channelID string) (*biz.ChannelMeta, error)
}

type agentReader interface {
	GetForSession(ctx context.Context, id string) (*biz.AgentMeta, error)
}

type sessionBackend interface {
	GetByID(ctx context.Context, id string) (*biz.ChatSession, error)
}

// RewindBackend soft-hides messages from an anchor (ChatService satisfies this).
type RewindBackend interface {
	RewindToMessage(ctx context.Context, sessionID, messageID string) (*service.RewindResult, error)
}

// TurnBackend runs a chat turn (ChatService satisfies this).
type TurnBackend interface {
	SendMessage(ctx context.Context, req *chatv1.SendMessageRequest) (*chatv1.MessageReply, error)
	SendMessageStream(ctx context.Context, req *chatv1.SendMessageRequest) (<-chan service.ChatStreamEvent, string, error)
	SaveAssistantMessage(ctx context.Context, sessionID, content string, metadata map[string]any) (*chatv1.MessageReply, error)
}

type routeBackend interface {
	Route(ctx context.Context, in biz.AgentRouteInput) (*biz.AgentRouteResult, error)
}

// Service exposes Portal Runtime session operations for the inbound Gateway.
type Service struct {
	chat     chatBackend
	peer     peerBackend
	channels channelReader
	agents   agentReader
	sessions sessionBackend
	rewinder RewindBackend
	turns    TurnBackend
	router   routeBackend
}

// NewService wires ChatUsecase + ChannelPeerUsecase (+ channel/agent readers + session repo ACL + rewind/turn backends).
func NewService(chatUC *biz.ChatUsecase, peerUC *biz.ChannelPeerUsecase, channelUC *biz.ChannelUsecase, agentUC *biz.AgentUsecase, sessions biz.ChatSessionRepo, rewinder RewindBackend, turns TurnBackend, routeUC *biz.AgentRouteUsecase) *Service {
	return &Service{
		chat:     chatUC,
		peer:     peerUC,
		channels: channelUC,
		agents:   agentUC,
		sessions: sessions,
		rewinder: rewinder,
		turns:    turns,
		router:   routeUC,
	}
}

type resolveRequest struct {
	ChannelID string `json:"channel_id"`
	PeerID    string `json:"peer_id"`
	AgentID   string `json:"agent_id"`
	ForceNew  bool   `json:"force_new"`
	Reason    string `json:"reason"`
}

type resolveReply struct {
	SessionID string `json:"session_id"`
	AgentID   string `json:"agent_id"`
	UserID    string `json:"user_id"`
	Created   bool   `json:"created"`
}

type createSessionRequest struct {
	AgentID         string `json:"agent_id"`
	Title           string `json:"title"`
	ParentSessionID string `json:"parent_session_id"`
}

type updateSessionRequest struct {
	Title string `json:"title"`
}

type rewindRequest struct {
	MessageID string `json:"message_id"`
}

type turnRequest struct {
	SessionID       string                      `json:"session_id"`
	Content         string                      `json:"content"`
	ReplyMode       string                      `json:"reply_mode"`
	ChannelID       string                      `json:"channel_id"`
	PeerID          string                      `json:"peer_id"`
	CorrelationID   string                      `json:"correlation_id"`
	IdempotencyKey  string                      `json:"idempotency_key"`
	InputResponse   *portalchat.InputResponse   `json:"input_response"`
	ConfirmResponse *portalchat.ConfirmResponse `json:"confirm_response"`
}

type turnFinalReply struct {
	CorrelationID string `json:"correlation_id"`
	Status        string `json:"status"`
	Content       string `json:"content,omitempty"`
	Error         string `json:"error,omitempty"`
}

type channelAgentItem struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

type channelAgentsReply struct {
	DefaultAgent        string             `json:"default_agent"`
	Agents              []channelAgentItem `json:"agents"`
	AutoRouteEnabled    bool               `json:"auto_route_enabled"`
	AutoRouteMention    bool               `json:"auto_route_mention"`
	AutoRouteClassifier bool               `json:"auto_route_classifier"`
}

const channelAgentDescriptionMaxRunes = 200

func truncateRunes(s string, max int) string {
	if max <= 0 || s == "" {
		return s
	}
	if utf8.RuneCountInString(s) <= max {
		return s
	}
	runes := []rune(s)
	return string(runes[:max])
}

// turnFinalTimeout caps reply_mode=final; overridable in tests.
var turnFinalTimeout = 120 * time.Second

func (s *Service) resolve(ctx context.Context, req resolveRequest) (*resolveReply, error) {
	out, err := s.peer.Resolve(ctx, biz.ChannelPeerResolveInput{
		ChannelID: req.ChannelID,
		PeerID:    req.PeerID,
		AgentID:   req.AgentID,
		ForceNew:  req.ForceNew,
		Reason:    req.Reason,
	})
	if err != nil {
		return nil, err
	}
	return &resolveReply{
		SessionID: out.SessionID,
		AgentID:   out.AgentID,
		UserID:    biz.PeerUserID(req.ChannelID, req.PeerID),
		Created:   out.Created,
	}, nil
}

func (s *Service) deleteBinding(ctx context.Context, channelID, peerID string) error {
	channelID = strings.TrimSpace(channelID)
	peerID = strings.TrimSpace(peerID)
	if channelID == "" || peerID == "" {
		return errors.BadRequest("INVALID_ARGUMENT", "channel_id and peer_id are required")
	}
	if s.peer == nil {
		return errors.InternalServer("UNAVAILABLE", "peer resolver unavailable")
	}
	err := s.peer.DeleteBinding(ctx, channelID, peerID)
	if err != nil && stderrors.Is(err, pkgErrors.ErrNotFound) {
		return nil // idempotent unbind
	}
	return err
}

func (s *Service) listChannelAgents(ctx context.Context, channelID string) (*channelAgentsReply, error) {
	channelID = strings.TrimSpace(channelID)
	if channelID == "" {
		return nil, errors.BadRequest("INVALID_ARGUMENT", "channel_id is required")
	}
	if s.channels == nil {
		return nil, errors.InternalServer("UNAVAILABLE", "channel store unavailable")
	}
	ch, err := s.channels.GetByChannelID(ctx, channelID)
	if err != nil {
		return nil, err
	}
	ids := ch.AllowedAgents
	if len(ids) == 0 {
		if def := strings.TrimSpace(ch.DefaultAgent); def != "" {
			ids = []string{def}
		}
	}
	agents := make([]channelAgentItem, 0, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		name := ""
		desc := ""
		if s.agents != nil {
			meta, getErr := s.agents.GetForSession(ctx, id)
			if getErr != nil {
				// Skip missing agents; surface unexpected store errors.
				if stderrors.Is(getErr, biz.ErrAgentNotFound) || stderrors.Is(getErr, pkgErrors.ErrNotFound) || errors.IsNotFound(getErr) {
					continue
				}
				return nil, getErr
			}
			if meta != nil {
				name = meta.Name
				desc = truncateRunes(meta.Description, channelAgentDescriptionMaxRunes)
			}
		}
		agents = append(agents, channelAgentItem{ID: id, Name: name, Description: desc})
	}
	return &channelAgentsReply{
		DefaultAgent:        ch.DefaultAgent,
		Agents:              agents,
		AutoRouteEnabled:    ch.AutoRouteEnabled,
		AutoRouteMention:    ch.AutoRouteMention,
		AutoRouteClassifier: ch.AutoRouteClassifier,
	}, nil
}

type routeRequest struct {
	Text   string `json:"text"`
	PeerID string `json:"peer_id"`
}

type routeReply struct {
	AgentID    string `json:"agent_id"`
	Confidence string `json:"confidence"`
	Source     string `json:"source"`
	Reason     string `json:"reason"`
}

func (s *Service) routeChannel(ctx context.Context, channelID string, req routeRequest) (*routeReply, error) {
	channelID = strings.TrimSpace(channelID)
	if channelID == "" {
		return nil, errors.BadRequest("INVALID_ARGUMENT", "channel_id is required")
	}
	if s.router == nil {
		return nil, errors.InternalServer("UNAVAILABLE", "route classifier unavailable")
	}
	out, err := s.router.Route(ctx, biz.AgentRouteInput{
		ChannelID: channelID,
		PeerID:    req.PeerID,
		Text:      req.Text,
	})
	if err != nil {
		return nil, err
	}
	return &routeReply{
		AgentID:    out.AgentID,
		Confidence: string(out.Confidence),
		Source:     string(out.Source),
		Reason:     out.Reason,
	}, nil
}

func (s *Service) createSession(ctx context.Context, req createSessionRequest) (*chatv1.SessionReply, error) {
	if err := requireRuntimeUser(ctx); err != nil {
		return nil, err
	}
	session, err := s.chat.CreateSession(ctx, req.AgentID, req.Title, req.ParentSessionID)
	if err != nil {
		return nil, err
	}
	return sessionToReply(session), nil
}

func (s *Service) listAllSessions(ctx context.Context, page, pageSize int32, includePreview bool) (*chatv1.ListAllSessionsReply, error) {
	if err := requireRuntimeUser(ctx); err != nil {
		return nil, err
	}
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 50
	}
	items, total, err := s.chat.ListAllSessions(ctx, page, pageSize, includePreview)
	if err != nil {
		return nil, err
	}
	replies := make([]*chatv1.SessionReply, len(items))
	for i, sess := range items {
		replies[i] = sessionToReply(sess)
	}
	return &chatv1.ListAllSessionsReply{
		Ret:   &common.BaseResponse{Code: 0, Message: "ok"},
		Items: replies,
		Total: int32(total),
	}, nil
}

func (s *Service) listSessionsByAgent(ctx context.Context, agentID string, q string, page, pageSize int32, includePreview bool) (*chatv1.ListSessionsReply, error) {
	if err := requireRuntimeUser(ctx); err != nil {
		return nil, err
	}
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 10
	}
	items, total, err := s.chat.ListSessions(ctx, agentID, q, page, pageSize, includePreview)
	if err != nil {
		return nil, err
	}
	replies := make([]*chatv1.SessionReply, len(items))
	for i, sess := range items {
		replies[i] = sessionToReply(sess)
	}
	return &chatv1.ListSessionsReply{
		Ret:   &common.BaseResponse{Code: 0, Message: "ok"},
		Items: replies,
		Total: int32(total),
	}, nil
}

func (s *Service) getSession(ctx context.Context, id string) (*chatv1.SessionReply, error) {
	if err := s.requireSessionOwner(ctx, id); err != nil {
		return nil, err
	}
	session, err := s.chat.GetSession(ctx, id)
	if err != nil {
		return nil, err
	}
	return sessionToReply(session), nil
}

func (s *Service) updateSession(ctx context.Context, id string, title string) (*chatv1.SessionReply, error) {
	if err := s.requireSessionOwner(ctx, id); err != nil {
		return nil, err
	}
	session, err := s.chat.UpdateSession(ctx, id, title)
	if err != nil {
		return nil, err
	}
	return sessionToReply(session), nil
}

func (s *Service) deleteSession(ctx context.Context, id string) (*chatv1.DeleteSessionReply, error) {
	if err := s.requireSessionOwner(ctx, id); err != nil {
		return nil, err
	}
	if err := s.chat.DeleteSession(ctx, id); err != nil {
		return nil, err
	}
	return &chatv1.DeleteSessionReply{Ret: &common.BaseResponse{Code: 0, Message: "ok"}}, nil
}

func (s *Service) listMessages(ctx context.Context, sessionID string) (*chatv1.ListMessagesReply, error) {
	if err := s.requireSessionOwner(ctx, sessionID); err != nil {
		return nil, err
	}
	items, err := s.chat.ListMessages(ctx, sessionID, 100)
	if err != nil {
		return nil, err
	}
	replies := make([]*chatv1.MessageReply, len(items))
	for i, m := range items {
		replies[i] = messageToReply(m)
	}
	return &chatv1.ListMessagesReply{
		Ret:   &common.BaseResponse{Code: 0, Message: "ok"},
		Items: replies,
	}, nil
}

func (s *Service) searchSessions(ctx context.Context, query, agentID string, limit int) (*chatv1.SearchSessionsReply, error) {
	if err := requireRuntimeUser(ctx); err != nil {
		return nil, err
	}
	hits, msg, err := s.chat.SearchSessions(ctx, query, agentID, limit)
	if err != nil {
		return nil, err
	}
	replies := make([]*chatv1.SearchHitReply, len(hits))
	for i, h := range hits {
		replies[i] = &chatv1.SearchHitReply{
			SessionId:       h.SessionID,
			RootSessionId:   h.RootSessionID,
			AgentId:         h.AgentID,
			AgentName:       h.AgentName,
			Title:           h.Title,
			Preview:         h.Preview,
			MatchedSnippets: h.MatchedSnippets,
			UpdatedAt:       h.UpdatedAt.Format(time.RFC3339),
		}
	}
	return &chatv1.SearchSessionsReply{
		Ret:   &common.BaseResponse{Code: 0, Message: msg},
		Items: replies,
	}, nil
}

func (s *Service) rewindSession(ctx context.Context, sessionID, messageID string) (*service.RewindResult, error) {
	if err := s.requireSessionOwner(ctx, sessionID); err != nil {
		return nil, err
	}
	if s.rewinder == nil {
		return nil, errors.InternalServer("UNAVAILABLE", "rewind unavailable")
	}
	return s.rewinder.RewindToMessage(ctx, sessionID, messageID)
}

func (s *Service) bindTurnContext(ctx context.Context, req turnRequest) context.Context {
	if req.InputResponse != nil {
		ctx = portalchat.WithInputResponse(ctx, req.InputResponse)
	}
	if req.ConfirmResponse != nil {
		ctx = portalchat.WithConfirmResponse(ctx, req.ConfirmResponse)
	}
	return ctx
}

func (s *Service) correlationID(req turnRequest) string {
	if id := strings.TrimSpace(req.CorrelationID); id != "" {
		return id
	}
	return uuid.NewString()
}

func (s *Service) runFinalTurn(ctx context.Context, req turnRequest) (*turnFinalReply, error) {
	corr := s.correlationID(req)
	out := &turnFinalReply{CorrelationID: corr, Status: "failed"}
	if s.turns == nil {
		out.Error = "turn runner unavailable"
		return out, nil
	}
	if err := s.requireSessionOwner(ctx, req.SessionID); err != nil {
		return nil, err
	}
	runCtx, cancel := context.WithTimeout(s.bindTurnContext(ctx, req), turnFinalTimeout)
	defer cancel()

	chatReq := &chatv1.SendMessageRequest{SessionId: req.SessionID, Content: req.Content}
	ch, sessionID, err := s.turns.SendMessageStream(runCtx, chatReq)
	if err != nil {
		out.Error = err.Error()
		return out, nil
	}
	agg := chatsse.AggregateFinal(ch)
	// Timeout/cancel must fail even when partial chunks arrived and stream error was suppressed.
	if err := runCtx.Err(); err != nil {
		out.Status = "failed"
		out.Error = err.Error()
		if agg.Content != "" {
			out.Content = agg.Content
		}
		return out, nil
	}
	if agg.HITL || agg.Failed {
		out.Status = "failed"
		out.Error = agg.Error
		if out.Error == "" && agg.HITL {
			out.Error = "hitl required but reply_mode=final has no interactive surface"
		}
		if agg.Content != "" {
			out.Content = agg.Content
		}
		return out, nil
	}
	if _, err := s.turns.SaveAssistantMessage(context.WithoutCancel(ctx), sessionID, agg.Content, nil); err != nil {
		out.Error = err.Error()
		return out, nil
	}
	out.Status = "ok"
	out.Content = agg.Content
	out.Error = ""
	return out, nil
}

// startStreamTurn ACL-checks and starts SendMessageStream bound to ctx (disconnect cancels).
func (s *Service) startStreamTurn(ctx context.Context, req turnRequest) (<-chan service.ChatStreamEvent, string, error) {
	if s.turns == nil {
		return nil, "", errors.InternalServer("UNAVAILABLE", "turn runner unavailable")
	}
	if err := s.requireSessionOwner(ctx, req.SessionID); err != nil {
		return nil, "", err
	}
	runCtx := s.bindTurnContext(ctx, req)
	chatReq := &chatv1.SendMessageRequest{SessionId: req.SessionID, Content: req.Content}
	return s.turns.SendMessageStream(runCtx, chatReq)
}

func (s *Service) requireSessionOwner(ctx context.Context, sessionID string) error {
	if err := requireRuntimeUser(ctx); err != nil {
		return err
	}
	caller, _ := biz.CallerUserID(ctx)
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return biz.ErrSessionNotFound
	}
	if s.sessions == nil {
		return errors.InternalServer("INTERNAL", "session store unavailable")
	}
	sess, err := s.sessions.GetByID(ctx, sessionID)
	if err != nil {
		if stderrors.Is(err, pkgErrors.ErrNotFound) {
			return biz.ErrSessionNotFound
		}
		return err
	}
	if sess.UserID != caller {
		return errors.Forbidden("FORBIDDEN", "session does not belong to caller")
	}
	return nil
}

func requireRuntimeUser(ctx context.Context) error {
	caller, ok := biz.CallerUserID(ctx)
	if !ok || strings.TrimSpace(caller) == "" {
		return errors.Unauthorized("UNAUTHORIZED", "X-Sath-User-Id required")
	}
	return nil
}

func sessionToReply(s *biz.ChatSession) *chatv1.SessionReply {
	reply := &chatv1.SessionReply{
		Ret:       &common.BaseResponse{Code: 0, Message: "ok"},
		Id:        s.ID,
		AgentId:   s.AgentID,
		Title:     s.Title,
		CreatedAt: s.CreatedAt.Format(time.RFC3339),
		UpdatedAt: s.UpdatedAt.Format(time.RFC3339),
	}
	if s.ParentSessionID != "" {
		reply.ParentSessionId = &s.ParentSessionID
	}
	if s.Preview != "" {
		reply.Preview = &s.Preview
	}
	if s.AgentName != "" {
		reply.AgentName = &s.AgentName
	}
	return reply
}

func messageToReply(m *biz.ChatMessage) *chatv1.MessageReply {
	reply := &chatv1.MessageReply{
		Ret:       &common.BaseResponse{Code: 0, Message: "ok"},
		Id:        m.ID,
		SessionId: m.SessionID,
		Role:      m.Role,
		Content:   m.Content,
		CreatedAt: m.CreatedAt.Format(time.RFC3339),
	}
	if len(m.Metadata) > 0 {
		if st, err := structpb.NewStruct(m.Metadata); err == nil {
			reply.Metadata = st
		}
	}
	return reply
}
