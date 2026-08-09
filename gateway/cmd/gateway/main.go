package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/sixath/gateway/internal/adapter"
	"github.com/sixath/gateway/internal/channel"
	"github.com/sixath/gateway/internal/config"
	"github.com/sixath/gateway/internal/idempotency"
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

	mux := http.NewServeMux()
	mux.Handle("POST /hooks/{channel_id}", adapter.NewWebhookHandler(adapter.WebhookDeps{
		Registry:    reg,
		Runtime:     rt,
		Sessions:    session.NewRouter(rt, 30*time.Second),
		Idempotency: idempotency.NewStore(10 * time.Minute),
		Reply:       reply.NewDispatcher(nil),
		TurnTimeout: turnTimeout,
	}))
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	fmt.Printf("sixath-gateway version=%s listen=%s\n", Version, cfg.Listen)
	fmt.Printf("portal_base_url=%s turn_timeout_sec=%d channels_file=%s\n",
		cfg.PortalBaseURL, cfg.TurnTimeoutSec, cfg.ChannelsFile)

	log.Fatal(http.ListenAndServe(cfg.Listen, mux))
}
