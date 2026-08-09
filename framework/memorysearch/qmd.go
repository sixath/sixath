package memorysearch

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/sixath/framework/config"
)

// QmdMemoryManager 调用外部 qmd 命令实现记忆检索（Phase 3）。
type QmdMemoryManager struct {
	cmd         string
	workspace   string
	collections []string
}

// qmdSearchHit qmd search --json 单条结果。
type qmdSearchHit struct {
	Docid   string  `json:"docid"`
	Score   float64 `json:"score"`
	Path    string  `json:"path"`
	Snippet string  `json:"snippet"`
}

// qmdSearchOutput qmd search --json 输出。
type qmdSearchOutput struct {
	Hits []qmdSearchHit `json:"hits"`
}

// NewQmdMemoryManager 创建 QMD 管理器。若 qmd 命令不可用则返回错误。
func NewQmdMemoryManager(qmd *config.QmdConfig, workspace string) (*QmdMemoryManager, error) {
	if qmd == nil || qmd.Command == "" {
		return nil, fmt.Errorf("memorysearch: qmd command is empty")
	}
	cmd := qmd.Command
	if cmd == "qmd" {
		if _, err := exec.LookPath("qmd"); err != nil {
			return nil, fmt.Errorf("memorysearch: qmd not found in PATH: %w", err)
		}
	}
	return &QmdMemoryManager{
		cmd:       cmd,
		workspace: workspace,
	}, nil
}

// Search 实现 MemorySearchManager。
func (q *QmdMemoryManager) Search(ctx context.Context, query string, opts *SearchOpts) ([]MemorySearchResult, error) {
	maxResults := 10
	minScore := 0.3
	if opts != nil {
		if opts.MaxResults > 0 {
			maxResults = opts.MaxResults
		}
		if opts.MinScore > 0 {
			minScore = opts.MinScore
		}
	}
	args := []string{"search", query, "--json", "-n", fmt.Sprintf("%d", maxResults)}
	if minScore > 0 {
		args = append(args, "--min-score", fmt.Sprintf("%g", minScore))
	}
	out, err := q.run(ctx, args...)
	if err != nil {
		return nil, err
	}
	var parsed qmdSearchOutput
	if err := json.Unmarshal(out, &parsed); err != nil {
		return nil, fmt.Errorf("memorysearch: qmd search json parse: %w", err)
	}
	results := make([]MemorySearchResult, 0, len(parsed.Hits))
	for _, h := range parsed.Hits {
		startLine, endLine := parsePathLines(h.Path)
		results = append(results, MemorySearchResult{
			Path:      h.Path,
			StartLine: startLine,
			EndLine:   endLine,
			Score:     h.Score,
			Snippet:   h.Snippet,
			Source:    "qmd",
			Citation:  fmt.Sprintf("%s#L%d-%d", h.Path, startLine, endLine),
		})
	}
	return results, nil
}

func parsePathLines(path string) (start, end int) {
	// path 可能含 #L1-5 或 #L10
	if idx := strings.Index(path, "#L"); idx >= 0 {
		rest := path[idx+2:]
		var a, b int
		if n, _ := fmt.Sscanf(rest, "%d-%d", &a, &b); n >= 1 {
			start = a
			end = b
			if start <= 0 {
				start = 1
			}
			if end < start {
				end = start
			}
		}
	}
	if start == 0 {
		start = 1
	}
	if end == 0 {
		end = start
	}
	return start, end
}

// ReadFile 实现 MemorySearchManager。优先尝试 qmd get，失败则读磁盘。
func (q *QmdMemoryManager) ReadFile(ctx context.Context, params *ReadFileParams) (*ReadFileResult, error) {
	if params == nil || params.RelPath == "" {
		return nil, fmt.Errorf("memorysearch: relPath is required")
	}
	cleaned := filepath.Clean(params.RelPath)
	if strings.Contains(cleaned, "..") {
		return nil, fmt.Errorf("memorysearch: path must not escape workspace")
	}
	if !strings.HasSuffix(strings.ToLower(cleaned), ".md") {
		return nil, fmt.Errorf("memorysearch: only .md files allowed")
	}
	// 尝试 qmd get
	out, err := q.run(ctx, "get", cleaned)
	if err == nil && len(out) > 0 {
		text := string(out)
		if params.From > 0 || params.Lines > 0 {
			lines := strings.Split(text, "\n")
			from := params.From - 1
			if from < 0 {
				from = 0
			}
			to := from + params.Lines
			if params.Lines <= 0 {
				to = len(lines)
			}
			if from < len(lines) {
				if to > len(lines) {
					to = len(lines)
				}
				text = strings.Join(lines[from:to], "\n")
			} else {
				text = ""
			}
		}
		return &ReadFileResult{Text: text, Path: params.RelPath}, nil
	}
	// 回退：读磁盘
	fullPath := filepath.Join(q.workspace, cleaned)
	absFull, err := filepath.Abs(fullPath)
	if err != nil {
		return nil, err
	}
	absWorkspace, _ := filepath.Abs(q.workspace)
	if !strings.HasPrefix(absFull, absWorkspace+string(filepath.Separator)) && absFull != absWorkspace {
		return nil, fmt.Errorf("memorysearch: path must be under workspace")
	}
	data, err := os.ReadFile(absFull)
	if err != nil {
		return nil, err
	}
	text := string(data)
	if params.From > 0 || params.Lines > 0 {
		lines := strings.Split(text, "\n")
		from := params.From - 1
		if from < 0 {
			from = 0
		}
		to := from + params.Lines
		if params.Lines <= 0 {
			to = len(lines)
		}
		if from < len(lines) {
			if to > len(lines) {
				to = len(lines)
			}
			text = strings.Join(lines[from:to], "\n")
		} else {
			text = ""
		}
	}
	return &ReadFileResult{Text: text, Path: params.RelPath}, nil
}

// Status 实现 MemorySearchManager。
func (q *QmdMemoryManager) Status(ctx context.Context) (*MemoryProviderStatus, error) {
	return &MemoryProviderStatus{
		Backend: "qmd",
		Files:   0,
		Chunks:  0,
		Vector:  true,
		FTS:     true,
	}, nil
}

// Sync 实现 MemorySearchManager。QMD 自管理索引，可执行 qmd embed 触发更新。
func (q *QmdMemoryManager) Sync(ctx context.Context, params *SyncParams) error {
	_, err := q.run(ctx, "embed")
	return err
}

func (q *QmdMemoryManager) run(ctx context.Context, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, q.cmd, args...)
	cmd.Dir = q.workspace
	return cmd.Output()
}
