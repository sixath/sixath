package main

import (
	"flag"
	"os"

	"backend/internal/chat"
	"backend/internal/conf"
	"backend/internal/cron"
	"backend/internal/runtime"
	"backend/internal/server"

	"github.com/go-kratos/kratos/v2"
	"github.com/go-kratos/kratos/v2/config"
	"github.com/go-kratos/kratos/v2/config/file"
	"github.com/go-kratos/kratos/v2/log"
	"github.com/go-kratos/kratos/v2/middleware/tracing"
	"github.com/go-kratos/kratos/v2/transport/grpc"
	"github.com/go-kratos/kratos/v2/transport/http"

	fwconfig "github.com/sixath/framework/config"

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

func newApp(logger log.Logger, gs *grpc.Server, hs *http.Server, cronSrv *cron.Server) *kratos.App {
	return kratos.New(
		kratos.ID(id),
		kratos.Name(Name),
		kratos.Version(Version),
		kratos.Metadata(map[string]string{}),
		kratos.Logger(logger),
		kratos.Server(gs, hs, cronSrv),
	)
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
	chat.EnrichSkillManageConfirmFromEnv()

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
	} else if v := os.Getenv("SATH_ALLOW_SCRIPT_EXECUTION"); v != "" {
		chat.SetAllowScriptExecution(v == "1" || v == "true" || v == "yes")
	}

	if bc.Data != nil {
		chat.SetMemoryVectorDataRoot(bc.Data.GetDataRoot())
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
