package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"
)

const (
	portalDefaultTimeout      = 10 * time.Minute
	portalDefaultPollInterval = 2 * time.Second
	portalDefaultGracePeriod  = 2 * time.Minute
	portalDefaultJudgeTimeout = 2 * time.Minute
	portalFailureFetchTimeout = 10 * time.Second
	// portalMaxPollErrors 轮询消息时容忍的连续瞬时错误次数。
	portalMaxPollErrors = 3
	// portalMaxSSELine 单行 SSE 上限；超长行（如携带完整工具结果的 tool_call）整行丢弃，done/error 帧很小不受影响。
	portalMaxSSELine = 4 << 20
	// portalMaxJSONBody 消息列表等 JSON 响应的读取上限。
	portalMaxJSONBody = 32 << 20
	// portalErrorPrefix 与 portal chatsse.WriteStream 失败时落库的正文前缀一致。
	portalErrorPrefix = "Error: "
)

// PortalClient 通过 Portal（或 Gateway）的对话接口驱动真实 agent。
// 直连 Portal 需以 SATH_CHAT_PUBLIC_INBOUND_ENABLED=true 启动，否则 POST 返回 403。
type PortalClient struct {
	BaseURL string
	Token   string
	OrgID   string
	HTTP    *http.Client
	// Timeout 单轮从发送到拿到终答的总时限；0 用默认值。应大于 agent 跑满 MaxSteps 的最坏耗时，
	// 否则正常但较慢的运行会被记为 timeout。
	Timeout time.Duration
	// PollInterval SSE 中断后轮询消息的间隔；0 用默认值。
	PollInterval time.Duration
	// GracePeriod 客户端超时后继续等待服务端落库的时长；0 用默认值。服务端以 context.WithoutCancel 运行且没有取消接口，
	// 不等的话同一题的下一次重复会与残留运行重叠。
	GracePeriod time.Duration
}

// portalTurn 一轮对话的结果。Failed 表示本轮以 SSE error 结束或落库消息是 "Error: ..."；
// HITL 表示出现了 confirm_required / input_required；TimedOut 表示客户端单轮超时。
type portalTurn struct {
	Answer   string
	Timeline []any
	Failed   bool
	Error    string
	HITL     bool
	TimedOut bool
}

type portalRet struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type sseOutcome struct {
	failed    bool
	errMsg    string
	completed bool
	hitl      bool
}

// RunTurn 新建会话、发送问题、等待结束并取回终答与执行时间线。
// 返回的 error 都是基础设施错误（传输、HTTP 非 2xx、ret.code≠0、done 后消息未落库、父 ctx 取消）。
func (c *PortalClient) RunTurn(ctx context.Context, agentID, title, content string) (portalTurn, error) {
	turnCtx, cancel := context.WithTimeout(ctx, c.timeout())
	defer cancel()

	sessionID, err := c.createSession(turnCtx, agentID, title)
	if err != nil {
		return portalTurn{}, err
	}
	out, err := c.stream(turnCtx, sessionID, content)
	if err != nil {
		if turnTimedOut(ctx, turnCtx) {
			return c.awaitAfterTimeout(ctx, sessionID)
		}
		return portalTurn{}, err
	}
	if out.failed {
		turn := portalTurn{Failed: true, Error: out.errMsg, HITL: out.hitl}
		// 读到 EOF 时单轮 ctx 可能已过期，取 timeline 改用父 ctx 派生的短时限。
		fctx, fcancel := context.WithTimeout(ctx, portalFailureFetchTimeout)
		if _, tl, found, err := c.lastAssistant(fctx, sessionID); err == nil && found {
			turn.Timeline = tl
		}
		fcancel()
		return turn, nil
	}
	turn, err := c.poll(turnCtx, sessionID, out.completed)
	if err != nil {
		if turnTimedOut(ctx, turnCtx) {
			return c.awaitAfterTimeout(ctx, sessionID)
		}
		return portalTurn{}, err
	}
	turn.HITL = out.hitl
	return turn, nil
}

func turnTimedOut(parent, turnCtx context.Context) bool {
	return parent.Err() == nil && errors.Is(turnCtx.Err(), context.DeadlineExceeded)
}

// awaitAfterTimeout 客户端超时后在 GracePeriod 内继续轮询，直到服务端残留运行落库或宽限期结束。
func (c *PortalClient) awaitAfterTimeout(parent context.Context, sessionID string) (portalTurn, error) {
	gctx, cancel := context.WithTimeout(parent, c.gracePeriod())
	defer cancel()
	turn := portalTurn{TimedOut: true, Error: fmt.Sprintf("portal: turn exceeded %s", c.timeout())}
	t, err := c.poll(gctx, sessionID, false)
	if err == nil {
		turn.Timeline = t.Timeline
	} else if parent.Err() != nil {
		return portalTurn{}, fmt.Errorf("portal: %w", parent.Err())
	}
	return turn, nil
}

