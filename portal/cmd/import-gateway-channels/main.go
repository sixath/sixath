// import-gateway-channels upserts Gateway channels.yaml into Portal channels.
//
// One-shot migration tool: yaml id → Portal channel_id. Does not delete yaml (see Task 10).
//
// Usage (from portal/):
//
//	go run ./cmd/import-gateway-channels -config ./configs -channels ../gateway/configs/channels.yaml
//	go run ./cmd/import-gateway-channels -config ./configs -channels ../gateway/configs/channels.yaml -dry-run
//
// Flags:
//
//	-config    Portal config path (file or directory; same idea as backend -conf)
//	-channels  Path to Gateway channels.yaml
//	-dry-run   Parse + report create/update without writing
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"backend/internal/biz"
	"backend/internal/conf"
	"backend/internal/data"

	"github.com/go-kratos/kratos/v2/config"
	"github.com/go-kratos/kratos/v2/config/file"
	"github.com/go-kratos/kratos/v2/log"
	"gopkg.in/yaml.v3"
)

// yamlChannel mirrors gateway/internal/channel.Channel plus optional allowed_agents.
type yamlChannel struct {
	ID               string    `yaml:"id"`
	Type             string    `yaml:"type"`
	DefaultAgent     string    `yaml:"default_agent"`
	WebhookSecret    string    `yaml:"webhook_secret"`
	IPWhitelist      []string  `yaml:"ip_whitelist"`
	Enabled          bool      `yaml:"enabled"`
	DefaultReplyMode string    `yaml:"default_reply_mode"`
	BotID            string    `yaml:"bot_id"`
	Secret           string    `yaml:"secret"`
	BotNames         []string  `yaml:"bot_names"`
	WSURL            string    `yaml:"ws_url"`
	CorpID           string    `yaml:"corp_id"`
	CorpSecret       string    `yaml:"corp_secret"`
	AllowedAgents    *[]string `yaml:"allowed_agents"` // nil = field absent → preserve Portal
}

type channelsFile struct {
	Channels []yamlChannel `yaml:"channels"`
}

func main() {
	configPath := flag.String("config", "./configs", "Portal config path, eg: -config ./configs")
	channelsPath := flag.String("channels", "", "path to Gateway channels.yaml")
	dryRun := flag.Bool("dry-run", false, "report create/update without writing")
	flag.Parse()

	if strings.TrimSpace(*channelsPath) == "" {
		fatalf("-channels is required")
	}

	chs, err := loadChannelsYAML(*channelsPath)
	if err != nil {
		fatalf("load channels: %v", err)
	}
	if len(chs) == 0 {
		fmt.Println("no channels in yaml; nothing to do")
		return
	}

	logger := log.With(log.NewStdLogger(os.Stderr),
		"ts", log.DefaultTimestamp,
		"caller", log.DefaultCaller,
		"service.name", "import-gateway-channels",
	)

	dataConf, authConf, err := loadPortalDataConfig(*configPath)
	if err != nil {
		if *dryRun {
			printParseOnlyDryRun(chs, err)
			return
		}
		fatalf("load config: %v", err)
	}

	d, cleanup, err := data.NewData(dataConf, authConf, logger)
	if err != nil {
		if *dryRun {
			printParseOnlyDryRun(chs, err)
			return
		}
		fatalf("database: %v", err)
	}
	defer cleanup()

	agentRepo := data.NewAgentRepo(d, dataConf, logger)
	uc := biz.NewChannelUsecase(data.NewChannelRepo(d, logger), agentRepo, logger)
	ctx := context.Background()

	var created, updated, skipped int
	for _, ch := range chs {
		action, err := upsertOne(ctx, uc, ch, *dryRun)
		if err != nil {
			fatalf("channel %q: %v", ch.ID, err)
		}
		switch action {
		case "create":
			created++
		case "update":
			updated++
		case "skip":
			skipped++
		}
		prefix := "would "
		if !*dryRun {
			prefix = ""
		}
		fmt.Printf("%s%s %s (type=%s enabled=%v)\n", prefix, action, ch.ID, ch.Type, ch.Enabled)
	}

	mode := "applied"
	if *dryRun {
		mode = "dry-run"
	}
	fmt.Printf("%s: create=%d update=%d skip=%d total=%d\n", mode, created, updated, skipped, len(chs))
}

func printParseOnlyDryRun(chs []yamlChannel, reason error) {
	fmt.Fprintf(os.Stderr, "import-gateway-channels: config/DB unavailable (%v); yaml parse-only dry-run\n", reason)
	for _, ch := range chs {
		fmt.Printf("would upsert %s (type=%s enabled=%v default_agent=%q)\n", ch.ID, ch.Type, ch.Enabled, ch.DefaultAgent)
	}
	fmt.Printf("dry-run(parse-only): total=%d\n", len(chs))
}

