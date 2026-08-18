package toolskill

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/sixath/framework/events"
	"github.com/sixath/framework/growth"
	"github.com/sixath/framework/skills"
	"github.com/sixath/framework/tool"
)

// skillBodyLoadOnce de-duplicates full SKILL.md loads within one Registry (one ReAct turn).
type skillBodyLoadOnce struct {
	mu   sync.Mutex
	seen map[string]struct{}
}

func newSkillBodyLoadOnce() *skillBodyLoadOnce {
	return &skillBodyLoadOnce{seen: make(map[string]struct{})}
}

func skillLoadKey(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}

func (s *skillBodyLoadOnce) claim(name string) (already bool) {
	key := skillLoadKey(name)
	if s == nil || key == "" {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.seen[key]; ok {
		return true
	}
	s.seen[key] = struct{}{}
	return false
}

func (s *skillBodyLoadOnce) unclaim(name string) {
	key := skillLoadKey(name)
	if s == nil || key == "" {
		return
	}
	s.mu.Lock()
	delete(s.seen, key)
	s.mu.Unlock()
}

func skillAlreadyLoadedNotice(name string) string {
	return fmt.Sprintf("Skill %q is already loaded in this turn. Do not call load_skill or skill_view again for this name. Continue the workflow using the SKILL.md already in the transcript; call other tools next.", name)
}

// McpServerEntry 描述一个可用的 MCP 服务配置（与 config.MCPServerEntry 对齐），用于在 load_skill 时按 Skill 声明按需注册。
type McpServerEntry struct {
	Transport string
	Endpoint  string
	Id        string
	Backend   string
	Command   string
	Args      []string
	Env       map[string]string
}

// RegisterLoadSkillTool 向 Registry 注册用于加载 Skill 正文的工具。当某 Skill 被加载且其 frontmatter 声明了 mcp_servers 时，
// 会按 mcpServers 配置将该 Skill 声明的 MCP 能力注册到当前 Registry，使后续步骤可调用对应 MCP 工具。
// mcpServers 可为 nil/空，此时仅加载正文，不注册 MCP。
func RegisterLoadSkillTool(reg *tool.Registry, idx *skills.Index, mcpServers []McpServerEntry) error {
	if reg == nil {
		return errors.New("load_skill: registry is nil")
	}
	if idx == nil {
		return errors.New("load_skill: index is nil")
	}

	once := newSkillBodyLoadOnce()
	err := reg.Register(tool.Tool{
		Name: "load_skill",
		Description: "Load full SKILL.md content by skill name. Call at most once per skill per turn; " +
			"repeats return a short already-loaded notice instead of the full body.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"name": map[string]any{
					"type":        "string",
					"description": "Skill name (kebab-case).",
				},
			},
			"required": []string{"name"},
		},
		Execute: func(ctx context.Context, params map[string]any) (any, error) {
			raw, ok := params["name"]
			if !ok {
				return nil, errors.New("load_skill: name is required")
			}
			name, _ := raw.(string)
			name = strings.TrimSpace(name)
			if name == "" {
				return nil, errors.New("load_skill: name is empty")
			}
			if once.claim(name) {
				return skillAlreadyLoadedNotice(name), nil
			}
			body, err := idx.LoadSkillBody(name)
			if err != nil {
				once.unclaim(name)
				return nil, err
			}
			// 明确使用该 Skill 时，将该 Skill 声明的 MCP 能力注册到当前上下文（仅注册配置中存在的服务）。
			registerSkillMcpFromMeta(reg, idx, name, mcpServers)
			return body, nil
		},
	})
	if err != nil {
		return err
	}
	return RegisterReadSkillFileTool(reg, idx)
}

// RegisterReadSkillFileTool 向 Registry 注册用于读取 Skill 捆绑文档的工具。
// 模型可据此按需加载 Skill 目录下的 docs/、assets/、scripts/ 等文件内容（仅读取，不执行）。
func RegisterReadSkillFileTool(reg *tool.Registry, idx *skills.Index) error {
	if reg == nil {
		return errors.New("read_skill_file: registry is nil")
	}
	if idx == nil {
		return errors.New("read_skill_file: index is nil")
	}
	return reg.Register(tool.Tool{
		Name:        "read_skill_file",
		Description: "Read a file bundled with a Skill by skill name and relative path (e.g. docs/advanced.md, assets/template.json). Path must be under the skill directory; only reading is supported, no script execution.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"name": map[string]any{
					"type":        "string",
					"description": "Skill name (kebab-case).",
				},
				"path": map[string]any{
					"type":        "string",
					"description": "Relative path under the skill directory, e.g. docs/advanced.md, assets/example.json.",
				},
			},
			"required": []string{"name", "path"},
		},
		Execute: func(ctx context.Context, params map[string]any) (any, error) {
			name, _ := params["name"].(string)
			if name == "" {
				return nil, errors.New("read_skill_file: name is required")
			}
			path, _ := params["path"].(string)
			if path == "" {
				return nil, errors.New("read_skill_file: path is required")
			}
			body, err := idx.LoadSkillFile(name, path)
			rid, _ := ctx.Value(tool.ContextKeyRequestID).(string)
			invokedPayload := map[string]any{
				"killToolName": "read_skill_file",
				"path":         path,
				"file_name":    name,
			}
			events.DefaultBus().Publish(ctx, events.Event{
				Kind:      events.ToolExecuted,
				RequestID: rid,
				Payload:   invokedPayload,
			})
			if err != nil {
				return nil, err
			}
			return body, nil
		},
	})
}

