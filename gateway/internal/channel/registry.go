package channel

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// Channel is a Gateway-managed inbound channel configuration.
type Channel struct {
	ID               string   `yaml:"id"`
	Type             string   `yaml:"type"`
	DefaultAgent     string   `yaml:"default_agent"`
	WebhookSecret    string   `yaml:"webhook_secret"`
	IPWhitelist      []string `yaml:"ip_whitelist"`
	Enabled          bool     `yaml:"enabled"`
	DefaultReplyMode string   `yaml:"default_reply_mode"` // async|sync
	BotID            string   `yaml:"bot_id"`
	Secret           string   `yaml:"secret"`
	BotNames         []string `yaml:"bot_names"`
	WSURL            string   `yaml:"ws_url"`
}

type channelsFile struct {
	Channels []Channel `yaml:"channels"`
}

// Registry looks up channels by id.
type Registry struct {
	byID map[string]Channel
}

// Load reads channels.yaml into a Registry.
func Load(path string) (*Registry, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read channels: %w", err)
	}
	var file channelsFile
	if err := yaml.Unmarshal(data, &file); err != nil {
		return nil, fmt.Errorf("parse channels: %w", err)
	}
	reg := &Registry{byID: make(map[string]Channel, len(file.Channels))}
	for _, ch := range file.Channels {
		if ch.ID == "" {
			return nil, fmt.Errorf("channel id is required")
		}
		if _, exists := reg.byID[ch.ID]; exists {
			return nil, fmt.Errorf("duplicate channel id %q", ch.ID)
		}
		if ch.IPWhitelist == nil {
			ch.IPWhitelist = []string{}
		}
		if ch.Type == "wecom_bot" && ch.Enabled {
			if ch.BotID == "" {
				return nil, fmt.Errorf("channel %q: bot_id is required for enabled wecom_bot", ch.ID)
			}
			if ch.Secret == "" {
				return nil, fmt.Errorf("channel %q: secret is required for enabled wecom_bot", ch.ID)
			}
		}
		reg.byID[ch.ID] = ch
	}
	return reg, nil
}

// Get returns a channel by id, or an error if unknown.
func (r *Registry) Get(id string) (Channel, error) {
	if r == nil || r.byID == nil {
		return Channel{}, fmt.Errorf("unknown channel id %q", id)
	}
	ch, ok := r.byID[id]
	if !ok {
		return Channel{}, fmt.Errorf("unknown channel id %q", id)
	}
	return ch, nil
}