// poll 轮询直到出现 assistant 消息；completed=true 时只查到空就判定未落库。
func (c *PortalClient) poll(ctx context.Context, sessionID string, completed bool) (portalTurn, error) {
	ticker := time.NewTicker(c.pollInterval())
	defer ticker.Stop()
	fails := 0
	for {
		answer, tl, found, err := c.lastAssistant(ctx, sessionID)
		switch {
		case err != nil:
			if ctx.Err() != nil {
				return portalTurn{}, err
			}
			fails++
			if fails > portalMaxPollErrors {
				return portalTurn{}, err
			}
		case found:
			return assistantTurn(answer, tl), nil
		case completed:
			return portalTurn{}, errors.New("portal: stream completed but no assistant message was persisted")
		default:
			fails = 0
		}
		select {
		case <-ctx.Done():
			return portalTurn{}, fmt.Errorf("portal: waiting for reply: %w", ctx.Err())
		case <-ticker.C:
		}
	}
}

func assistantTurn(answer string, tl []any) portalTurn {
	if strings.HasPrefix(answer, portalErrorPrefix) {
		return portalTurn{Failed: true, Error: strings.TrimSpace(strings.TrimPrefix(answer, portalErrorPrefix)), Timeline: tl}
	}
	return portalTurn{Answer: answer, Timeline: tl}
}

func (c *PortalClient) createSession(ctx context.Context, agentID, title string) (string, error) {
	var out struct {
		ID string `json:"id"`
	}
	path := "/api/v1/agents/" + url.PathEscape(agentID) + "/sessions"
	if err := c.doJSON(ctx, http.MethodPost, path, map[string]any{"title": title}, &out); err != nil {
		return "", fmt.Errorf("portal: create session: %w", err)
	}
	if out.ID == "" {
		return "", errors.New("portal: create session returned no id")
	}
	return out.ID, nil
}

// stream 发送消息并读 SSE。读到 done 立即返回 completed；读到 error 记下后继续读到 EOF，
// 因为服务端写完 error 帧后才落库 "Error: ..." 与 timeline。
// SSE 中途断开（没有终止事件）时 completed=false、err=nil，由调用方轮询兜底。
func (c *PortalClient) stream(ctx context.Context, sessionID, content string) (sseOutcome, error) {
	var out sseOutcome
	body, err := json.Marshal(map[string]any{"content": content})
	if err != nil {
		return out, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/api/v1/sessions/"+url.PathEscape(sessionID)+"/messages/stream", bytes.NewReader(body))
	if err != nil {
		return out, err
	}
	c.setHeaders(req)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	resp, err := c.httpClient().Do(req)
	if err != nil {
		return out, fmt.Errorf("portal: send message: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return out, fmt.Errorf("portal: send message: HTTP %d: %s", resp.StatusCode, truncateRunes(strings.TrimSpace(string(raw)), 300))
	}
	br := bufio.NewReaderSize(resp.Body, 64*1024)
	event := ""
	for {
		line, tooLong, rerr := readSSELine(br, portalMaxSSELine)
		if rerr != nil {
			break
		}
		if tooLong {
			continue
		}
		switch {
		case line == "":
			event = ""
		case strings.HasPrefix(line, "event:"):
			event = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
			if event == "confirm_required" || event == "input_required" {
				out.hitl = true
			}
		case strings.HasPrefix(line, "data:"):
			data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			switch event {
			case "done":
				if !out.failed {
					out.completed = true
					return out, nil
				}
			case "error":
				if out.failed {
					continue
				}
				var e struct {
					Error string `json:"error"`
				}
				_ = json.Unmarshal([]byte(data), &e)
				if e.Error == "" {
					e.Error = data
				}
				out.failed, out.errMsg, out.completed = true, e.Error, true
			}
		}
	}
	if out.failed {
		return out, nil
	}
	if ctx.Err() != nil {
		return out, fmt.Errorf("portal: stream: %w", ctx.Err())
	}
	return out, nil
}

// readSSELine 读一行（去掉 \r\n）；超过 max 的行读完丢弃并返回 tooLong=true，内存占用不超过 max。
func readSSELine(br *bufio.Reader, max int) (line string, tooLong bool, err error) {
	var buf []byte
	for {
		chunk, err := br.ReadSlice('\n')
		if !tooLong {
			if len(buf)+len(chunk) > max {
				tooLong, buf = true, nil
			} else {
				buf = append(buf, chunk...)
			}
		}
		if err == bufio.ErrBufferFull {
			continue
		}
		if err != nil {
			if err == io.EOF && len(buf) > 0 && !tooLong {
				return strings.TrimRight(string(buf), "\r\n"), false, nil
			}
			return "", tooLong, err
		}
		return strings.TrimRight(string(buf), "\r\n"), tooLong, nil
	}
}

// lastAssistant 取会话中最后一条 assistant 消息。评测每轮都新建会话（一问一答），所以它就是本轮的回复。
func (c *PortalClient) lastAssistant(ctx context.Context, sessionID string) (answer string, timeline []any, found bool, err error) {
	var out struct {
		Items []struct {
			Role     string         `json:"role"`
			Content  string         `json:"content"`
			Metadata map[string]any `json:"metadata"`
		} `json:"items"`
	}
	if err := c.doJSON(ctx, http.MethodGet, "/api/v1/sessions/"+url.PathEscape(sessionID)+"/messages", nil, &out); err != nil {
		return "", nil, false, fmt.Errorf("portal: list messages: %w", err)
	}
	for i := len(out.Items) - 1; i >= 0; i-- {
		it := out.Items[i]
		if it.Role != "assistant" {
			continue
		}
		tl, _ := it.Metadata["timeline"].([]any)
		return it.Content, tl, true, nil
	}
	return "", nil, false, nil
}

func (c *PortalClient) doJSON(ctx context.Context, method, path string, in, out any) error {
	var body io.Reader
	if in != nil {
		raw, err := json.Marshal(in)
		if err != nil {
			return err
		}
		body = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.BaseURL+path, body)
	if err != nil {
		return err
	}
	c.setHeaders(req)
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.httpClient().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, portalMaxJSONBody+1))
	if err != nil {
		return err
	}
	if len(raw) > portalMaxJSONBody {
		return fmt.Errorf("response body exceeds %d bytes", portalMaxJSONBody)
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, truncateRunes(strings.TrimSpace(string(raw)), 300))
	}
	var ret struct {
		Ret *portalRet `json:"ret"`
	}
	if json.Unmarshal(raw, &ret) == nil && ret.Ret != nil && ret.Ret.Code != 0 {
		return fmt.Errorf("ret.code=%d: %s", ret.Ret.Code, ret.Ret.Message)
	}
	return json.Unmarshal(raw, out)
}