// ExecuteSkillScriptOptions 脚本执行可选配置（任务 15.1）；nil 或零值使用默认（仅 .sh，30 秒超时）。
type ExecuteSkillScriptOptions struct {
	// AllowedExtensions 允许的扩展名白名单，如 [".sh"]；空时默认 [".sh"]。
	AllowedExtensions []string
	// TimeoutSeconds 单次执行超时（秒）；<=0 时默认 30；建议上限 300。
	TimeoutSeconds int
}

func defaultScriptAllowedExtensions(opts *ExecuteSkillScriptOptions) []string {
	if opts != nil && len(opts.AllowedExtensions) > 0 {
		return opts.AllowedExtensions
	}
	return []string{".sh", ".js", ".py", ".ps1"}
}

// scriptCommand 根据脚本扩展名返回执行命令：解释器名与参数列表。
// .sh -> sh [path], .py -> python [path], .js -> node [path], .ps1 -> powershell -ExecutionPolicy Bypass -File [path]
func scriptCommand(ext, fullPath string) (name string, args []string) {
	switch ext {
	case ".py":
		return "python", []string{fullPath}
	case ".js":
		return "node", []string{fullPath}
	case ".ps1":
		return "powershell", []string{"-ExecutionPolicy", "Bypass", "-File", fullPath}
	default:
		return "sh", []string{fullPath}
	}
}

func defaultScriptTimeout(opts *ExecuteSkillScriptOptions) time.Duration {
	sec := 30
	if opts != nil && opts.TimeoutSeconds > 0 {
		sec = opts.TimeoutSeconds
		if sec > 300 {
			sec = 300
		}
	}
	return time.Duration(sec) * time.Second
}

func scriptArgsFromParams(params map[string]any) ([]string, error) {
	raw, ok := params["args"]
	if !ok || raw == nil {
		return nil, nil
	}
	items, ok := raw.([]any)
	if !ok {
		return nil, errors.New("execute_skill_script: args must be an array of strings")
	}
	args := make([]string, 0, len(items))
	for _, item := range items {
		s, ok := item.(string)
		if !ok {
			return nil, errors.New("execute_skill_script: args must be an array of strings")
		}
		args = append(args, s)
	}
	return args, nil
}

