package handbook

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// ErrBadPath rejects page paths that are empty, absolute or escape the skill directory.
var ErrBadPath = errors.New("handbook: invalid page path")

const maxReadBytes = 256 << 10

// Store lays out handbook versions under Root (normally {data_root}/handbooks):
// repos/<id>/v<N>/... and repos/<id>/current.json.
type Store struct{ Root string }

type currentPointer struct {
	Version int `json:"version"`
}

func (s Store) repoDir(id string) string { return filepath.Join(s.Root, "repos", id) }

// VersionDir is the directory of one published version.
func (s Store) VersionDir(id string, v int) string {
	return filepath.Join(s.repoDir(id), "v"+strconv.Itoa(v))
}

// SkillDir is the agent-facing skill directory of one version.
func (s Store) SkillDir(id string, v int) string { return filepath.Join(s.VersionDir(id, v), "skill") }

// Publish writes files into v<version>, points current.json at it and keeps the newest keep
// versions. Older versions stay readable until pruned so in-flight chats are not broken.
func (s Store) Publish(id string, version int, files map[string][]byte, keep int) error {
	if version <= 0 {
		return fmt.Errorf("handbook: invalid version %d", version)
	}
	final := s.VersionDir(id, version)
	tmp := final + ".tmp"
	if err := os.RemoveAll(tmp); err != nil {
		return err
	}
	for rel, b := range files {
		p := filepath.Join(tmp, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(p, b, 0o644); err != nil {
			return err
		}
	}
	if err := os.RemoveAll(final); err != nil {
		return err
	}
	if err := os.Rename(tmp, final); err != nil {
		return err
	}
	ptr, err := json.Marshal(currentPointer{Version: version})
	if err != nil {
		return err
	}
	if err := WriteFileAtomic(filepath.Join(s.repoDir(id), "current.json"), ptr); err != nil {
		return err
	}
	s.prune(id, version, keep)
	return nil
}

// Current returns the version current.json points at, or 0 when nothing is published.
func (s Store) Current(id string) (int, error) {
	b, err := os.ReadFile(filepath.Join(s.repoDir(id), "current.json"))
	if errors.Is(err, fs.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	var p currentPointer
	if err := json.Unmarshal(b, &p); err != nil {
		return 0, err
	}
	return p.Version, nil
}

func (s Store) prune(id string, current, keep int) {
	if keep < 1 {
		keep = 1
	}
	ents, err := os.ReadDir(s.repoDir(id))
	if err != nil {
		return
	}
	for _, e := range ents {
		name := e.Name()
		if !e.IsDir() || !strings.HasPrefix(name, "v") {
			continue
		}
		if strings.HasSuffix(name, ".tmp") {
			_ = os.RemoveAll(filepath.Join(s.repoDir(id), name))
			continue
		}
		n, err := strconv.Atoi(strings.TrimPrefix(name, "v"))
		if err == nil && n <= current-keep {
			_ = os.RemoveAll(filepath.Join(s.repoDir(id), name))
		}
	}
}

// ListSkillFiles lists the files of a version's skill directory as sorted slash paths.
func (s Store) ListSkillFiles(id string, v int) ([]string, error) {
	root := s.SkillDir(id, v)
	var out []string
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		out = append(out, filepath.ToSlash(rel))
		return nil
	})
	sort.Strings(out)
	return out, err
}

// ReadSkillFile reads one page of a version's skill directory, capped at 256 KiB.
func (s Store) ReadSkillFile(id string, v int, rel string) ([]byte, error) {
	clean := path.Clean(strings.ReplaceAll(rel, `\`, "/"))
	if strings.TrimSpace(rel) == "" || path.IsAbs(clean) || clean == "." || clean == ".." ||
		strings.HasPrefix(clean, "../") || filepath.VolumeName(filepath.FromSlash(clean)) != "" {
		return nil, ErrBadPath
	}
	f, err := os.Open(filepath.Join(s.SkillDir(id, v), filepath.FromSlash(clean)))
	if err != nil {
		return nil, err
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, maxReadBytes))
	if err != nil {
		return nil, err
	}
	return b, nil
}

// WriteFileAtomic writes b to a temp file next to p and renames it over p.
func WriteFileAtomic(p string, b []byte) error {
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(p), "."+filepath.Base(p)+".*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		os.Remove(name)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(name)
		return err
	}
	if err := os.Rename(name, p); err != nil {
		os.Remove(name)
		return err
	}
	return nil
}
