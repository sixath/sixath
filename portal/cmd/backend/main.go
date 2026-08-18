package main

import (
	"context"
	"flag"
	"os"
	"path/filepath"
	"strings"

	"backend/internal/biz"
	"backend/internal/chat"
	"backend/internal/conf"
	"backend/internal/cron"
	"backend/internal/runtime"
	"backend/internal/server"
	"backend/internal/service"

	"github.com/go-kratos/kratos/v2"
	"github.com/go-kratos/kratos/v2/config"
	"github.com/go-kratos/kratos/v2/config/file"
	"github.com/go-kratos/kratos/v2/log"
	"github.com/go-kratos/kratos/v2/middleware/tracing"
	"github.com/go-kratos/kratos/v2/transport/grpc"
	"github.com/go-kratos/kratos/v2/transport/http"

	fwconfig "github.com/sixath/framework/config"
	"github.com/sixath/framework/turntrace"

	_ "go.uber.org/automaxprocs"
)

// go build -ldflags "-X main.Version=x.y.z"
var (
	// Name is the name of the compiled software.
	Name string
	// Version is the version of the compiled software.
	Version string
	// flagconf is the config flag.
	flagconf string

	id, _ = os.Hostname()
)

func init() {
	flag.StringVar(&flagconf, "conf", "../configs", "config path, eg: -conf config.yaml")
}

func newApp(logger log.Logger, gs *grpc.Server, hs *http.Server, cronSrv *cron.Server, chatSvc *service.ChatService, gw *service.GrowthWorker, cw *service.CuratorWorker, workerEnabled workerEnabledInput) *kratos.App {
	workerCtx, stopWorker := context.WithCancel(context.Background())
	if chatSvc != nil && gw != nil {
		chatSvc.SetBackgroundReviewer(gw)
	}
	opts := []kratos.Option{
		kratos.ID(id),
		kratos.Name(Name),
		kratos.Version(Version),
		kratos.Metadata(map[string]string{}),
		kratos.Logger(logger),
		kratos.Server(gs, hs, cronSrv),
		kratos.AfterStop(func(context.Context) error {
			stopWorker()
			return nil
		}),
	}
	// GrowthWorker is always constructed (C3 SpawnBackgroundReview); poll Loop only when enabled.
	if gw != nil && bool(workerEnabled) {
		opts = append(opts, kratos.BeforeStart(func(context.Context) error {
			go gw.Loop(workerCtx)
			return nil
		}))
	}
	if cw != nil {
		opts = append(opts, kratos.BeforeStart(func(context.Context) error {
			go cw.Loop(workerCtx)
			return nil
		}))
	}
	return kratos.New(opts...)
}

// Distinct Wire input types avoid ambiguous primitive bindings in wireApp.
type llmReviewEnabledInput bool
type growthReviewPatchFileInput string
type curatorPatchFileInput string
type workerEnabledInput bool

func provideGrowthWorker(
	logger log.Logger,
	chatUC *biz.ChatUsecase,
	agentUC *biz.AgentUsecase,
	growthUC *biz.GrowthUsecase,
	cronRefUC *biz.CronRefRewriteUsecase,
	auth *conf.Auth,
	llmReviewEnabled llmReviewEnabledInput,
	reviewPatchFile growthReviewPatchFileInput,
	growthCfg *conf.Growth,
	_ workerEnabledInput, // Loop gated in newApp; always construct for C3 SpawnBackgroundReview
	turnTraces turntrace.Store,
) *service.GrowthWorker {
	return service.NewGrowthWorker(logger, chatUC, agentUC, growthUC, cronRefUC, bool(llmReviewEnabled), string(reviewPatchFile), growthCfg, auth, turnTraces)
}

func provideCuratorWorker(
	logger log.Logger,
	curatorUC *biz.CuratorUsecase,
	cronRefUC *biz.CronRefRewriteUsecase,
	growthCfg *conf.Growth,
	llmReviewEnabled llmReviewEnabledInput,
	curatorPatchFile curatorPatchFileInput,
) *service.CuratorWorker {
	return service.NewCuratorWorker(logger, curatorUC, cronRefUC, growthCfg, bool(llmReviewEnabled), string(curatorPatchFile))
}

