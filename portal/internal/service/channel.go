package service

import (
	"bytes"
	"context"
	"encoding/json"
	stderrors "errors"
	"net/http"
	urlpkg "net/url"
	"strings"
	"time"

	agentv1 "backend/api/agent/v1"
	channelv1 "backend/api/channel/v1"
	"backend/internal/biz"
	"backend/internal/channel"
	pkgErrors "backend/internal/pkg/errors"

	"github.com/go-kratos/kratos/v2/errors"
	"github.com/go-kratos/kratos/v2/log"
	"google.golang.org/protobuf/types/known/structpb"
)

const wecomWebhookURLPrefix = "https://qyapi.weixin.qq.com/cgi-bin/webhook/send"

func isValidWeComWebhookURL(url string) bool {
	return strings.HasPrefix(url, wecomWebhookURLPrefix)
}

func maskWeComWebhookURL(url string) string {
	if url == "" {
		return ""
	}
	suffix := last4Chars(url)
	if u, err := urlpkg.Parse(url); err == nil {
		if key := u.Query().Get("key"); key != "" {
			suffix = last4Chars(key)
		}
	}
	return "***" + suffix
}

func last4Chars(s string) string {
	if len(s) <= 4 {
		return s
	}
	return s[len(s)-4:]
}

// ChannelService implements channel.v1.ChannelHTTPServer
type ChannelService struct {
	uc          *biz.ChannelUsecase
	runtimeRepo biz.ChannelRuntimeRepo
	agentSvc    *AgentService
	log         *log.Helper
}

// NewChannelService creates a ChannelService
func NewChannelService(uc *biz.ChannelUsecase, runtimeRepo biz.ChannelRuntimeRepo, agentSvc *AgentService, logger log.Logger) *ChannelService {
	return &ChannelService{uc: uc, runtimeRepo: runtimeRepo, agentSvc: agentSvc, log: log.NewHelper(logger)}
}

// channelMetaToReply maps ChannelMeta to Admin ChannelReply.
// Never includes plaintext bot_secret / corp_secret (use secret_set instead).
func channelMetaToReply(ch *biz.ChannelMeta) *channelv1.ChannelReply {
	r := &channelv1.ChannelReply{
		Ret:                 baseSuccess(),
		Id:                  ch.ID,
		ChannelId:           ch.ChannelID,
		Type:                ch.Type,
		DefaultAgent:       ch.DefaultAgent,
		AllowedAgents:      ch.AllowedAgents,
		Enabled:             ch.Enabled,
		AutoRouteEnabled:    ch.AutoRouteEnabled,
		AutoRouteMention:    ch.AutoRouteMention,
		AutoRouteClassifier: ch.AutoRouteClassifier,
		WebhookPath:         ch.WebhookPath,
		IpWhitelist:         ch.IPWhitelist,
		CreatedAt:           ch.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
		UpdatedAt:           ch.UpdatedAt.Format("2006-01-02T15:04:05Z07:00"),
		BotId:               ch.BotID,
		SecretSet:           ch.BotSecret != "",
		BotNames:            ch.BotNames,
		WsUrl:               ch.WSURL,
		CorpId:              ch.CorpID,
		DefaultReplyMode:    ch.DefaultReplyMode,
	}
	if ch.Type == "wxpusher" {
		r.DefaultUids = ch.DefaultUids
	}
	if ch.Type == "wecom" {
		r.WebhookUrlMasked = maskWeComWebhookURL(ch.WebhookURL)
	}
	return r
}

func runtimeStatusViewToProto(v *biz.RuntimeStatusView) *channelv1.RuntimeStatus {
	if v == nil {
		return nil
	}
	out := &channelv1.RuntimeStatus{
		State:             v.State,
		LastError:         v.LastError,
		ReconnectAttempt:  int32(v.ReconnectAttempt),
		ReconnectInMs:     int32(v.ReconnectInMs),
	}
	if !v.LastHeartbeatAt.IsZero() {
		out.LastHeartbeatAt = v.LastHeartbeatAt.UTC().Format(time.RFC3339)
	}
	return out
}

