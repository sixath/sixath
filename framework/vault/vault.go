package vault

import (
	"path/filepath"
	"strings"

	yaml "go.yaml.in/yaml/v2"
)

// Vault 是一个经验知识 vault 根目录。
type Vault struct {
	Root string // 绝对路径；空串表示未启用
}

// Discover 返回根目录为 root 的 Vault。root 为空返回零值（未启用），不报错。
func Discover(root string) *Vault {
	return &Vault{Root: root}
}

// zoneOf 依据路径相对 vault 根的位置判定分区。
func (v *Vault) zoneOf(path string) Zone {
	if v.Root == "" {
		return zoneUnknown
	}
	rel, err := filepath.Rel(v.Root, path)
	if err != nil {
		return zoneUnknown
	}
	if rel == "_agent_notes" || strings.HasPrefix(rel, "_agent_notes"+string(filepath.Separator)) {
		return zonePending
	}
	if rel == "ops" || strings.HasPrefix(rel, "ops"+string(filepath.Separator)) {
		return zonePromoted
	}
	if rel == "mine" || strings.HasPrefix(rel, "mine"+string(filepath.Separator)) {
		return zoneMine
	}
	if rel == "shared" || strings.HasPrefix(rel, "shared"+string(filepath.Separator)) {
		return zoneShared
	}
	return zoneUnknown
}

// zoneOfDir 依据单个目录名判定分区（用于 parseNote 从 path 推断）。
func zoneOfDir(dir string) Zone {
	switch dir {
	case "_agent_notes":
		return zonePending
	case "ops":
		return zonePromoted
	case "mine":
		return zoneMine
	case "shared":
		return zoneShared
	}
	return zoneUnknown
}

// parseNote 解析一条 markdown 笔记的 frontmatter 与正文。
func parseNote(raw, path string) (Note, error) {
	n := Note{
		Path: path,
		Zone: zoneOfDir(filepath.Base(filepath.Dir(path))),
	}

	if !strings.HasPrefix(raw, "---\n") && !strings.HasPrefix(raw, "---\r\n") {
		// 无 frontmatter，正文即全文。
		n.Body = strings.TrimSpace(raw)
		return n, nil
	}

	// 定位 frontmatter 结束分隔符（下一个 \n--- 或 \r\n---）。
	body := ""
	var rawYAML string
	found := false
	for _, delim := range []string{"\n---", "\r\n---"} {
		if idx := strings.Index(raw[len("---"):], delim); idx >= 0 {
			// rawYAML 是首行 "---" 之后、delim 之前的内容。
			start := len("---")
			end := start + idx
			rawYAML = raw[start:end]
			body = raw[end+len(delim):]
			found = true
			break
		}
	}
	if !found {
		// 有开头分隔符但找不到结束分隔符，视为无正文 frontmatter。
		n.Body = strings.TrimSpace(raw)
		return n, nil
	}

	var fm struct {
		Type    string   `yaml:"type"`
		Status  string   `yaml:"status"`
		Tags    []string `yaml:"tags"`
		Source  string   `yaml:"source"`
		Created string   `yaml:"created"`
	}
	if err := yaml.Unmarshal([]byte(rawYAML), &fm); err != nil {
		return n, err
	}

	n.Type = strings.TrimSpace(fm.Type)
	n.Source = strings.TrimSpace(fm.Source)
	n.Created = strings.TrimSpace(fm.Created)

	// Status 若为空，按 path 的 zone 推断。
	switch strings.TrimSpace(fm.Status) {
	case string(StatusPending):
		n.Status = StatusPending
	case string(StatusPromoted):
		n.Status = StatusPromoted
	default:
		switch n.Zone {
		case zonePending:
			n.Status = StatusPending
		case zonePromoted:
			n.Status = StatusPromoted
		}
	}

	// Tags 每个元素 TrimSpace。
	for _, t := range fm.Tags {
		if s := strings.TrimSpace(t); s != "" {
			n.Tags = append(n.Tags, s)
		}
	}

	n.Body = strings.TrimSpace(body)
	return n, nil
}