func main() {
	flag.Parse()
	logger := log.With(log.NewStdLogger(os.Stdout),
		"ts", log.DefaultTimestamp,
		"caller", log.DefaultCaller,
		"service.id", id,
		"service.name", Name,
		"service.version", Version,
		"trace.id", tracing.TraceID(),
		"span.id", tracing.SpanID(),
	)
	c := config.New(
		config.WithSource(
			file.NewSource(flagconf),
		),
	)
	defer c.Close()

	if err := c.Load(); err != nil {
		panic(err)
	}

	var bc conf.Bootstrap
	if err := c.Scan(&bc); err != nil {
		panic(err)
	}

	bc.Auth = conf.EnrichAuthFromEnv(bc.Auth)
	conf.EnrichDataFromEnv(bc.Data)
	conf.EnrichGrowthFromEnv(bc.Growth)
	chat.EnrichFailureCaptureFromEnv()

	chat.InitSessionSearchFromEnv()

	var portalExtra *fwconfig.PortalAgentExtra
	if p, err := fwconfig.ResolvePortalAgentExtraPath(flagconf); err == nil {
		if extra, err := fwconfig.LoadPortalAgentExtra(p); err != nil {
			panic(err)
		} else if extra != nil {
			portalExtra = extra
		}
	}
	chat.InitWebSettings(flagconf, portalExtra)

	if rt, err := conf.LoadRuntimeFromConfigPath(flagconf); err != nil {
		panic(err)
	} else if rt != nil {
		runtime.Configure(rt.ServiceToken)
	}

	if chatCfg, err := conf.LoadChatFromConfigPath(flagconf); err != nil {
		panic(err)
	} else if chatCfg != nil {
		server.ConfigurePublicInbound(chatCfg.PublicInboundEnabled)
		if chatCfg.TurnToolSurfaceEnabled != nil {
			chat.SetTurnToolSurfaceEnabled(*chatCfg.TurnToolSurfaceEnabled)
			if !*chatCfg.TurnToolSurfaceEnabled {
				log.NewHelper(logger).Info("turn tool surface disabled (chat.turn_tool_surface_enabled=false or SATH_TURN_TOOL_SURFACE=0); bound RCA/MCP tools are fully registered")
			}
		}
	}

	flags := chat.DefaultHermesP0ToolFlags
	chat.EnrichHermesP0FromEnv(&flags)
	// Do not promote WebToolsShouldRegister (Bocha/Tavily key present) into process-wide
	// WebToolsEnabled: that OR-merged into every agent and bypassed agent webToolsEnabled=false.
	// Enable web via Agent runtime_tools.web_tools_enabled and/or SATH_WEB_TOOLS_ENABLED.
	chat.SetHermesP0ToolFlags(flags)

	// 技能脚本执行开关：config > 环境变量 SATH_ALLOW_SCRIPT_EXECUTION > 默认 true
	if bc.Skills != nil {
		chat.SetAllowScriptExecution(bc.Skills.GetAllowScriptExecution())
		chat.SetSkillRouteSettings(chat.SkillRouteSettings{
			Enabled:      bc.Skills.GetAutoRouteEnabled(),
			MinScore:     int(bc.Skills.GetRouteMinScore()),
			MaxBodyRunes: int(bc.Skills.GetRouteMaxBodyRunes()),
		})
	} else if v := os.Getenv("SATH_ALLOW_SCRIPT_EXECUTION"); v != "" {
		chat.SetAllowScriptExecution(v == "1" || v == "true" || v == "yes")
	}
	// 否则保持默认 true；技能路由默认 DefaultSkillRouteSettings（auto_route 默认 true）

	// growth.llm_review_enabled：YAML 优先；未配置 growth 节时可由 SATH_GROWTH_LLM_REVIEW_ENABLED 开启（二期里程碑 2.1）。
	llmReviewEnabled := false
	if bc.Growth != nil {
		llmReviewEnabled = bc.Growth.GetLlmReviewEnabled()
	} else if v := os.Getenv("SATH_GROWTH_LLM_REVIEW_ENABLED"); v != "" {
		llmReviewEnabled = v == "1" || v == "true" || v == "yes"
	}

	// growth.worker_enabled：YAML 优先；未配置时默认 true；可通过 SATH_GROWTH_WORKER_ENABLED 关闭。
	workerEnabled := true
	if bc.Growth != nil {
		workerEnabled = bc.Growth.GetWorkerEnabled()
	} else if v := os.Getenv("SATH_GROWTH_WORKER_ENABLED"); v != "" {
		workerEnabled = v == "1" || v == "true" || v == "yes"
	}

	// growth.review_patch_file：YAML 优先；否则 SATH_GROWTH_REVIEW_PATCH_FILE；相对路径相对 -conf 目录（或单文件 yaml 的父目录）。
	growthReviewPatchFile := ""
	if bc.Growth != nil {
		growthReviewPatchFile = strings.TrimSpace(bc.Growth.GetReviewPatchFile())
	}
	if growthReviewPatchFile == "" {
		growthReviewPatchFile = strings.TrimSpace(os.Getenv("SATH_GROWTH_REVIEW_PATCH_FILE"))
	}
	if growthReviewPatchFile != "" && !filepath.IsAbs(growthReviewPatchFile) {
		base := flagconf
		if st, err := os.Stat(flagconf); err == nil && !st.IsDir() {
			base = filepath.Dir(flagconf)
		}
		growthReviewPatchFile = filepath.Clean(filepath.Join(base, growthReviewPatchFile))
	}

	curatorPatchFile := ""
	if bc.Growth != nil {
		curatorPatchFile = strings.TrimSpace(bc.Growth.GetCuratorPatchFile())
	}
	if curatorPatchFile == "" {
		curatorPatchFile = strings.TrimSpace(os.Getenv("SATH_GROWTH_CURATOR_PATCH_FILE"))
	}
	if curatorPatchFile != "" && !filepath.IsAbs(curatorPatchFile) {
		base := flagconf
		if st, err := os.Stat(flagconf); err == nil && !st.IsDir() {
			base = filepath.Dir(flagconf)
		}
		curatorPatchFile = filepath.Clean(filepath.Join(base, curatorPatchFile))
	}

	if bc.Data != nil {
		chat.SetMemoryVectorDataRoot(bc.Data.GetDataRoot())
		chat.SetMEADataRoot(bc.Data.GetDataRoot())
	}

	if portalExtra != nil {
		chat.SetPortalAgentExtra(portalExtra)
	} else if p, err := fwconfig.ResolvePortalAgentExtraPath(flagconf); err == nil {
		if extra, err := fwconfig.LoadPortalAgentExtra(p); err != nil {
			panic(err)
		} else if extra != nil {
			chat.SetPortalAgentExtra(extra)
		}
	}

	app, cleanup, err := wireApp(
		bc.Server,
		bc.Data,
		bc.Auth,
		llmReviewEnabledInput(llmReviewEnabled),
		growthReviewPatchFileInput(growthReviewPatchFile),
		curatorPatchFileInput(curatorPatchFile),
		workerEnabledInput(workerEnabled),
		bc.Growth,
		logger,
	)
	if err != nil {
		panic(err)
	}
	defer cleanup()

	// start and wait for stop signal
	if err := app.Run(); err != nil {
		panic(err)
	}
}