func (s *ChannelService) loadRuntimeRow(ctx context.Context, channelID string) *biz.RuntimeStatusRow {
	if s == nil || s.runtimeRepo == nil || channelID == "" {
		return nil
	}
	row, err := s.runtimeRepo.Get(ctx, channelID)
	if err != nil {
		if stderrors.Is(err, pkgErrors.ErrNotFound) {
			return nil
		}
		s.log.Warnf("get channel runtime status channel_id=%s: %v", channelID, err)
		return nil
	}
	return row
}

func (s *ChannelService) channelToReply(ctx context.Context, ch *biz.ChannelMeta) *channelv1.ChannelReply {
	r := channelMetaToReply(ch)
	view := biz.DeriveRuntimeStatus(ch, s.loadRuntimeRow(ctx, ch.ChannelID), time.Now())
	r.RuntimeStatus = runtimeStatusViewToProto(view)
	return r
}

func structToMap(s *structpb.Struct) map[string]any {
	if s == nil || s.Fields == nil {
		return nil
	}
	m := make(map[string]any)
	for k, v := range s.Fields {
		m[k] = structValueToAny(v)
	}
	return m
}

func structValueToAny(v *structpb.Value) any {
	if v == nil {
		return nil
	}
	switch x := v.Kind.(type) {
	case *structpb.Value_StringValue:
		return x.StringValue
	case *structpb.Value_NumberValue:
		return x.NumberValue
	case *structpb.Value_BoolValue:
		return x.BoolValue
	case *structpb.Value_ListValue:
		if x.ListValue == nil {
			return nil
		}
		arr := make([]any, len(x.ListValue.Values))
		for i, item := range x.ListValue.Values {
			arr[i] = structValueToAny(item)
		}
		return arr
	case *structpb.Value_StructValue:
		if x.StructValue == nil {
			return nil
		}
		return structToMap(x.StructValue)
	default:
		return nil
	}
}

// CreateChannel implements channel.v1.ChannelHTTPServer
func (s *ChannelService) CreateChannel(ctx context.Context, req *channelv1.CreateChannelRequest) (*channelv1.ChannelReply, error) {
	if req.GetChannelId() == "" || req.GetType() == "" {
		return nil, errors.BadRequest("INVALID", "channel_id and type required")
	}
	if req.GetType() == "wxpusher" && req.GetAppToken() == "" {
		return nil, errors.BadRequest("INVALID", "wxpusher channel requires app_token")
	}
	if req.GetType() == "wecom" && !isValidWeComWebhookURL(req.GetWebhookUrl()) {
		return nil, errors.BadRequest("INVALID", "wecom channel requires valid webhook_url")
	}
	autoRouteEnabled, autoRouteMention, autoRouteClassifier := true, true, true
	if req.AutoRouteEnabled != nil {
		autoRouteEnabled = req.GetAutoRouteEnabled()
	}
	if req.AutoRouteMention != nil {
		autoRouteMention = req.GetAutoRouteMention()
	}
	if req.AutoRouteClassifier != nil {
		autoRouteClassifier = req.GetAutoRouteClassifier()
	}
	ch, err := s.uc.Create(ctx, &biz.ChannelCreate{
		ChannelID:           req.GetChannelId(),
		Type:                req.GetType(),
		DefaultAgent:        req.GetDefaultAgent(),
		AllowedAgents:       req.GetAllowedAgents(),
		Enabled:             req.GetEnabled(),
		AutoRouteEnabled:    autoRouteEnabled,
		AutoRouteMention:    autoRouteMention,
		AutoRouteClassifier: autoRouteClassifier,
		WebhookPath:         req.GetWebhookPath(),
		WebhookSecret:       req.GetWebhookSecret(),
		IPWhitelist:         req.GetIpWhitelist(),
		AppToken:            req.GetAppToken(),
		DefaultUids:         req.GetDefaultUids(),
		WebhookURL:          req.GetWebhookUrl(),
		BotID:               req.GetBotId(),
		BotSecret:           req.GetSecret(),
		BotNames:            req.GetBotNames(),
		WSURL:               req.GetWsUrl(),
		CorpID:              req.GetCorpId(),
		CorpSecret:          req.GetCorpSecret(),
		DefaultReplyMode:    req.GetDefaultReplyMode(),
	})
	if err != nil {
		logServiceError(s.log, "CreateChannel", err, "channel_id", req.GetChannelId(), "type", req.GetType())
		return nil, err
	}
	if ch.Type == "wecom" && ch.DefaultAgent != "" {
		if err := s.agentSvc.BindWecomChannel(ctx, ch.DefaultAgent, ch.ID); err != nil {
			logServiceError(s.log, "CreateChannel.BindWecomChannel", err, "channel_id", ch.ID, "agent_id", ch.DefaultAgent)
		}
	}
	return s.channelToReply(ctx, ch), nil
}