func (c *PortalClient) setHeaders(req *http.Request) {
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
	if c.OrgID != "" {
		req.Header.Set("X-Org-Id", c.OrgID)
	}
}

func (c *PortalClient) httpClient() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return http.DefaultClient
}

func (c *PortalClient) timeout() time.Duration {
	if c.Timeout > 0 {
		return c.Timeout
	}
	return portalDefaultTimeout
}

func (c *PortalClient) pollInterval() time.Duration {
	if c.PollInterval > 0 {
		return c.PollInterval
	}
	return portalDefaultPollInterval
}

func (c *PortalClient) gracePeriod() time.Duration {
	if c.GracePeriod > 0 {
		return c.GracePeriod
	}
	return portalDefaultGracePeriod
}

// portalRunOptions portal 模式运行配置。JudgeTimeout 为单次 judge 调用时限，0 用默认值。
type portalRunOptions struct {
	AgentID      string
	RunID        string
	Judge        *Judge
	JudgeTimeout time.Duration
	Repeat       int
	Concurrency  int
}

// runPortal 只运行 answer_shape 任务；每题按 Repeat 顺序跑多次后合并，题目之间按 Concurrency 并发。
// ctx 取消后不再开始新题或新的重复，未开始的题记为丢失运行。
// Concurrency>1 时 Judge.Model 会被并发调用，须是并发安全的实现。
func runPortal(ctx context.Context, tasks []Task, c *PortalClient, o portalRunOptions) []TaskResult {
	var shape []Task
	for _, t := range tasks {
		if t.Category == "answer_shape" {
			shape = append(shape, t)
		}
	}
	repeat, conc := o.Repeat, o.Concurrency
	if repeat < 1 {
		repeat = 1
	}
	if conc < 1 {
		conc = 1
	}
	results := make([]TaskResult, len(shape))
	started := make([]bool, len(shape))
	sem := make(chan struct{}, conc)
	var wg sync.WaitGroup
dispatch:
	for i, task := range shape {
		select {
		case <-ctx.Done():
			break dispatch
		case sem <- struct{}{}:
		}
		if ctx.Err() != nil {
			<-sem
			break
		}
		started[i] = true
		wg.Add(1)
		go func(i int, task Task) {
			defer wg.Done()
			defer func() { <-sem }()
			runs := make([]TaskResult, 0, repeat)
			for r := 0; r < repeat; r++ {
				if r > 0 && ctx.Err() != nil {
					break
				}
				runs = append(runs, runPortalTask(ctx, c, o, task, r))
			}
			results[i] = mergeRuns(runs)
		}(i, task)
	}
	wg.Wait()
	for i, task := range shape {
		if !started[i] {
			results[i] = mergeRuns([]TaskResult{{TaskID: task.ID, Category: task.Category, FailureReason: "infra_error", Error: fmt.Sprintf("portal: not started: %v", ctx.Err())}})
		}
	}
	return results
}

