package main

import (
	"flag"
	"fmt"
	"log"
	"os"

	"github.com/sixath/gateway/internal/config"
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

	fmt.Printf("sixath-gateway version=%s listen=%s\n", Version, cfg.Listen)
	fmt.Printf("portal_base_url=%s turn_timeout_sec=%d channels_file=%s\n",
		cfg.PortalBaseURL, cfg.TurnTimeoutSec, cfg.ChannelsFile)

	// Scaffold only: adapters land in later tasks.
	os.Exit(0)
}
