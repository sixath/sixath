package main

import (
	"context"
	"flag"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/sixath/gateway/internal/adapter"
	"github.com/sixath/gateway/internal/channel"
	"github.com/sixath/gateway/internal/config"
	"github.com/sixath/gateway/internal/idempotency"
	"github.com/sixath/gateway/internal/metrics"
	"github.com/sixath/gateway/internal/observability"
	"github.com/sixath/gateway/internal/reply"
	"github.com/sixath/gateway/internal/runtimeclient"
	"github.com/sixath/gateway/internal/session"
)

// Version is set at build time via -ldflags "-X main.Version=...".
var Version = "dev"

func main() {
	logger := observability.SetupLogger()

	configPath := flag.String("config", "./configs/config.example.yaml", "path to gateway config YAML")
	flag.Parse()

	cfg, err := config.Load(*configPath)
	if err != nil {
		logger.Error("load_config_failed", "err", err)
		os.Exit(1)
	}

	reg, err := channel.Load(cfg.ChannelsFile)
	if err != nil {
		logger.Error("load_channels_failed", "err", err)
		os.Exit(1)
	}

	rt := runtimeclient.New(cfg.PortalBaseURL, cfg.RuntimeToken)
	turnTimeout := time.Duration(cfg.TurnTimeoutSec) * time.Second

	// Shared with webhook + wecom_bot so peer sessions and msgid idempotency align.
	sessions := session.NewRouter(rt, 30*time.Second)
	idem := idempotency.NewStore(10 * time.Minute)

	mux := http.NewServeMux()
	mux.Handle("POST /hooks/{channel_id}", adapter.NewWebhookHandler(adapter.WebhookDeps{
		Registry:    reg,
		Runtime:     rt,
		Sessions:    sessions,
		Idempotency: idem,
		Reply:       reply.NewDispatcher(nil),
		TurnTimeout: turnTimeout,
	}))
	adapter.MountWeb(mux, adapter.WebDeps{
		PortalBaseURL: cfg.PortalBaseURL,
		Runtime:       rt,
	})
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	mux.Handle("GET /metrics", metrics.Handler())

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	adapter.StartWecomBots(ctx, adapter.WecomBotDeps{
		Registry:    reg,
		Runtime:     rt,
		Sessions:    sessions,
		Idempotency: idem,
		TurnTimeout: turnTimeout,
	})

	logger.Info("gateway_starting",
		"version", Version,
		"listen", cfg.Listen,
		"portal_base_url", cfg.PortalBaseURL,
		"turn_timeout_sec", cfg.TurnTimeoutSec,
		"channels_file", cfg.ChannelsFile,
	)

	srv := &http.Server{Addr: cfg.Listen, Handler: observability.Middleware(mux)}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}()
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		logger.Error("listen_failed", "err", err)
		os.Exit(1)
	}
}
