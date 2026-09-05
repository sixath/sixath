package server

import (
	agentv1 "backend/api/agent/v1"
	channelv1 "backend/api/channel/v1"
	chatv1 "backend/api/chat/v1"
	"backend/api/common"
	cronv1 "backend/api/cron/v1"
	toolv1 "backend/api/tool/v1"
	"backend/internal/biz"
	"backend/internal/conf"
	"backend/internal/cron"
	"backend/internal/runtime"
	"backend/internal/server/middleware"
	"backend/internal/service"
	"github.com/go-kratos/aegis/ratelimit/bbr"
	"github.com/go-kratos/kratos/v2/errors"
	"github.com/go-kratos/kratos/v2/middleware/logging"
	"github.com/go-kratos/kratos/v2/middleware/ratelimit"
	"github.com/go-kratos/kratos/v2/middleware/tracing"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	"net/http"
	"reflect"

	"github.com/go-kratos/kratos/v2/log"
	"github.com/go-kratos/kratos/v2/middleware/recovery"
	httptransport "github.com/go-kratos/kratos/v2/transport/http"
	"github.com/sixath/framework/obs"
)

// NewHTTPServer new an HTTP server.
func NewHTTPServer(c *conf.Server, tool *service.ToolService, agent *service.AgentService, chat *service.ChatService, channelSvc *service.ChannelService, cronSvc *cron.CronService, channelUC *biz.ChannelUsecase, identityRepo biz.IdentityRepo, aclAPI *biz.ACLAPIUsecase, authUC *biz.AuthUsecase, mcpServer *service.McpServerService, runtimeSvc *runtime.Service, pinger DBPinger, agentUC *biz.AgentUsecase, codeRoots []string, logger log.Logger) *httptransport.Server {
	addr := ":0"
	if c != nil && c.Http != nil && c.Http.Addr != "" {
		addr = c.Http.Addr
	}

	opts := []httptransport.ServerOption{
		httptransport.Address(addr),
		httptransport.ResponseEncoder(responseEncoder),
		httptransport.ErrorEncoder(errorEncoder),
		// Close legacy public Chat inbound when chat.public_inbound_enabled=false (default).
		httptransport.Filter(PublicChatInboundFilter()),
	}
	// Kratos defaults HTTP timeout to 1s; without this, ACL-heavy list APIs hit
	// context deadline exceeded even when config.yaml sets server.http.timeout.
	if c != nil && c.Http != nil && c.Http.Timeout != nil {
		opts = append(opts, httptransport.Timeout(c.Http.Timeout.AsDuration()))
	}
	opts = append(opts, httptransport.Middleware(
		recovery.Recovery(),
		tracing.Server(
			tracing.WithTracerProvider(otel.GetTracerProvider()),
			tracing.WithPropagator(
				propagation.NewCompositeTextMapPropagator(propagation.Baggage{}, propagation.TraceContext{}),
			),
		),
		logging.Server(logger),
		middleware.ServerMetrics(),
		middleware.Validator(),
		middleware.TraceparentMiddleware(),
		middleware.MetaData(),
		middleware.Auth(identityRepo, identityRepo),
		middleware.WebhookVerify(channelUC),
		ratelimit.Server(ratelimit.WithLimiter(bbr.NewLimiter())),
	))

	srv := httptransport.NewServer(opts...)
	toolv1.RegisterToolHTTPServer(srv, tool)
	agentv1.RegisterAgentHTTPServer(srv, agent)
	chatv1.RegisterChatHTTPServer(srv, chat)
	channelv1.RegisterChannelHTTPServer(srv, channelSvc)
	cronv1.RegisterCronHTTPServer(srv, cronSvc)
	// SSE 流式接口（5.5.4）：/stream 路径或主路径 + Accept: text/event-stream
	r := srv.Route("/")
	r.POST("/api/v1/auth/login", LoginHandler(authUC))
	r.POST("/api/v1/auth/register", RegisterHandler(authUC))
	r.GET("/api/v1/auth/invites/{token}", PreviewInviteHandler(authUC))
	r.POST("/api/v1/auth/verify-email", VerifyEmailHandler(authUC))
	// Gateway resolves opaque session tokens here; global Auth skips /api/v1/auth/*.
	r.GET("/api/v1/auth/me", AuthMeHandler(identityRepo))
	r.POST("/api/v1/orgs", CreateOrgHandler(aclAPI))
	r.GET("/api/v1/orgs", ListOrgsHandler(aclAPI))
	r.POST("/api/v1/orgs/{id}/invites", CreateInviteHandler(aclAPI))
	r.GET("/api/v1/orgs/{id}/invites", ListInvitesHandler(aclAPI))
	r.DELETE("/api/v1/orgs/{id}/invites/{invite_id}", RevokeInviteHandler(aclAPI))
	r.POST("/api/v1/sessions/{session_id}/messages/stream", SendMessageSSE(chat, logger))
	r.POST("/api/v1/orgs/{id}/members", AddOrgMemberHandler(aclAPI))
	r.POST("/api/v1/resources/{id}/grants", CreateResourceGrantHandler(aclAPI))
	r.POST("/api/v1/users/{id}/tokens", IssueUserTokenHandler(aclAPI))
	// E2: growth 指标 JSON 端点（spec phase2 §E2）。便于人工排查；正式生产可接 Prometheus collector。
	r.GET("/api/v1/growth/metrics", GrowthMetricsHandler())
	// Trajectory utilization: message-level SearchAnchored for UI (hand-written; not in chat.proto).
	r.GET("/api/v1/agents/{agent_id}/transcript/search", TranscriptSearchHandler(chat))
	// Code roots browse + agent workspace/code symlink (hand-written).
	r.GET("/api/v1/code-roots", CodeRootsListHandler(codeRoots))
	r.GET("/api/v1/code-roots/browse", CodeRootsBrowseHandler(codeRoots))
	r.GET("/api/v1/agents/{agent_id}/workspace-link", AgentWorkspaceLinkGetHandler(agentUC))
	r.POST("/api/v1/agents/{agent_id}/workspace-link", AgentWorkspaceLinkHandler(agentUC, codeRoots))
	r.POST("/api/v1/sessions/{session_id}/rewind", RewindHandler(chat))
	r.POST("/api/v1/mcp-servers", CreateMcpServerHandler(mcpServer))
	r.GET("/api/v1/mcp-servers", ListMcpServersHandler(mcpServer))
	r.GET("/api/v1/mcp-servers/{id}", GetMcpServerHandler(mcpServer))
	r.PUT("/api/v1/mcp-servers/{id}", UpdateMcpServerHandler(mcpServer))
	r.DELETE("/api/v1/mcp-servers/{id}", DeleteMcpServerHandler(mcpServer))
	r.POST("/api/v1/mcp-servers/{id}/test", TestMcpServerHandler(mcpServer))
	r.POST("/api/v1/agents/{id}/mcp-servers", BindAgentMcpServersHandler(mcpServer))
	r.DELETE("/api/v1/agents/{id}/mcp-servers", UnbindAgentMcpServersHandler(mcpServer))
	// Runtime (/runtime/v1): Gateway service-token surface; auth applied per-handler.
	runtime.RegisterRoutes(srv, runtimeSvc)
	srv.Handle("/healthz", healthzHandler())
	srv.Handle("/readyz", readyzHandler(pinger))
	setupPrometheusEndpoint(srv)
	return srv
}