// RegisterExecuteSkillScriptTool 向 Registry 注册执行 Skill 目录下脚本的工具（可选，受 allowScriptExecution 控制）。
// 当 allowScriptExecution 为 false 时，工具执行直接返回错误；为 true 时在 scripts/ 下按 AllowedExtensions 与 TimeoutSeconds 执行（任务 15.2～15.4）。
// opts 可为 nil，此时默认仅 .sh、30 秒超时。
func RegisterExecuteSkillScriptTool(reg *tool.Registry, idx *skills.Index, allowScriptExecution bool, opts *ExecuteSkillScriptOptions) error {
	if reg == nil {
		return errors.New("execute_skill_script: registry is nil")
	}
	if idx == nil {
		return errors.New("execute_skill_script: index is nil")
	}
	allow := allowScriptExecution
	allowedExt := defaultScriptAllowedExtensions(opts)
	timeout := defaultScriptTimeout(opts)
	return reg.Register(tool.Tool{
		Name:        "execute_skill_script",
		Description: "Execute a script bundled with a Skill. Supports multiple runtimes: shell (.sh), Python (.py), Node.js (.js), PowerShell (.ps1), etc.; allowed extensions are configured by the system (e.g. script_allowed_extensions). Path must be under the skill directory and must start with scripts/. Only available when script execution is enabled (skills.allow_script_execution).",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"name": map[string]any{
					"type":        "string",
					"description": "Skill name (kebab-case).",
				},
				"path": map[string]any{
					"type":        "string",
					"description": "Relative path to script under the skill directory, e.g. scripts/run.sh, scripts/main.py, scripts/index.js, scripts/run.ps1. Must be under scripts/.",
				},
				"input": map[string]any{
					"type":        "string",
					"description": "Optional input to pass to the script via stdin (e.g. JSON string for Node/Python scripts that read from stdin).",
				},
				"args": map[string]any{
					"type": "array",
					"items": map[string]any{
						"type": "string",
					},
					"description": "Optional command-line arguments passed to the script after the script path, e.g. [\"-FlowId\", \"4_v0cag1d3guo8\"].",
				},
			},
			"required": []string{"name", "path"},
		},
		Execute: func(ctx context.Context, params map[string]any) (any, error) {
			if !allow {
				return nil, errors.New("execute_skill_script: script execution is disabled (set skills.allow_script_execution to enable)")
			}
			name, _ := params["name"].(string)
			if name == "" {
				return nil, errors.New("execute_skill_script: name is required")
			}
			relPath, _ := params["path"].(string)
			if relPath == "" {
				return nil, errors.New("execute_skill_script: path is required")
			}
			meta, ok := idx.GetByName(name)
			if !ok {
				return nil, fmt.Errorf("execute_skill_script: skill not found: %s", name)
			}
			if meta.Path == "" {
				return nil, fmt.Errorf("execute_skill_script: skill path is empty for %s", name)
			}
			skillRoot := filepath.Dir(meta.Path)
			cleaned := filepath.Clean(relPath)
			if cleaned == ".." || strings.HasPrefix(cleaned, "..") {
				return nil, errors.New("execute_skill_script: path must be under skill directory")
			}
			fullPath := filepath.Join(skillRoot, cleaned)
			absRoot, err := filepath.Abs(skillRoot)
			if err != nil {
				return nil, err
			}
			absFull, err := filepath.Abs(fullPath)
			if err != nil {
				return nil, err
			}
			if absFull != absRoot && !strings.HasPrefix(absFull, absRoot+string(filepath.Separator)) {
				return nil, errors.New("execute_skill_script: path must be under skill directory")
			}
			if !strings.HasPrefix(cleaned, "scripts") {
				return nil, errors.New("execute_skill_script: only scripts under scripts/ are allowed")
			}
			ext := filepath.Ext(cleaned)
			allowed := false
			for _, e := range allowedExt {
				if e == ext {
					allowed = true
					break
				}
			}
			if !allowed {
				return nil, fmt.Errorf("execute_skill_script: extension %q not in allowed list %v", ext, allowedExt)
			}
			if _, err := os.Stat(fullPath); err != nil {
				return nil, fmt.Errorf("execute_skill_script: script file: %w", err)
			}
			cmdName, cmdArgs := scriptCommand(ext, fullPath)
			extraArgs, err := scriptArgsFromParams(params)
			if err != nil {
				return nil, err
			}
			cmdArgs = append(cmdArgs, extraArgs...)
			runCtx, cancel := context.WithTimeout(ctx, timeout)
			defer cancel()
			cmd := exec.CommandContext(runCtx, cmdName, cmdArgs...)
			cmd.Dir = skillRoot
			if inputStr, ok := params["input"].(string); ok && inputStr != "" {
				cmd.Stdin = strings.NewReader(inputStr)
			}
			out, err := cmd.CombinedOutput()

			if err != nil {
				return string(out) + "\n" + err.Error(), nil
			}
			rid, _ := ctx.Value(tool.ContextKeyRequestID).(string)
			invokedPayload := map[string]any{
				"killToolName": "execute_skill_script",
				"path":         fullPath,
				"file_name":    name,
			}
			events.DefaultBus().Publish(ctx, events.Event{
				Kind:      events.ToolExecuted,
				RequestID: rid,
				Payload:   invokedPayload,
			})
			return string(out), nil
		},
	})
}

// RegisterSkillsListViewTools registers Hermes-aligned skills_list and skill_view tools.
func RegisterSkillsListViewTools(reg *tool.Registry, idx *skills.Index, mcpServers []McpServerEntry) error {
	if reg == nil {
		return errors.New("skills_list: registry is nil")
	}
	if idx == nil {
		return errors.New("skills_list: index is nil")
	}
	if err := registerSkillsListTool(reg, idx); err != nil {
		return err
	}
	return registerSkillViewTool(reg, idx, mcpServers)
}

func registerSkillsListTool(reg *tool.Registry, idx *skills.Index) error {
	return reg.Register(tool.Tool{
		Name:        "skills_list",
		Description: "List available skills (name + description). Use skill_view(name) to load full content.",
		Toolset:     tool.ToolsetSkills,
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"category": map[string]any{
					"type":        "string",
					"description": "Optional tag/category filter (matches skill tags).",
				},
			},
		},
		Execute: func(ctx context.Context, params map[string]any) (any, error) {
			category, _ := params["category"].(string)
			metas := idx.All()
			if category != "" {
				metas = filterSkillsByCategory(metas, category)
			}
			items := make([]map[string]any, 0, len(metas))
			for _, m := range metas {
				item := map[string]any{
					"name":        m.Name,
					"description": m.Description,
				}
				if len(m.Tags) > 0 {
					item["tags"] = m.Tags
				}
				items = append(items, item)
			}
			return map[string]any{"skills": items}, nil
		},
	})
}

