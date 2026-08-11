package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/sixath/gateway/internal/adapter"
	"github.com/sixath/gateway/internal/channel"
	"github.com/sixath/gateway/internal/config"
	"github.com/sixath/gateway/internal/idempotency"
	"github.com/sixath/gateway/internal/pendingswitch"
	"github.com/sixath/gateway/internal/reply"
	"github.com/sixath/gateway/internal/runtimeclient"
	"github.com/sixath/gateway/internal/session"
)

// Version is set at build time via -ldflags "-X main.Version=...".
var Version = "dev"

func main() {
	configPath := flag.String("config", "./configs/config.example.yaml", "path to gateway config YAML")
	flag.Parse()

	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	reg, err := channel.Load(cfg.ChannelsFile)
	if err != nil {
		log.Fatalf("load channels: %v", err)
	}

	rt := runtimeclient.New(cfg.PortalBaseURL, cfg.RuntimeToken)
	turnTimeout := time.Duration(cfg.TurnTimeoutSec) * time.Second

	// Shared with webhook + wecom_bot so peer sessions and msgid idempotency align.
	sessions := session.NewRouter(rt, 30*time.Second)
	idem := idempotency.NewStore(10 * time.Minute)
	pendingSwitch := pendingswitch.New()

	mux := http.NewServeMux()
	mux.Handle("POST /hooks/{channel_id}", adapter.NewWebhookHandler(adapter.WebhookDeps{
		Registry:      reg,
		Runtime:       rt,
		Sessions:      sessions,
		Idempotency:   idem,
		PendingSwitch: pendingSwitch,
		Reply:         reply.NewDispatcher(nil),
		TurnTimeout:   turnTimeout,
	}))
	adapter.MountWeb(mux, adapter.WebDeps{
		PortalBaseURL: cfg.PortalBaseURL,
		Runtime:       rt,
	})
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	adapter.StartWecomBots(ctx, adapter.WecomBotDeps{
		Registry:      reg,
		Runtime:       rt,
		Sessions:      sessions,
		Idempotency:   idem,
		PendingSwitch: pendingSwitch,
		TurnTimeout:   turnTimeout,
	})

	fmt.Printf("sixath-gateway version=%s listen=%s\n", Version, cfg.Listen)
	fmt.Printf("portal_base_url=%s turn_timeout_sec=%d channels_file=%s\n",
		cfg.PortalBaseURL, cfg.TurnTimeoutSec, cfg.ChannelsFile)

	srv := &http.Server{Addr: cfg.Listen, Handler: mux}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}()
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}