// resolveConfigFile prefers configs/config.yaml when -config is a directory so
// example JSON patches in the same folder do not break kratos merge.
func resolveConfigFile(path string) (string, error) {
	st, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	if st.IsDir() {
		return filepath.Join(path, "config.yaml"), nil
	}
	return path, nil
}

// loadPortalDataConfig loads only data (+ optional auth) to avoid scanning
// growth.* Duration values like "10m" that protobuf Duration rejects.
func loadPortalDataConfig(path string) (*conf.Data, *conf.Auth, error) {
	cfgFile, err := resolveConfigFile(path)
	if err != nil {
		return nil, nil, err
	}
	c := config.New(config.WithSource(file.NewSource(cfgFile)))
	defer c.Close()
	if err := c.Load(); err != nil {
		return nil, nil, err
	}
	var dataConf conf.Data
	if err := c.Value("data").Scan(&dataConf); err != nil {
		return nil, nil, fmt.Errorf("scan data: %w", err)
	}
	if dataConf.GetDatabase() == nil || dataConf.GetDatabase().GetSource() == "" {
		return nil, nil, errors.New("data.database.source required")
	}
	var authConf conf.Auth
	_ = c.Value("auth").Scan(&authConf) // optional for AutoMigrate ACL bootstrap
	auth := &authConf
	if authConf.GetBootstrapUserId() == "" && authConf.GetBootstrapOrgId() == "" {
		auth = nil
	}
	return &dataConf, auth, nil
}

func loadChannelsYAML(path string) ([]yamlChannel, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var file channelsFile
	if err := yaml.Unmarshal(data, &file); err != nil {
		return nil, err
	}
	seen := make(map[string]struct{}, len(file.Channels))
	out := make([]yamlChannel, 0, len(file.Channels))
	for _, ch := range file.Channels {
		id := strings.TrimSpace(ch.ID)
		if id == "" {
			return nil, fmt.Errorf("channel id is required")
		}
		if _, ok := seen[id]; ok {
			return nil, fmt.Errorf("duplicate channel id %q", id)
		}
		seen[id] = struct{}{}
		ch.ID = id
		if ch.IPWhitelist == nil {
			ch.IPWhitelist = []string{}
		}
		if ch.BotNames == nil {
			ch.BotNames = []string{}
		}
		out = append(out, ch)
	}
	return out, nil
}

func upsertOne(ctx context.Context, uc *biz.ChannelUsecase, ch yamlChannel, dryRun bool) (string, error) {
	existing, err := uc.GetByChannelID(ctx, ch.ID)
	if err != nil {
		if !errors.Is(err, biz.ErrChannelNotFound) {
			return "", err
		}
		if dryRun {
			return "create", nil
		}
		_, err := uc.Create(ctx, toCreate(ch))
		if err != nil {
			return "", err
		}
		return "create", nil
	}

	updates := protocolUpdates(ch)
	if da := strings.TrimSpace(ch.DefaultAgent); da != "" {
		updates["default_agent"] = da
	}
	if ch.AllowedAgents != nil {
		updates["allowed_agents"] = *ch.AllowedAgents
	}
	if len(updates) == 0 {
		return "skip", nil
	}
	if dryRun {
		return "update", nil
	}
	_, err = uc.Update(ctx, existing.ID, updates)
	if err != nil {
		return "", err
	}
	return "update", nil
}

func toCreate(ch yamlChannel) *biz.ChannelCreate {
	c := &biz.ChannelCreate{
		ChannelID:        ch.ID,
		Type:             ch.Type,
		Enabled:          ch.Enabled,
		WebhookSecret:    ch.WebhookSecret,
		IPWhitelist:      ch.IPWhitelist,
		DefaultReplyMode: ch.DefaultReplyMode,
		BotID:            ch.BotID,
		BotSecret:        ch.Secret,
		BotNames:         ch.BotNames,
		WSURL:            ch.WSURL,
		CorpID:           ch.CorpID,
		CorpSecret:       ch.CorpSecret,
	}
	if da := strings.TrimSpace(ch.DefaultAgent); da != "" {
		c.DefaultAgent = da
	}
	if ch.AllowedAgents != nil {
		c.AllowedAgents = *ch.AllowedAgents
	}
	return c
}

func protocolUpdates(ch yamlChannel) map[string]any {
	return map[string]any{
		"type":               ch.Type,
		"enabled":            ch.Enabled,
		"webhook_secret":     ch.WebhookSecret,
		"ip_whitelist":       ch.IPWhitelist,
		"default_reply_mode": ch.DefaultReplyMode,
		"bot_id":             ch.BotID,
		"bot_secret":         ch.Secret,
		"bot_names":          ch.BotNames,
		"ws_url":             ch.WSURL,
		"corp_id":            ch.CorpID,
		"corp_secret":        ch.CorpSecret,
	}
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "import-gateway-channels: "+format+"\n", args...)
	os.Exit(1)
}
