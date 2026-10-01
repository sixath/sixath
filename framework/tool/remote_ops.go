package tool

import (
	"fmt"
	"strconv"
	"strings"
)

// 远程排查常用操作：模型只给结构化参数（op/path/pattern/lines），由工具拼出安全的命令，
// 避免每个 agent 反复摸索 cmd.exe / shell 的写法，也避免日志过大时只拿到文件开头。
const (
	RemoteOpListRecent = "ls_recent"
	RemoteOpTail       = "tail"
	RemoteOpGrep       = "grep"
	RemoteOpFind       = "find"

	remoteOpDefaultLines = 100
	remoteOpMaxLines     = 1000
	remoteOpMaxPatterns  = 10
)

type remoteOp struct {
	Op          string
	Path        string
	Patterns    []string
	Lines       int
	IncludeDirs bool
	Recursive   bool
}

// remoteOpSchema 为 vm_run_cmd / ssh_exec 共用的 op 参数定义。
func remoteOpSchema() map[string]any {
	return map[string]any{
		"op": map[string]any{
			"type": "string",
			"enum": []string{RemoteOpListRecent, RemoteOpTail, RemoteOpGrep, RemoteOpFind},
			"description": "Built-in read-only operation used instead of a raw command: " +
				"ls_recent = list files in path, newest first; " +
				"tail = last N lines of the file at path; " +
				"grep = lines of the file at path containing any of the keywords (case-insensitive literal, OR), last N matches kept; " +
				"find = recursively search the directory at path for files named pattern/patterns, returns full paths. " +
				"Prefer op=grep over piping findstr/grep yourself (piped findstr fails on long lines). " +
				"When a file's location is unknown or differs between hosts, locate it with op=find instead of guessing a directory " +
				"(path = drive or directory to search, patterns = exact file names or wildcards like *.log; search each drive separately).",
		},
		"path":    map[string]any{"type": "string", "description": "Directory (ls_recent, find root) or file (tail/grep) for op."},
		"pattern": map[string]any{"type": "string", "description": "Literal keyword for op=grep, or file name for op=find (wildcards * ? allowed)."},
		"patterns": map[string]any{
			"type":        "array",
			"items":       map[string]any{"type": "string"},
			"description": fmt.Sprintf("Several literal keywords for op=grep (a line matches if it contains any of them), or several file names for op=find (max %d). Combined with pattern.", remoteOpMaxPatterns),
		},
		"lines": map[string]any{"type": "integer", "description": "Max lines returned for op (default 100, max 1000)."},
		"include_dirs": map[string]any{
			"type":        "boolean",
			"description": "op=ls_recent: also list sub-directories (newest first). Use it to spot what changed recently near a failing component (state/cache/patch/config dirs), not only log files.",
		},
		"recursive": map[string]any{
			"type":        "boolean",
			"description": "op=ls_recent: walk sub-directories too (output grouped per directory, newest first inside each). Point path at a narrow directory; large trees are cut to `lines`.",
		},
	}
}

// parseRemoteOp 返回 nil 表示未使用 op。
func parseRemoteOp(params map[string]any) (*remoteOp, error) {
	name, _ := params["op"].(string)
	name = strings.TrimSpace(strings.ToLower(name))
	if name == "" {
		return nil, nil
	}
	op := &remoteOp{Op: name, Lines: remoteOpDefaultLines}
	op.Path, _ = params["path"].(string)
	op.Path = strings.TrimSpace(op.Path)
	op.Patterns = remoteOpPatterns(params)
	op.IncludeDirs, _ = params["include_dirs"].(bool)
	op.Recursive, _ = params["recursive"].(bool)
	if n := intFromParam(params["lines"], 0); n > 0 {
		op.Lines = n
	}
	if op.Lines > remoteOpMaxLines {
		op.Lines = remoteOpMaxLines
	}
	switch name {
	case RemoteOpListRecent, RemoteOpTail:
	case RemoteOpGrep, RemoteOpFind:
		if len(op.Patterns) == 0 {
			return nil, fmt.Errorf("op=%s requires pattern or patterns", name)
		}
		if len(op.Patterns) > remoteOpMaxPatterns {
			return nil, fmt.Errorf("op=%s accepts at most %d patterns", name, remoteOpMaxPatterns)
		}
		if name == RemoteOpFind {
			for _, p := range op.Patterns {
				if strings.ContainsAny(p, `\/`) {
					return nil, fmt.Errorf("op=find patterns must be bare file names without directories (got %q); put the directory in path", p)
				}
			}
		}
	default:
		return nil, fmt.Errorf("unknown op %q (use %s, %s, %s or %s)", name, RemoteOpListRecent, RemoteOpTail, RemoteOpGrep, RemoteOpFind)
	}
	if op.Path == "" {
		return nil, fmt.Errorf("op=%s requires path", name)
	}
	return op, nil
}

