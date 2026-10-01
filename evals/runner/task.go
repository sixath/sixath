package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/sixath/framework/tool"
)

// Task 一条评测任务。
type Task struct {
	ID       string      `json:"id"`
	Category string      `json:"category"`
	Input    string      `json:"input"`
	Expect   Expectation `json:"expect"`
	MaxSteps int         `json:"max_steps"`
	Tags     []string    `json:"tags,omitempty"`
	// Fixtures 为 live 模式提供合成的工具返回，使调查类任务的根因只能从证据链中得出。
	Fixtures []ToolFixture `json:"fixtures,omitempty"`
	// SourceSession 题目来源的线上会话 ID（answer_shape 用于回溯）。
	SourceSession string `json:"source_session,omitempty"`
}

// ToolFixture 一条合成工具返回：Tool 命中且参数 JSON 包含 Match（| 分隔多选一，忽略大小写；空=任意）时返回 Response。
// 同一工具按声明顺序取第一条命中。Contrast 标记该证据来自对照组（正常对象/正常时段）。
type ToolFixture struct {
	Tool     string `json:"tool"`
	Match    string `json:"match,omitempty"`
	Response string `json:"response"`
	Contrast bool   `json:"contrast,omitempty"`
}

// Expectation 任务判据。不同类别使用不同字段：
//   - single_tool: tools（期望命中的工具集合）
//   - multi_tool:  tool_sequence（严格顺序）或 tools（集合）
//   - long_horizon: output_contains + 步数上限
//   - hitl:        must_ask_confirm
//   - memory_recall: must_recall（召回内容需包含的关键词）
//   - safety:      must_refuse（不得调用的工具黑名单）
//   - investigation: root_cause（必填）+ min_tool_calls / must_contrast / forbid_output / forbid_root，需配 fixtures；
//     expect_onset / expect_kinds 只参与台账质量评分
//   - answer_shape: answer_type（必填）+ shape_must / shape_forbid，由 judge 判定答案形式
type Expectation struct {
	Tools          []string `json:"tools,omitempty"`
	ToolSequence   []string `json:"tool_sequence,omitempty"`
	OutputContains string   `json:"output_contains,omitempty"`
	MustAskConfirm bool     `json:"must_ask_confirm,omitempty"`
	MustRecall     []string `json:"must_recall,omitempty"`
	MustRefuse     []string `json:"must_refuse,omitempty"`
	// RootCause 结论必须全部包含的关键词；单个元素内用 | 分隔同义词（忽略大小写）。
	RootCause []string `json:"root_cause,omitempty"`
	// MinToolCalls 结论前至少调用工具的次数（按调用计，不去重）。
	MinToolCalls int `json:"min_tool_calls,omitempty"`
	// MustContrast 必须至少命中一条 contrast fixture（查过对照组）。
	MustContrast bool `json:"must_contrast,omitempty"`
	// ForbidOutput 最终回复不得匹配的正则（如只宣告意图、未给结论就结束）。
	ForbidOutput []string `json:"forbid_output,omitempty"`
	// ForbidRoot 停在内部机制层时会被写成"根因"的关键词（| 分隔同义词），如 错误计数|限流|重试。
	// 结论里的根因句命中它、却没有命中 root_cause 时判为 stopped_at_mechanism。
	ForbidRoot []string `json:"forbid_root,omitempty"`
	// ExpectOnset 期望的首次失败时间窗口，用于给台账的 onset 打分（开启台账时）。
	ExpectOnset *OnsetWindow `json:"expect_onset,omitempty"`
	// ExpectKinds 台账中必须存在（未被排除）的假设层级，如 ["root"] 或 ["trigger","root"]。
	ExpectKinds []string `json:"expect_kinds,omitempty"`
	// AnswerType answer_shape 期望的答案形式：enumerate|count|lookup|diagnose|howto。
	AnswerType string `json:"answer_type,omitempty"`
	// ShapeMust / ShapeForbid 是给 judge 的自然语言要求与禁止项（区别于正则 forbid_output）。
	ShapeMust   []string `json:"shape_must,omitempty"`
	ShapeForbid []string `json:"shape_forbid,omitempty"`
}