// ListChannels implements channel.v1.ChannelHTTPServer
func (s *ChannelService) ListChannels(ctx context.Context, req *channelv1.ListChannelsRequest) (*channelv1.ListChannelsReply, error) {
	page, pageSize := req.GetPage(), req.GetPageSize()
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 20
	}
	var enabled *bool
	if req.Enabled != nil {
		enabled = req.Enabled
	}
	list, total, err := s.uc.List(ctx, page, pageSize, req.GetType(), enabled)
	if err != nil {
		logServiceError(s.log, "ListChannels", err, "page", page, "page_size", pageSize, "type", req.GetType())
		return nil, err
	}
	items := make([]*channelv1.ChannelReply, len(list))
	for i, c := range list {
		items[i] = s.channelToReply(ctx, c)
	}
	return &channelv1.ListChannelsReply{
		Ret:   baseSuccess(),
		Items: items,
		Total: int32(total),
	}, nil
}

// GetChannel implements channel.v1.ChannelHTTPServer
func (s *ChannelService) GetChannel(ctx context.Context, req *channelv1.GetChannelRequest) (*channelv1.ChannelReply, error) {
	ch, err := s.uc.Get(ctx, req.GetId())
	if err != nil {
		logServiceError(s.log, "GetChannel", err, "channel_id", req.GetId())
		return nil, err
	}
	return s.channelToReply(ctx, ch), nil
}

// UpdateChannel implements channel.v1.ChannelHTTPServer.
// updates Struct keys (snake_case) include existing fields plus gateway keys:
// bot_id, secret (alias→bot_secret), bot_secret, bot_names, ws_url, corp_id,
// corp_secret, default_reply_mode. Empty secret/bot_secret/corp_secret preserve existing.
func (s *ChannelService) UpdateChannel(ctx context.Context, req *channelv1.UpdateChannelRequest) (*channelv1.ChannelReply, error) {
	updates := structToMap(req.GetUpdates())
	if len(updates) == 0 {
		return s.GetChannel(ctx, &channelv1.GetChannelRequest{Id: req.GetId()})
	}
	current, err := s.uc.Get(ctx, req.GetId())
	if err != nil {
		logServiceError(s.log, "UpdateChannel.Get", err, "channel_id", req.GetId())
		return nil, err
	}
	effectiveType := current.Type
	if v, ok := updates["type"].(string); ok {
		effectiveType = v
	}
	if effectiveType == "wecom" {
		if w, ok := updates["webhook_url"].(string); ok {
			if !isValidWeComWebhookURL(w) {
				return nil, errors.BadRequest("INVALID", "wecom channel requires valid webhook_url")
			}
		} else if current.Type != "wecom" {
			if !isValidWeComWebhookURL(current.WebhookURL) {
				return nil, errors.BadRequest("INVALID", "wecom channel requires valid webhook_url")
			}
		}
	}
	ch, err := s.uc.Update(ctx, req.GetId(), updates)
	if err != nil {
		logServiceError(s.log, "UpdateChannel", err, "channel_id", req.GetId())
		return nil, err
	}
	if ch.Type == "wecom" && ch.DefaultAgent != "" {
		if err := s.agentSvc.BindWecomChannel(ctx, ch.DefaultAgent, ch.ID); err != nil {
			logServiceError(s.log, "UpdateChannel.BindWecomChannel", err, "channel_id", ch.ID, "agent_id", ch.DefaultAgent)
		}
	}
	return s.channelToReply(ctx, ch), nil
}

// DeleteChannel implements channel.v1.ChannelHTTPServer
func (s *ChannelService) DeleteChannel(ctx context.Context, req *channelv1.DeleteChannelRequest) (*channelv1.DeleteChannelReply, error) {
	if err := s.uc.Delete(ctx, req.GetId()); err != nil {
		logServiceError(s.log, "DeleteChannel", err, "channel_id", req.GetId())
		return nil, err
	}
	return &channelv1.DeleteChannelReply{Ret: baseSuccess()}, nil
}