func remoteOpPatterns(params map[string]any) []string {
	var out []string
	seen := map[string]bool{}
	add := func(s string) {
		if strings.TrimSpace(s) == "" || seen[s] {
			return
		}
		seen[s] = true
		out = append(out, s)
	}
	if s, ok := params["pattern"].(string); ok {
		add(s)
	}
	switch v := params["patterns"].(type) {
	case []any:
		for _, it := range v {
			if s, ok := it.(string); ok {
				add(s)
			}
		}
	case []string:
		for _, s := range v {
			add(s)
		}
	}
	return out
}

// windowsCmdUnsafe 为 cmd.exe 下无法安全放进双引号参数的字符。
const windowsCmdUnsafe = "\"&|<>^%!\r\n"

func windowsQuote(field, s string) (string, error) {
	if strings.ContainsAny(s, windowsCmdUnsafe) {
		return "", fmt.Errorf("%s contains characters not allowed for cmd.exe ( \" & | < > ^ %% ! or newline)", field)
	}
	return `"` + s + `"`, nil
}

// windowsLineCountCmd 统计文件行数（tail 的第一步）。find 直接读文件，
// 不经 type 管道：大日志上管道版会被实例端判超时。输出形如 "---------- PATH: 200225"。
func (o *remoteOp) windowsLineCountCmd() (string, error) {
	p, err := windowsQuote("path", o.Path)
	if err != nil {
		return "", err
	}
	return `find /c /v "" ` + p, nil
}