func runPortalTask(ctx context.Context, c *PortalClient, o portalRunOptions, task Task, attempt int) TaskResult {
	title := fmt.Sprintf("eval-%s-%s-%d", o.RunID, task.ID, attempt+1)
	turn, err := c.RunTurn(ctx, o.AgentID, title, task.Input)
	res, judge := classifyPortalTurn(task, turn, err)
	if !judge {
		return res
	}
	jt := o.JudgeTimeout
	if jt <= 0 {
		jt = portalDefaultJudgeTimeout
	}
	jctx, cancel := context.WithTimeout(ctx, jt)
	defer cancel()
	return scoreAnswerShape(jctx, o.Judge, task, turn.Answer, summarizeTimeline(turn.Timeline))
}

// portalInfraMarkers 运行错误文本中表明基础设施故障的片段（小写匹配）；命中则记为丢失运行。
var portalInfraMarkers = []string{
	"bad gateway", "service unavailable",
	"connection refused", "connection reset", "caller identity", "inbound is disabled",
	"rate limit", "too many requests",
}

// portalInfraStatusRes 状态码只在有上下文时才认，避免 vmid 15029 这类数字误判；
// 裸 "forbidden"/"unauthorized" 不认，工具侧（如 k8s RBAC）的拒绝属于运行错误。
var portalInfraStatusRes = []*regexp.Regexp{
	regexp.MustCompile(`(?i)\b(?:status(?:\s*code)?|http)[\s:=]*(?:40[13]|429|50[234])\b`),
	regexp.MustCompile(`(?i)\b(?:401\s+unauthorized|403\s+forbidden|429\s+too many requests|50[234]\s+(?:bad gateway|service unavailable|gateway timeout))\b`),
}

// classifyPortalTurn 把一轮结果归类；第二个返回值为 true 表示是正常答复，需交给 judge 打分。
// 只有明确的基础设施故障记 infra_error（丢失运行，不进分母）；超时、跑满步数、HITL 与其他运行错误都计入失败。
func classifyPortalTurn(task Task, turn portalTurn, err error) (TaskResult, bool) {
	if err != nil {
		return TaskResult{TaskID: task.ID, Category: task.Category, FailureReason: "infra_error", Error: err.Error()}, false
	}
	if !turn.TimedOut && !turn.Failed && !turn.HITL {
		return TaskResult{}, true
	}
	ts := summarizeTimeline(turn.Timeline)
	res := TaskResult{TaskID: task.ID, Category: task.Category, Output: turn.Answer, Error: turn.Error, Steps: len(ts.Calls) + 1, Trace: &ts}
	seen := map[string]bool{}
	for _, call := range ts.Calls {
		if call.Tool != "" && !seen[call.Tool] {
			seen[call.Tool] = true
			res.ToolsUsed = append(res.ToolsUsed, call.Tool)
		}
	}
	switch {
	case turn.TimedOut:
		res.FailureReason, res.Attribution = "timeout", "harness"
	case turn.Failed:
		res.FailureReason, res.Attribution, res.HitMaxSteps = classifyRunError(turn.Error)
	default:
		res.FailureReason, res.Attribution = "hitl_required", "harness"
		res.Error = "agent requested human confirmation/input; eval cannot respond"
	}
	return res, false
}

// classifyRunError 先认 max steps，再查基础设施白名单（文本片段 + 带上下文的状态码），其余为 run_error。
func classifyRunError(msg string) (reason, attribution string, hitMaxSteps bool) {
	lower := strings.ToLower(msg)
	if strings.Contains(lower, "max steps") {
		return "model_error", "model", true
	}
	for _, m := range portalInfraMarkers {
		if strings.Contains(lower, m) {
			return "infra_error", "", false
		}
	}
	for _, re := range portalInfraStatusRes {
		if re.MatchString(msg) {
			return "infra_error", "", false
		}
	}
	return "run_error", "harness", false
}
