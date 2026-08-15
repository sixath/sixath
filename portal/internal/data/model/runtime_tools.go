package model

import (
	"database/sql/driver"
	"encoding/json"
	"errors"
)

// RuntimeToolsConfig is stored as JSON on agents.runtime_tools.
type RuntimeToolsConfig struct {
	MemoryWriteEnabled        bool  `json:"memory_write_enabled"`
	SkillRuntimeManageEnabled bool  `json:"skill_runtime_manage_enabled"`
	TodoEnabled               bool  `json:"todo_enabled"`
	WorkspaceFilesEnabled     bool  `json:"workspace_files_enabled"`
	WebToolsEnabled           bool  `json:"web_tools_enabled"`
	TerminalLocalEnabled      bool  `json:"terminal_local_enabled"`
	CronjobToolEnabled        bool  `json:"cronjob_tool_enabled"`
	BrowserEnabled            bool  `json:"browser_enabled"`
	MEAEnabled                bool  `json:"mea_enabled"`
	HybridRecall              *bool `json:"hybrid_recall,omitempty"` // unset = on; presence preserved
	HubGovernance                   *string `json:"hub_governance,omitempty"`
	HubKnowledge                    *string `json:"hub_knowledge,omitempty"`
	HubFallbackToDefaultOnReadError *bool   `json:"hub_fallback_to_default_on_read_error,omitempty"`
}

func (c RuntimeToolsConfig) Value() (driver.Value, error) {
	return json.Marshal(c)
}

func (c *RuntimeToolsConfig) Scan(value interface{}) error {
	if value == nil {
		*c = RuntimeToolsConfig{}
		return nil
	}
	bytes, ok := value.([]byte)
	if !ok {
		s, ok := value.(string)
		if !ok {
			return errors.New("failed to unmarshal RuntimeToolsConfig")
		}
		bytes = []byte(s)
	}
	return json.Unmarshal(bytes, c)
}
