package cases

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"
)

// DirName 为 agent 工作区下的案例目录名。
const DirName = "cases"

var caseIDRe = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

// FileStore 把每个案例存为 <dir>/<id>.md：YAML front matter 为数据，正文是给人看的渲染（加载时忽略）。
type FileStore struct {
	dir string
	mu  sync.Mutex
	now func() time.Time
}

// NewFileStore 创建以 dir 为根的案例库；目录在首次写入时创建。
func NewFileStore(dir string) *FileStore {
	return &FileStore{dir: dir, now: time.Now}
}

// ForWorkspace 返回 agent 工作区下的案例库。
func ForWorkspace(workspace string) *FileStore {
	return NewFileStore(filepath.Join(workspace, DirName))
}

func (s *FileStore) path(id string) (string, error) {
	if !caseIDRe.MatchString(id) {
		return "", fmt.Errorf("invalid case id %q", id)
	}
	return filepath.Join(s.dir, id+".md"), nil
}

func newCaseID(now time.Time) string {
	var b [3]byte
	_, _ = rand.Read(b[:])
	return "case-" + now.Format("20060102-150405") + "-" + hex.EncodeToString(b[:])
}

func (s *FileStore) Search(query string, k int, includeDrafts bool) ([]Hit, error) {
	all, err := s.List("")
	if err != nil {
		return nil, err
	}
	docs := all[:0]
	for _, c := range all {
		if includeDrafts || c.Status == StatusConfirmed {
			docs = append(docs, c)
		}
	}
	return rank(query, docs, k), nil
}

func (s *FileStore) Get(id string) (Case, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.load(id)
}

func (s *FileStore) SaveDraft(c Case) (Case, error) {
	if strings.TrimSpace(c.Symptom) == "" {
		return Case{}, errors.New("case symptom is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if c.ID == "" {
		c.ID = newCaseID(s.now())
	} else if old, err := s.load(c.ID); err == nil && old.Status == StatusConfirmed {
		return Case{}, fmt.Errorf("case %s is already confirmed", c.ID)
	}
	c.Status = StatusDraft
	c.ConfirmedAt, c.ConfirmedBy = nil, ""
	if c.CreatedAt.IsZero() {
		c.CreatedAt = s.now()
	}
	return c, s.write(c)
}

func (s *FileStore) Update(id string, p Patch) (Case, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, err := s.load(id)
	if err != nil {
		return Case{}, err
	}
	p.apply(&c)
	if strings.TrimSpace(c.Symptom) == "" {
		return Case{}, errors.New("case symptom is required")
	}
	return c, s.write(c)
}

func (s *FileStore) Confirm(id, by string) (Case, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, err := s.load(id)
	if err != nil {
		return Case{}, err
	}
	if c.Status == StatusConfirmed {
		return c, nil
	}
	now := s.now()
	c.Status, c.ConfirmedAt, c.ConfirmedBy = StatusConfirmed, &now, strings.TrimSpace(by)
	return c, s.write(c)
}

func (s *FileStore) Discard(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, err := s.path(id)
	if err != nil {
		return err
	}
	if err := os.Remove(p); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return ErrNotFound
		}
		return err
	}
	return nil
}

func (s *FileStore) List(status string) ([]Case, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var out []Case
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".md") {
			continue
		}
		c, err := s.load(strings.TrimSuffix(name, ".md"))
		if err != nil {
			continue
		}
		if status == "" || c.Status == status {
			out = append(out, c)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	return out, nil
}

func (s *FileStore) load(id string) (Case, error) {
	p, err := s.path(id)
	if err != nil {
		return Case{}, err
	}
	data, err := os.ReadFile(p)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Case{}, ErrNotFound
		}
		return Case{}, err
	}
	front, ok := frontMatter(data)
	if !ok {
		return Case{}, fmt.Errorf("case %s: missing front matter", id)
	}
	var c Case
	if err := yaml.Unmarshal(front, &c); err != nil {
		return Case{}, fmt.Errorf("case %s: %w", id, err)
	}
	c.ID = id
	if c.Status != StatusConfirmed {
		c.Status = StatusDraft
	}
	return c, nil
}

func (s *FileStore) write(c Case) error {
	p, err := s.path(c.ID)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(s.dir, 0o755); err != nil {
		return err
	}
	front, err := yaml.Marshal(c)
	if err != nil {
		return err
	}
	var b bytes.Buffer
	b.WriteString("---\n")
	b.Write(front)
	b.WriteString("---\n\n")
	b.WriteString(Render(c))
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, b.Bytes(), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, p)
}

func frontMatter(data []byte) ([]byte, bool) {
	data = bytes.ReplaceAll(data, []byte("\r\n"), []byte("\n"))
	if !bytes.HasPrefix(data, []byte("---\n")) {
		return nil, false
	}
	rest := data[4:]
	end := bytes.Index(rest, []byte("\n---\n"))
	if end < 0 {
		if bytes.HasSuffix(rest, []byte("\n---")) {
			return rest[:len(rest)-4], true
		}
		return nil, false
	}
	return rest[:end+1], true
}

// Render 把案例渲染成 markdown，用于正文与确认卡片预览。
func Render(c Case) string {
	var b strings.Builder
	title := c.Title
	if title == "" {
		title = c.Symptom
	}
	fmt.Fprintf(&b, "# %s\n\n", title)
	fmt.Fprintf(&b, "- 状态：%s\n- 症状：%s\n", c.Status, c.Symptom)
	if len(c.Signature) > 0 {
		fmt.Fprintf(&b, "- 特征：%s\n", strings.Join(c.Signature, "、"))
	}
	if c.Onset != "" {
		fmt.Fprintf(&b, "- 开始：%s\n", c.Onset)
	}
	if c.LastGood != "" {
		fmt.Fprintf(&b, "- 最后正常：%s\n", c.LastGood)
	}
	if len(c.Chain) > 0 {
		b.WriteString("\n## 根因链\n\n")
		for _, l := range c.Chain {
			fmt.Fprintf(&b, "- [%s] %s\n", l.Kind, l.Statement)
		}
	}
	if len(c.KeyEvidence) > 0 {
		b.WriteString("\n## 关键证据\n\n")
		for _, e := range c.KeyEvidence {
			if e.Tool != "" {
				fmt.Fprintf(&b, "- (%s) %s\n", e.Tool, e.Quote)
			} else {
				fmt.Fprintf(&b, "- %s\n", e.Quote)
			}
		}
	}
	if len(c.VerifyProbes) > 0 {
		b.WriteString("\n## 验证探针\n\n")
		for _, p := range c.VerifyProbes {
			args, _ := yaml.Marshal(p.Args)
			fmt.Fprintf(&b, "- %s %s", p.Tool, strings.TrimSpace(strings.ReplaceAll(string(args), "\n", "; ")))
			if p.Expect != "" {
				fmt.Fprintf(&b, "（预期：%s）", p.Expect)
			}
			b.WriteString("\n")
		}
	}
	if c.Fix != "" {
		fmt.Fprintf(&b, "\n## 修复\n\n%s\n", c.Fix)
	}
	return b.String()
}