type BizResp interface {
	GetRet() *common.BaseResponse
}

func responseEncoder(w http.ResponseWriter, r *http.Request, v interface{}) error {
	if v == nil {
		return nil
	}
	if rd, ok := v.(httptransport.Redirector); ok {
		url, code := rd.Redirect()
		http.Redirect(w, r, url, code)
		return nil
	}
	bizResp, ok := v.(BizResp)
	if ok && bizResp != nil && bizResp.GetRet() == nil {
		val := reflect.ValueOf(v).Elem()
		ret := val.FieldByName("Ret")
		if ret.CanSet() {
			ret.Set(reflect.ValueOf(&common.BaseResponse{Code: 0, Message: "", Reason: ""}))
		}
	}
	codec, _ := httptransport.CodecForRequest(r, "Accept")
	data, err := codec.Marshal(v)
	if err != nil {
		return err
	}
	w.Header().Set("Content-Type", "application/json")
	_, err = w.Write(data)
	return err
}

func errorEncoder(w http.ResponseWriter, r *http.Request, err error) {
	se := errors.FromError(err)
	codec, _ := httptransport.CodecForRequest(r, "Accept")
	bizCode := common.ErrorReason_value[se.Reason]
	if bizCode == 0 {
		bizCode = se.GetCode()
	}
	rsp := common.Response{
		Ret: &common.BaseResponse{
			Code:    bizCode,
			Reason:  se.Reason,
			Message: se.Message,
		},
	}
	body, err := codec.Marshal(&rsp)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(int(se.Code))
	_, _ = w.Write(body)
}

// setupPrometheusEndpoint mounts framework/obs Prometheus metrics (incl. memory_extract_*).
func setupPrometheusEndpoint(srv *httptransport.Server) {
	if srv == nil {
		return
	}
	srv.Handle("/metrics", obs.MetricsHandler())
}
