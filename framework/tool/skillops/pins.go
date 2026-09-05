package toolskill

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

const pinnedSkillsRelPath = ".growth/pinned_skills.json"

type pinnedSkillsFile struct {
	Pinned []string `json:"pinned"`
}

// LoadPinnedSkillNames reads workspace-local pinned skill names.
func LoadPinnedSkillNames(workspaceRoot string) (map[string]struct{}, error) {
	out := make(map[string]struct{})
	if strings.TrimSpace(workspaceRoot) == "" {
		return out, nil
	}
	full, err := filepath.Abs(filepath.Join(workspaceRoot, pinnedSkillsRelPath))
	if err != nil {
		return nil, err
	}
	b, err := os.ReadFile(full)
	if err != nil {
		if os.IsNotExist(err) {
			return out, nil
		}
		return nil, err
	}
	var pf pinnedSkillsFile
	if err := json.Unmarshal(b, &pf); err != nil {
		return nil, err
	}
	for _, name := range pf.Pinned {
		name = strings.TrimSpace(name)
		if name != "" {
			out[name] = struct{}{}
		}
	}
	return out, nil
}

// IsSkillPinned reports whether name is pinned in workspace.
func IsSkillPinned(workspaceRoot, name string) (bool, error) {
	pins, err := LoadPinnedSkillNames(workspaceRoot)
	if err != nil {
		return false, err
	}
	_, ok := pins[name]
	return ok, nil
}
