package channel

import (
	"fmt"
	"os"
	"sync"

	"gopkg.in/yaml.v3"
)

// Channel is a Gateway-managed inbound channel configuration.
type Channel struct {
	ID               string   `yaml:"id" json:"id"`
	Type             string   `yaml:"type" json:"type"`
	DefaultAgent     string   `yaml:"default_agent" json:"default_agent,omitempty"`
	WebhookSecret    string   `yaml:"webhook_secret" json:"webhook_secret,omitempty"`
	IPWhitelist      []string `yaml:"ip_whitelist" json:"ip_whitelist,omitempty"`
	Enabled          bool     `yaml:"enabled" json:"enabled"`
	DefaultReplyMode string   `yaml:"default_reply_mode" json:"default_reply_mode,omitempty"` // async|sync
	BotID            string   `yaml:"bot_id" json:"bot_id,omitempty"`
	Secret           string   `yaml:"secret" json:"secret,omitempty"`
	BotNames         []string `yaml:"bot_names" json:"bot_names,omitempty"`
	WSURL            string   `yaml:"ws_url" json:"ws_url,omitempty"`
	// Optional self-built app credentials to resolve from.userid → display name.
	CorpID     string `yaml:"corp_id" json:"corp_id,omitempty"`
	CorpSecret string `yaml:"corp_secret" json:"corp_secret,omitempty"`
}

type channelsFile struct {
	Channels []Channel `yaml:"channels"`
}

// Registry looks up channels by id. Safe for concurrent use.
type Registry struct {
	mu   sync.RWMutex
	byID map[string]Channel
}

// NewRegistry returns an empty thread-safe registry.
func NewRegistry() *Registry {
	return &Registry{byID: make(map[string]Channel)}
}

// Load reads channels.yaml into a Registry.
// Kept for import tools and tests; Gateway main will stop using it.
func Load(path string) (*Registry, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read channels: %w", err)
	}
	var file channelsFile
	if err := yaml.Unmarshal(data, &file); err != nil {
		return nil, fmt.Errorf("parse channels: %w", err)
	}
	seen := make(map[string]struct{}, len(file.Channels))
	chs := make([]Channel, 0, len(file.Channels))
	for _, ch := range file.Channels {
		if ch.ID == "" {
			return nil, fmt.Errorf("channel id is required")
		}
		if _, exists := seen[ch.ID]; exists {
			return nil, fmt.Errorf("duplicate channel id %q", ch.ID)
		}
		seen[ch.ID] = struct{}{}
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
		chs = append(chs, ch)
	}
	reg := NewRegistry()
	reg.ReplaceAll(chs)
	return reg, nil
}

// ReplaceAll atomically replaces the registry contents with chs.
func (r *Registry) ReplaceAll(chs []Channel) {
	if r == nil {
		return
	}
	byID := make(map[string]Channel, len(chs))
	for _, ch := range chs {
		if ch.ID == "" {
			continue
		}
		if ch.IPWhitelist == nil {
			ch.IPWhitelist = []string{}
		}
		byID[ch.ID] = ch
	}
	r.mu.Lock()
	r.byID = byID
	r.mu.Unlock()
}

// Snapshot returns a copy of configured channels.
func (r *Registry) Snapshot() []Channel {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.byID == nil {
		return nil
	}
	out := make([]Channel, 0, len(r.byID))
	for _, ch := range r.byID {
		out = append(out, ch)
	}
	return out
}

// Get returns a channel by id, or an error if unknown.
func (r *Registry) Get(id string) (Channel, error) {
	if r == nil {
		return Channel{}, fmt.Errorf("unknown channel id %q", id)
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.byID == nil {
		return Channel{}, fmt.Errorf("unknown channel id %q", id)
	}
	ch, ok := r.byID[id]
	if !ok {
		return Channel{}, fmt.Errorf("unknown channel id %q", id)
	}
	return ch, nil
}

// All returns a snapshot of configured channels.
func (r *Registry) All() []Channel {
	return r.Snapshot()
}
