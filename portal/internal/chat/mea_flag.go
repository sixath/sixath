package chat

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"backend/internal/biz"

	"github.com/sixath/framework/mea"
)

const (
	meaEnv            = "SATH_MEA"
	meaPilotAgentsEnv = "SATH_MEA_PILOT_AGENTS"
)

var (
	meaDataMu   sync.RWMutex
	meaDataRoot string
)

// MEAEnabled reports whether MEA is globally enabled via env.
// SATH_MEA=1|true|yes|on → true; default false when unset.
func MEAEnabled() bool {
	v := strings.ToLower(strings.TrimSpace(os.Getenv(meaEnv)))
	switch v {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

// MEAEnabledForAgent reports whether MEA should run for this agent.
// True if agent runtime_tools.mea_enabled, OR global SATH_MEA, OR agent id in SATH_MEA_PILOT_AGENTS.
func MEAEnabledForAgent(agentID string, agentMEAEnabled bool) bool {
	if agentMEAEnabled {
		return true
	}
	if MEAEnabled() {
		return true
	}
	agentID = strings.TrimSpace(agentID)
	if agentID == "" {
		return false
	}
	for _, id := range strings.Split(os.Getenv(meaPilotAgentsEnv), ",") {
		if strings.TrimSpace(id) == agentID {
			return true
		}
	}
	return false
}

// MEAEnabledForAgentMeta is a convenience wrapper over AgentMeta.RuntimeTools.MEAEnabled.
func MEAEnabledForAgentMeta(meta *biz.AgentMeta) bool {
	if meta == nil {
		return MEAEnabledForAgent("", false)
	}
	return MEAEnabledForAgent(meta.ID, meta.RuntimeTools.MEAEnabled)
}

// SetMEADataRoot supplies Portal's data.data_root for MEA session JSON storage.
// Call once at startup. Empty root clears the setting.
func SetMEADataRoot(root string) {
	meaDataMu.Lock()
	defer meaDataMu.Unlock()
	meaDataRoot = strings.TrimSpace(root)
}

// MEADataRoot returns the configured data root (Portal data.data_root).
func MEADataRoot() string {
	meaDataMu.RLock()
	defer meaDataMu.RUnlock()
	return meaDataRoot
}

// MEAFileStore returns a FileStore under {data_root}/mea.
// If the data root is empty, returns (nil, error).
func MEAFileStore() (*mea.FileStore, error) {
	root := MEADataRoot()
	if root == "" {
		return nil, errors.New("mea: data root not set")
	}
	return mea.NewFileStore(filepath.Join(root, "mea")), nil
}
