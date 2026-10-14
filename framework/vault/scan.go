package vault

import (
	"os"
	"path/filepath"
	"strings"
)

// Scan 扫描 vault 根，返回可检索的正式笔记：
//   - 只收 zonePromoted（ops/ 目录）下 status==promoted 的 *.md；
//   - 跳过 .obsidian/ 目录（Obsidian 自身配置）；
//   - _agent_notes/（pending）、mine/、shared/、以及其他目录一律不收。
func Scan(root string) ([]Note, error) {
	v := Discover(root)
	if root == "" {
		return nil, nil
	}
	var notes []Note
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == ".obsidian" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.EqualFold(filepath.Ext(path), ".md") {
			return nil
		}
		if v.zoneOf(path) != zonePromoted {
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		n, err := parseNote(string(raw), path)
		if err != nil {
			return err
		}
		if n.Status != StatusPromoted {
			return nil
		}
		notes = append(notes, n)
		return nil
	})
	return notes, err
}