// SendChannel implements channel.v1.ChannelHTTPServer
func (s *ChannelService) SendChannel(ctx context.Context, req *channelv1.SendChannelRequest) (*channelv1.SendChannelReply, error) {
	content := strings.TrimSpace(req.GetContent())
	if content == "" {
		return nil, errors.BadRequest("INVALID", "content required")
	}
	ch, err := s.uc.Get(ctx, req.GetId())
	if err != nil {
		logServiceError(s.log, "SendChannel.Get", err, "channel_id", req.GetId())
		return nil, err
	}
	switch ch.Type {
	case "wxpusher":
		if ch.AppToken == "" {
			return nil, errors.BadRequest("INVALID", "wxpusher channel requires app_token")
		}
		uids := req.GetUids()
		if len(uids) == 0 {
			uids = ch.DefaultUids
		}
		if len(uids) == 0 {
			return nil, errors.BadRequest("INVALID", "uids or default_uids required")
		}
		if err := channel.PushToWxPusher(ctx, ch.AppToken, uids, content, req.GetSummary()); err != nil {
			logServiceError(s.log, "SendChannel.PushToWxPusher", err, "channel_id", req.GetId(), "uid_count", len(uids))
			return nil, errors.New(500, "INTERNAL", err.Error())
		}
	case "wecom":
		if ch.WebhookURL == "" {
			return nil, errors.BadRequest("INVALID", "wecom channel requires webhook_url")
		}
		if err := channel.PushToWeCom(ctx, ch.WebhookURL, content, "text"); err != nil {
			logServiceError(s.log, "SendChannel.PushToWeCom", err, "channel_id", req.GetId())
			return nil, errors.New(500, "INTERNAL", err.Error())
		}
	default:
		return nil, errors.BadRequest("INVALID", "only wxpusher or wecom channel supports send")
	}
	return &channelv1.SendChannelReply{Ret: baseSuccess()}, nil
}

// WebhookInbound implements channel.v1.ChannelHTTPServer
// 注意：签名校验、IP 白名单由 WebhookVerifyMiddleware 在解码前完成
func (s *ChannelService) WebhookInbound(ctx context.Context, req *channelv1.WebhookInboundRequest) (*channelv1.WebhookInboundReply, error) {
	content := strings.TrimSpace(req.GetContent())
	if content == "" {
		return nil, errors.BadRequest("INVALID", "content required")
	}
	ch, err := s.uc.GetByChannelID(ctx, req.GetChannelId())
	if err != nil {
		logServiceError(s.log, "WebhookInbound.GetByChannelID", err, "channel_id", req.GetChannelId())
		return nil, err
	}
	if !ch.Enabled {
		return nil, errors.Forbidden("FORBIDDEN", "channel disabled")
	}
	if ch.Type != "webhook" {
		return nil, errors.BadRequest("INVALID", "channel type must be webhook")
	}
	agentID := req.GetAgentId()
	if agentID == "" {
		agentID = ch.DefaultAgent
	}
	if agentID == "" {
		return nil, errors.BadRequest("INVALID", "agent_id required (or set channel default_agent)")
	}

	chatReq := &agentv1.ChatRequest{Id: agentID, Content: content}
	reply, err := s.agentSvc.Chat(ctx, chatReq)
	if err != nil {
		logServiceError(s.log, "WebhookInbound.AgentChat", err, "channel_id", req.GetChannelId(), "agent_id", agentID)
		return nil, err
	}

	contentOut := reply.GetContent()
	if reply.GetRet() != nil && reply.GetRet().GetCode() != 0 {
		contentOut = reply.GetRet().GetMessage()
	}

	if req.GetReplyUrl() != "" {
		go postReply(req.GetReplyUrl(), contentOut)
		return &channelv1.WebhookInboundReply{Ret: baseSuccess(), Async: true}, nil
	}
	return &channelv1.WebhookInboundReply{Ret: baseSuccess(), Content: contentOut}, nil
}

func postReply(url, content string) {
	body, _ := json.Marshal(map[string]string{"content": content})
	req, _ := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	http.DefaultClient.Do(req)
}
