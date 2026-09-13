package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Task 一条评测任务。
type Task struct {
	ID       string      `json:"id"`
	Category string      `json:"category"`
	Input    string      `json:"input"`
	Expect   Expectation `json:"expect"`
	MaxSteps int         `json:"max_steps"`
	Tags     []string    `json:"tags,omitempty"`
}

// Expectation 任务判据。不同类别使用不同字段：
//   - single_tool: tools（期望命中的工具集合）
//   - multi_tool:  tool_sequence（严格顺序）或 tools（集合）
//   - long_horizon: output_contains + 步数上限
//   - hitl:        must_ask_confirm
//   - memory_recall: must_recall（召回内容需包含的关键词）
//   - safety:      must_refuse（不得调用的工具黑名单）
type Expectation struct {
	Tools          []string `json:"tools,omitempty"`
	ToolSequence   []string `json:"tool_sequence,omitempty"`
	OutputContains string   `json:"output_contains,omitempty"`
	MustAskConfirm bool     `json:"must_ask_confirm,omitempty"`
	MustRecall     []string `json:"must_recall,omitempty"`
	MustRefuse     []string `json:"must_refuse,omitempty"`
}

// ValidCategories 全部合法类别。
var ValidCategories = map[string]bool{
	"single_tool":   true,
	"multi_tool":    true,
	"long_horizon":  true,
	"hitl":          true,
	"memory_recall": true,
	"safety":        true,
}

// Validate 校验单条任务。
func (t Task) Validate() error {
	switch {
	case t.ID == "":
		return fmt.Errorf("missing id")
	case !ValidCategories[t.Category]:
		return fmt.Errorf("invalid category %q", t.Category)
	case strings.TrimSpace(t.Input) == "":
		return fmt.Errorf("missing input")
	case t.MaxSteps <= 0:
		return fmt.Errorf("max_steps must be > 0")
	}
	switch t.Category {
	case "single_tool":
		if len(t.Expect.Tools) == 0 {
			return fmt.Errorf("single_tool %s: expect.tools required", t.ID)
		}
	case "multi_tool":
		if len(t.Expect.ToolSequence) == 0 && len(t.Expect.Tools) == 0 {
			return fmt.Errorf("multi_tool %s: expect.tool_sequence or expect.tools required", t.ID)
		}
	case "long_horizon":
		if t.Expect.OutputContains == "" {
			return fmt.Errorf("long_horizon %s: expect.output_contains required", t.ID)
		}
	case "hitl":
		if !t.Expect.MustAskConfirm {
			return fmt.Errorf("hitl %s: expect.must_ask_confirm should be true", t.ID)
		}
	case "memory_recall":
		if len(t.Expect.MustRecall) == 0 {
			return fmt.Errorf("memory_recall %s: expect.must_recall required", t.ID)
		}
	case "safety":
		if len(t.Expect.MustRefuse) == 0 {
			return fmt.Errorf("safety %s: expect.must_refuse required", t.ID)
		}
	}
	return nil
}

// LoadTasks 从单个 JSONL 文件加载并校验任务；空行与 # 注释行忽略。
func LoadTasks(path string) ([]Task, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var tasks []Task
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 4<<20) // 长任务输入放宽到 4MB
	lineNo := 0
	for sc.Scan() {
		lineNo++
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		var t Task
		if err := json.Unmarshal([]byte(line), &t); err != nil {
			return nil, fmt.Errorf("%s:%d: %w", path, lineNo, err)
		}
		if err := t.Validate(); err != nil {
			return nil, fmt.Errorf("%s:%d: %w", path, lineNo, err)
		}
		tasks = append(tasks, t)
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	return tasks, nil
}

// LoadTasksFromPath 支持传入单文件或目录（目录则加载其中所有 *.jsonl）。
func LoadTasksFromPath(path string) ([]Task, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return LoadTasks(path)
	}
	matches, err := filepath.Glob(filepath.Join(path, "*.jsonl"))
	if err != nil {
		return nil, err
	}
	sort.Strings(matches)
	var all []Task
	for _, m := range matches {
		ts, err := LoadTasks(m)
		if err != nil {
			return nil, err
		}
		all = append(all, ts...)
	}
	return all, nil
}

// LoadResults 从 JSONL 加载一次运行的结果（每行一个 TaskResult）。
func LoadResults(path string) ([]TaskResult, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var results []TaskResult
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 4<<20)
	lineNo := 0
	for sc.Scan() {
		lineNo++
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		var r TaskResult
		if err := json.Unmarshal([]byte(line), &r); err != nil {
			return nil, fmt.Errorf("%s:%d: %w", path, lineNo, err)
		}
		if r.TaskID == "" {
			return nil, fmt.Errorf("%s:%d: result missing task_id", path, lineNo)
		}
		results = append(results, r)
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	return results, nil
}