// windowsCmd 返回单步即可完成的 cmd.exe 命令；tail 需先计数，传入 totalLines（<0 表示未知）。
func (o *remoteOp) windowsCmd(totalLines int) (string, error) {
	p, err := windowsQuote("path", o.Path)
	if err != nil {
		return "", err
	}
	switch o.Op {
	case RemoteOpListRecent:
		cmd := `dir /O-D /T:W`
		if !o.IncludeDirs {
			cmd += ` /A-D`
		}
		if o.Recursive {
			cmd += ` /S`
		}
		return cmd + ` ` + p, nil
	case RemoteOpGrep:
		var b strings.Builder
		b.WriteString(`findstr /N /I /L`)
		for _, kw := range o.Patterns {
			q, err := windowsQuote("pattern", kw)
			if err != nil {
				return "", err
			}
			b.WriteString(` /C:` + q)
		}
		b.WriteString(` ` + p)
		return b.String(), nil
	case RemoteOpFind:
		root := strings.TrimRight(o.Path, `\/`)
		var b strings.Builder
		b.WriteString(`dir /S /B /A-D`)
		for _, name := range o.Patterns {
			q, err := windowsQuote("pattern", root+`\`+name)
			if err != nil {
				return "", err
			}
			b.WriteString(` ` + q)
		}
		return b.String(), nil
	case RemoteOpTail:
		if totalLines >= 0 && totalLines > o.Lines {
			return fmt.Sprintf(`more +%d %s`, totalLines-o.Lines, p), nil
		}
		return `type ` + p, nil
	}
	return "", fmt.Errorf("unknown op %q", o.Op)
}

func posixQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// posixCmd 返回 POSIX shell 命令；不使用管道，便于通过 ssh_exec 的前缀白名单。
func (o *remoteOp) posixCmd() (string, error) {
	if strings.ContainsAny(o.Path, "\r\n") {
		return "", fmt.Errorf("path/pattern must not contain newlines")
	}
	for _, kw := range o.Patterns {
		if strings.ContainsAny(kw, "\r\n") {
			return "", fmt.Errorf("path/pattern must not contain newlines")
		}
	}
	switch o.Op {
	case RemoteOpListRecent:
		if o.Recursive {
			return "ls -ltR " + posixQuote(o.Path), nil
		}
		return "ls -lt " + posixQuote(o.Path), nil
	case RemoteOpTail:
		return "tail -n " + strconv.Itoa(o.Lines) + " " + posixQuote(o.Path), nil
	case RemoteOpGrep:
		var b strings.Builder
		b.WriteString("grep -n -i -F")
		for _, kw := range o.Patterns {
			b.WriteString(" -e " + posixQuote(kw))
		}
		b.WriteString(" " + posixQuote(o.Path))
		return b.String(), nil
	case RemoteOpFind:
		var b strings.Builder
		b.WriteString("find " + posixQuote(o.Path) + " -type f \\(")
		for i, name := range o.Patterns {
			if i > 0 {
				b.WriteString(" -o")
			}
			b.WriteString(" -name " + posixQuote(name))
		}
		b.WriteString(" \\)")
		return b.String(), nil
	}
	return "", fmt.Errorf("unknown op %q", o.Op)
}

// trimOutput 按 op 语义截取输出：列目录保留最新的前 N 条，find 保留前 N 个路径，grep/tail 保留最后 N 行。
// 返回截取后的文本与被省略的行数。
func (o *remoteOp) trimOutput(out string) (string, int) {
	if strings.TrimSpace(out) == "" {
		return out, 0
	}
	lines := strings.Split(strings.TrimRight(out, "\r\n"), "\n")
	if o.Op == RemoteOpGrep {
		lines = dropFindstrDiagnostics(lines)
	}
	switch o.Op {
	case RemoteOpListRecent:
		// dir / ls -l 输出含若干表头行，留出余量。
		keep := o.Lines + 8
		if len(lines) > keep {
			return strings.Join(lines[:keep], "\n"), len(lines) - keep
		}
	case RemoteOpFind:
		if len(lines) > o.Lines {
			return strings.Join(lines[:o.Lines], "\n"), len(lines) - o.Lines
		}
	case RemoteOpGrep, RemoteOpTail:
		if len(lines) > o.Lines {
			return strings.Join(lines[len(lines)-o.Lines:], "\n"), len(lines) - o.Lines
		}
	}
	return strings.Join(lines, "\n"), 0
}

// dropFindstrDiagnostics 去掉 findstr 混入输出的自身报错（如 "FINDSTR: 行 N 太长"），它们不是文件内容。
func dropFindstrDiagnostics(lines []string) []string {
	out := lines[:0:0]
	for _, l := range lines {
		if strings.HasPrefix(strings.TrimSpace(l), "FINDSTR:") {
			continue
		}
		out = append(out, l)
	}
	return out
}

func parseLineCount(stdout string) (int, bool) {
	s := strings.TrimSpace(stdout)
	if i := strings.LastIndexAny(s, " :\n"); i >= 0 {
		s = strings.TrimSpace(s[i+1:])
	}
	n, err := strconv.Atoi(s)
	if err != nil || n < 0 {
		return 0, false
	}
	return n, true
}

// remoteOpRequested 判断本次调用是否走 op（且未给原始命令）。
func remoteOpRequested(params map[string]any, cmdKeys ...string) bool {
	if op, _ := params["op"].(string); strings.TrimSpace(op) == "" {
		return false
	}
	for _, k := range cmdKeys {
		if s, _ := params[k].(string); strings.TrimSpace(s) != "" {
			return false
		}
	}
	return true
}
