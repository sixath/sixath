package conf

import (
	"os"
	"strings"

	yaml "go.yaml.in/yaml/v2"
)

// ChatConfig holds Chat-surface process flags loaded from config.yaml
// (bypass of conf.proto, same pattern as LoadRuntimeFromConfigPath).
type ChatConfig struct {
	// PublicInboundEnabled opens legacy /api/v1 Chat create-session and send-message.
	// Default false: Gateway → /runtime/v1 is the public ingress.
	PublicInboundEnabled bool `yaml:"public_inbound_enabled"`
	// TurnToolSurfaceEnabled nil = omit (process default on). false 关闭本轮工具面收窄，全量挂上已绑定 RCA/MCP。
	// Env SATH_TURN_TOOL_SURFACE overlays when set.
	TurnToolSurfaceEnabled *bool `yaml:"turn_tool_surface_enabled,omitempty"`
}

type chatConfigYAML struct {
	Chat *ChatConfig `yaml:"chat"`
}

// LoadChatFromConfigPath reads chat.* from -conf file/dir config.yaml.
// Missing chat.public_inbound_enabled stays false. Env SATH_CHAT_PUBLIC_INBOUND_ENABLED overlays.
func LoadChatFromConfigPath(confPath string) (*ChatConfig, error) {
	out := &ChatConfig{}
	for _, p := range resolveConfigYAMLPaths(confPath) {
		data, err := os.ReadFile(p)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, err
		}
		var raw chatConfigYAML
		if err := yaml.Unmarshal(data, &raw); err != nil {
			return nil, err
		}
		if raw.Chat != nil {
			out.PublicInboundEnabled = raw.Chat.PublicInboundEnabled
			if raw.Chat.TurnToolSurfaceEnabled != nil {
				v := *raw.Chat.TurnToolSurfaceEnabled
				out.TurnToolSurfaceEnabled = &v
			}
		}
	}
	EnrichChatFromEnv(out)
	return out, nil
}

// EnrichChatFromEnv overlays SATH_CHAT_PUBLIC_INBOUND_ENABLED and SATH_TURN_TOOL_SURFACE when set.
func EnrichChatFromEnv(c *ChatConfig) {
	if c == nil {
		return
	}
	if v := strings.TrimSpace(os.Getenv("SATH_CHAT_PUBLIC_INBOUND_ENABLED")); v != "" {
		switch strings.ToLower(v) {
		case "1", "true", "yes", "on":
			c.PublicInboundEnabled = true
		case "0", "false", "no", "off":
			c.PublicInboundEnabled = false
		}
	}
	if v := strings.TrimSpace(os.Getenv("SATH_TURN_TOOL_SURFACE")); v != "" {
		on := true
		switch strings.ToLower(v) {
		case "0", "false", "no", "off":
			on = false
		}
		c.TurnToolSurfaceEnabled = &on
	}
}
