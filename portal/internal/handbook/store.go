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
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// ErrBadPath rejects repo ids and page paths that are empty, absolute, escape the skill
// directory or contain anything but slug-like segments.
var ErrBadPath = errors.New("handbook: invalid page path")

// ErrVersionExists means the version directory is already published and is left untouched.
var ErrVersionExists = errors.New("handbook: version already published")

const (
	maxReadBytes = 256 << 10
	tmpMarker    = ".tmp-"
	// staleTmpAge exceeds the build lease so prune never removes a live builder's staging dir.
	staleTmpAge = time.Hour
)

var (
	repoIDRe   = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)
	segmentRe  = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]*$`)
	errBadRepo = fmt.Errorf("%w: repo id", ErrBadPath)
)

func checkID(id string) error {
	if !repoIDRe.MatchString(id) {
		return errBadRepo
	}
	return nil
}

// checkRel requires a slash path of slug-like segments that is local on this OS
// (filepath.IsLocal rejects Windows device names such as AUX or CON.md).
func checkRel(rel string) error {
	for _, seg := range strings.Split(rel, "/") {
		if !segmentRe.MatchString(seg) || strings.HasSuffix(seg, ".") {
			return ErrBadPath
		}
	}
	if !filepath.IsLocal(filepath.FromSlash(rel)) {
		return ErrBadPath
	}
	return nil
}

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
// An already published v<version> is never replaced; ErrVersionExists is returned instead.
func (s Store) Publish(id string, version int, files map[string][]byte, keep int) error {
	if err := checkID(id); err != nil {
		return err
	}
	if version <= 0 {
		return fmt.Errorf("handbook: invalid version %d", version)
	}
	for rel := range files {
		if err := checkRel(rel); err != nil {
			return fmt.Errorf("%w: %q", err, rel)
		}
	}
	final := s.VersionDir(id, version)
	if _, err := os.Lstat(final); err == nil {
		return fmt.Errorf("%w: v%d", ErrVersionExists, version)
	}
	if err := os.MkdirAll(s.repoDir(id), 0o755); err != nil {
		return err
	}
	tmp, err := os.MkdirTemp(s.repoDir(id), "v"+strconv.Itoa(version)+tmpMarker+"*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	for rel, b := range files {
		p := filepath.Join(tmp, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(p, b, 0o644); err != nil {
			return err
		}
	}
	if _, err := os.Lstat(final); err == nil {
		return fmt.Errorf("%w: v%d", ErrVersionExists, version)
	}
	if err := os.Rename(tmp, final); err != nil {
		if _, serr := os.Lstat(final); serr == nil {
			return fmt.Errorf("%w: v%d", ErrVersionExists, version)
		}
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
	if err := checkID(id); err != nil {
		return 0, err
	}
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
		if strings.Contains(name, tmpMarker) || strings.HasSuffix(name, ".tmp") {
			if info, err := e.Info(); err == nil && time.Since(info.ModTime()) > staleTmpAge {
				_ = os.RemoveAll(filepath.Join(s.repoDir(id), name))
			}
			continue
		}
		n, err := strconv.Atoi(strings.TrimPrefix(name, "v"))
		if err == nil && n <= current-keep {
			_ = os.RemoveAll(filepath.Join(s.repoDir(id), name))
		}
	}
}

// LLMCache returns the LLM cache of a repository; the directory is created on first write.
func (s Store) LLMCache(id string) (LLMCache, error) {
	if err := checkID(id); err != nil {
		return LLMCache{}, err
	}
	return LLMCache{Dir: filepath.Join(s.repoDir(id), "llm")}, nil
}

// ReadFacts loads the facts of a published version.
func (s Store) ReadFacts(id string, v int) (*Facts, error) {
	if err := checkID(id); err != nil {
		return nil, err
	}
	dir := filepath.Join(s.VersionDir(id, v), "facts")
	f := &Facts{Symbols: map[string][]Symbol{}}
	for name, dst := range map[string]any{
		"files.json": &f.Files, "symbols.json": &f.Symbols, "registers.json": &f.Registers, "packages.json": &f.Packages,
	} {
		ok, err := readJSON(filepath.Join(dir, name), dst)
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, fmt.Errorf("handbook: %s v%d: missing facts/%s: %w", id, v, name, fs.ErrNotExist)
		}
	}
	var mod GoModule
	ok, err := readJSON(filepath.Join(dir, "module.json"), &mod)
	if err != nil {
		return nil, err
	}
	if ok {
		f.Module = &mod
	}
	if f.Symbols == nil {
		f.Symbols = map[string][]Symbol{}
	}
	return f, nil
}

// ListSkillFiles lists the files of a version's skill directory as sorted slash paths.
func (s Store) ListSkillFiles(id string, v int) ([]string, error) {
	if err := checkID(id); err != nil {
		return nil, err
	}
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
	if err := checkID(id); err != nil {
		return nil, err
	}
	clean := path.Clean(strings.ReplaceAll(rel, `\`, "/"))
	if strings.TrimSpace(rel) == "" || path.IsAbs(clean) || clean == "." || clean == ".." ||
		strings.HasPrefix(clean, "../") || filepath.VolumeName(filepath.FromSlash(clean)) != "" {
		return nil, ErrBadPath
	}
	if err := checkRel(clean); err != nil {
		return nil, err
	}
	f, err := os.OpenInRoot(s.SkillDir(id, v), filepath.FromSlash(clean))
	if err != nil {
		return nil, err
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if fi.IsDir() {
		return nil, ErrBadPath
	}
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
	if err := tmp.Sync(); err != nil {
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