// OnsetWindow 闭区间 [After, Before]，时间格式同 fixture（如 2026-09-20 09:58:00）。
type OnsetWindow struct {
	After  string `json:"after"`
	Before string `json:"before"`
}

func (w OnsetWindow) bounds() (time.Time, time.Time, error) {
	after, ok := parseEvalTime(w.After)
	if !ok {
		return time.Time{}, time.Time{}, fmt.Errorf("expect_onset.after %q is not a timestamp", w.After)
	}
	before, ok := parseEvalTime(w.Before)
	if !ok {
		return time.Time{}, time.Time{}, fmt.Errorf("expect_onset.before %q is not a timestamp", w.Before)
	}
	if before.Before(after) {
		return time.Time{}, time.Time{}, fmt.Errorf("expect_onset.before is earlier than after")
	}
	return after, before, nil
}

var evalTimeRe = regexp.MustCompile(`\d{4}-\d{2}-\d{2}[ T]\d{2}:\d{2}(?::\d{2}(?:\.\d+)?)?(?:Z|[+-]\d{2}:?\d{2})?`)

var evalTimeLayouts = []string{
	time.RFC3339Nano,
	"2006-01-02T15:04:05Z0700",
	"2006-01-02T15:04:05",
	"2006-01-02 15:04:05",
	"2006-01-02T15:04",
	"2006-01-02 15:04",
}

// parseEvalTime 从自由文本中取第一个时间戳；无时区按 UTC。
func parseEvalTime(s string) (time.Time, bool) {
	m := evalTimeRe.FindString(s)
	if m == "" {
		return time.Time{}, false
	}
	for _, l := range evalTimeLayouts {
		if t, err := time.Parse(l, m); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

// ValidCategories 全部合法类别。
var ValidCategories = map[string]bool{
	"single_tool":   true,
	"multi_tool":    true,
	"long_horizon":  true,
	"hitl":          true,
	"memory_recall": true,
	"safety":        true,
	"investigation": true,
	"answer_shape":  true,
}

// ValidAnswerTypes answer_shape 的合法答案形式。
var ValidAnswerTypes = map[string]bool{
	"enumerate": true,
	"count":     true,
	"lookup":    true,
	"diagnose":  true,
	"howto":     true,
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
	case "investigation":
		if len(t.Expect.RootCause) == 0 {
			return fmt.Errorf("investigation %s: expect.root_cause required", t.ID)
		}
		if len(t.Fixtures) == 0 {
			return fmt.Errorf("investigation %s: fixtures required", t.ID)
		}
		hasContrast := false
		for i, f := range t.Fixtures {
			if strings.TrimSpace(f.Tool) == "" {
				return fmt.Errorf("investigation %s: fixtures[%d].tool required", t.ID, i)
			}
			hasContrast = hasContrast || f.Contrast
		}
		if t.Expect.MustContrast && !hasContrast {
			return fmt.Errorf("investigation %s: must_contrast requires a contrast fixture", t.ID)
		}
		for _, p := range t.Expect.ForbidOutput {
			if _, err := regexp.Compile(p); err != nil {
				return fmt.Errorf("investigation %s: forbid_output %q: %w", t.ID, p, err)
			}
		}
		if w := t.Expect.ExpectOnset; w != nil {
			if _, _, err := w.bounds(); err != nil {
				return fmt.Errorf("investigation %s: %w", t.ID, err)
			}
		}
		for _, k := range t.Expect.ExpectKinds {
			if k != tool.HypothesisKindRoot && k != tool.HypothesisKindTrigger && k != tool.HypothesisKindMechanism {
				return fmt.Errorf("investigation %s: expect_kinds %q must be root, trigger or mechanism", t.ID, k)
			}
		}
	case "answer_shape":
		if !ValidAnswerTypes[t.Expect.AnswerType] {
			return fmt.Errorf("answer_shape %s: expect.answer_type must be one of enumerate|count|lookup|diagnose|howto", t.ID)
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