func registerSkillViewTool(reg *tool.Registry, idx *skills.Index, mcpServers []McpServerEntry) error {
	once := newSkillBodyLoadOnce()
	return reg.Register(tool.Tool{
		Name: "skill_view",
		Description: "Load a skill's full SKILL.md content or a linked file under references/, templates/, scripts/, docs/, or assets/. " +
			"First call with name only returns content plus linked_files; call again with file_path to read a linked file. " +
			"Do not reload the same skill body in one turn.",
		Toolset: tool.ToolsetSkills,
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"name": map[string]any{
					"type":        "string",
					"description": "Skill name (kebab-case).",
				},
				"file_path": map[string]any{
					"type":        "string",
					"description": "Optional relative path to a linked file (e.g. docs/guide.md, scripts/run.sh).",
				},
			},
			"required": []string{"name"},
		},
		Execute: func(ctx context.Context, params map[string]any) (any, error) {
			name, _ := params["name"].(string)
			if name == "" {
				return nil, errors.New("skill_view: name is required")
			}
			filePath, _ := params["file_path"].(string)
			if filePath != "" {
				body, err := idx.LoadSkillFile(name, filePath)
				if err != nil {
					return nil, err
				}
				if err := growth.ScanUserContent(body); err != nil {
					return map[string]any{"error": err.Error(), "name": name, "file_path": filePath}, nil
				}
				return map[string]any{
					"name":      name,
					"file_path": filePath,
					"content":   body,
				}, nil
			}

			if once.claim(name) {
				return map[string]any{
					"name":           name,
					"already_loaded": true,
					"content":        skillAlreadyLoadedNotice(name),
				}, nil
			}
			body, err := idx.LoadSkillBody(name)
			if err != nil {
				once.unclaim(name)
				return nil, err
			}
			if err := growth.ScanUserContent(body); err != nil {
				return map[string]any{"error": err.Error(), "name": name}, nil
			}
			registerSkillMcpFromMeta(reg, idx, name, mcpServers)

			meta, ok := idx.GetByName(name)
			if !ok || meta.Path == "" {
				return map[string]any{"name": name, "content": body}, nil
			}
			skillRoot := filepath.Dir(meta.Path)
			linked, lerr := listSkillLinkedFiles(skillRoot)
			if lerr != nil {
				once.unclaim(name)
				return nil, lerr
			}
			return map[string]any{
				"name":         name,
				"content":      body,
				"linked_files": linked,
			}, nil
		},
	})
}

func filterSkillsByCategory(metas []skills.SkillMeta, category string) []skills.SkillMeta {
	category = strings.TrimSpace(strings.ToLower(category))
	if category == "" {
		return metas
	}
	var out []skills.SkillMeta
	for _, m := range metas {
		for _, tag := range m.Tags {
			if strings.ToLower(strings.TrimSpace(tag)) == category {
				out = append(out, m)
				break
			}
		}
	}
	return out
}

func listSkillLinkedFiles(skillRoot string) (map[string][]string, error) {
	subdirs := []string{"references", "templates", "scripts", "docs", "assets"}
	out := make(map[string][]string)
	for _, sub := range subdirs {
		dir := filepath.Join(skillRoot, sub)
		entries, err := os.ReadDir(dir)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, err
		}
		var files []string
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			files = append(files, filepath.ToSlash(filepath.Join(sub, e.Name())))
		}
		if len(files) > 0 {
			out[sub] = files
		}
	}
	return out, nil
}

func registerSkillMcpFromMeta(reg *tool.Registry, idx *skills.Index, name string, mcpServers []McpServerEntry) {
	meta, hasMeta := idx.GetByName(name)
	if !hasMeta || len(meta.MCPServers) == 0 || len(mcpServers) == 0 {
		return
	}
	byID := make(map[string]McpServerEntry)
	for _, e := range mcpServers {
		if e.Id != "" {
			byID[e.Id] = e
		}
	}
	for _, id := range meta.MCPServers {
		if e, ok := byID[id]; ok {
			tool.RegisterMcpTool(reg, &tool.McpConfig{
				Transport: e.Transport,
				Endpoint:  e.Endpoint,
				Id:        e.Id,
				Backend:   e.Backend,
				Command:   e.Command,
				Args:      e.Args,
				Env:       e.Env,
			})
		}
	}
}
