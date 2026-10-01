package service

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"backend/internal/chat"

	"github.com/sixath/framework/events"
	agent "github.com/sixath/framework/harness"
	"github.com/sixath/framework/redact"
	"github.com/sixath/framework/tool"
	toolskill "github.com/sixath/framework/tool/skillops"
)

type ChatStreamEventType string

const (
	ChatStreamEventChunk           ChatStreamEventType = "chunk"
	ChatStreamEventConfirmRequired ChatStreamEventType = "confirm_required"
	ChatStreamEventConfirmResult   ChatStreamEventType = "confirm_result"
	ChatStreamEventInputRequired   ChatStreamEventType = "input_required"
	ChatStreamEventError           ChatStreamEventType = "error"
	ChatStreamEventDebug           ChatStreamEventType = "debug"
	ChatStreamEventToolCall        ChatStreamEventType = "tool_call"
	ChatStreamEventModelCall       ChatStreamEventType = "model_call"
	// ChatStreamEventPlan / PlanStep 表示 Plan-Execute 模式产出的规划与其步骤进度。
	ChatStreamEventPlan     ChatStreamEventType = "plan"
	ChatStreamEventPlanStep ChatStreamEventType = "plan_step"
	// ChatStreamEventCancelled 表示本轮因用户停止/断连被取消；
	// 已产生的正文**照常落库**（不置 Failed），前端据此展示"已中断"。
	ChatStreamEventCancelled ChatStreamEventType = "cancelled"
	ChatStreamEventMEA       ChatStreamEventType = "mea"
	// ChatStreamEventFinal 携带本轮最终答复（Done.Text / 末条 assistant），不再下发为 chunk；
	// 落库时优先用它，避免把中间步骤的过程性文字和被 stop hook 打回的草稿拼进历史。
	ChatStreamEventFinal ChatStreamEventType = "final"
)

const toolPayloadFieldLimit = 8 * 1024 // 单字段截断上限（字节）

type ChatStreamEvent struct {
	Type          ChatStreamEventType
	Content       string
	Error         string
	Confirmation  *ChatConfirmationRequest
	ConfirmResult *ConfirmResultPayload
	Input         *ChatInputRequest
	ToolCall      *ToolCallPayload
	ModelCall     *ModelCallPayload
	MEA           *MEAStreamPayload
	Plan          *agent.Plan     `json:"plan,omitempty"`
	PlanStep      *agent.PlanStep `json:"plan_step,omitempty"`
}

// MEAStreamPayload is emitted after a Manage-Execute-Audit round or final result (M0.5).
type MEAStreamPayload struct {
	Phase     string `json:"phase"` // started | round | finished
	Reason    string `json:"reason,omitempty"`
	Round     int    `json:"round,omitempty"`
	Pending   int    `json:"pending,omitempty"`
	Completed int    `json:"completed,omitempty"`
	Goal      string `json:"goal,omitempty"`
}

type ConfirmResultPayload struct {
	OK        bool   `json:"ok"`
	Kind      string `json:"kind"`
	Token     string `json:"token"`
	Error     string `json:"error,omitempty"`
	ErrorCode string `json:"error_code,omitempty"`
}

type ToolCallPayload struct {
	ID         string `json:"id"`
	Step       int    `json:"step"`
	Phase      string `json:"phase"`
	ToolName   string `json:"tool_name"`
	Arguments  any    `json:"arguments,omitempty"`
	Result     any    `json:"result,omitempty"`
	Error      string `json:"error,omitempty"`
	Allowed    bool   `json:"allowed"`
	Decision   string `json:"decision,omitempty"`
	DurationMS int64  `json:"duration_ms,omitempty"`
	Truncated  bool   `json:"truncated,omitempty"`
}

type ModelCallPayload struct {
	Step         int    `json:"step"`
	Phase        string `json:"phase"`
	Mode         string `json:"mode,omitempty"`
	Model        string `json:"model,omitempty"`
	InputTokens  int    `json:"input_tokens,omitempty"`
	OutputTokens int    `json:"output_tokens,omitempty"`
	MessageCount int    `json:"message_count,omitempty"`
	// EstimatedCostUSD 为本次模型调用估算成本（美元），按 chat.EstimateCost 计价表计算。
	EstimatedCostUSD float64 `json:"estimated_cost_usd,omitempty"`
}

// truncateField 将任意值转为 JSON，超过上限时截断并返回 truncated=true。
func truncateField(v any) (any, bool) {
	if v == nil {
		return nil, false
	}
	b, err := json.Marshal(v)
	if err != nil {
		return v, false
	}
	if len(b) <= toolPayloadFieldLimit {
		return v, false
	}
	// 按字节切片可能截断多字节 UTF-8 字符，去掉非法尾字节以保证合法 UTF-8。
	s := strings.ToValidUTF8(string(b[:toolPayloadFieldLimit]), "")
	return s + "…[truncated]", true
}

func toolCallPayloadFromRecord(rec agent.ToolCallRecord, phase string) *ToolCallPayload {
	p := &ToolCallPayload{
		ID:         rec.ToolCallID,
		Step:       rec.Step,
		Phase:      phase,
		ToolName:   rec.ToolName,
		Error:      redact.String(rec.Error),
		Allowed:    rec.Allowed,
		Decision:   rec.Decision,
		DurationMS: rec.DurationMS,
	}
	var argsIn any
	if rec.Arguments != nil {
		argsIn = redact.Value(rec.Arguments)
	}
	args, aTrunc := truncateField(argsIn)
	res, rTrunc := truncateField(redactResult(rec.Result))
	p.Arguments = args
	p.Result = res
	p.Truncated = aTrunc || rTrunc
	return p
}

// redactResult 把结果经 JSON 解码为通用 map/slice 后逐个字符串遮盖，覆盖结构体（SSHExecResult、QuerySpillStub.Sample）
// 与 []map[string]any 等 redact.Value 不直接递归的类型。不能对 JSON 文本跑正则：URL 等模式会跨字段匹配、转义引号会漏遮。
// 无凭据时返回原值，保持下游序列化不变。
func redactResult(v any) any {
	switch t := v.(type) {
	case nil:
		return nil
	case string:
		return redact.String(t)
	}
	b, err := json.Marshal(v)
	if err != nil {
		return v
	}
	var generic any
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.UseNumber()
	if err := dec.Decode(&generic); err != nil {
		return v
	}
	red := redact.Value(generic)
	keepPendingToken(generic, red)
	before, err1 := json.Marshal(generic)
	after, err2 := json.Marshal(red)
	if err1 != nil || err2 != nil || string(before) == string(after) {
		return v
	}
	return red
}

// keepPendingToken 保留待确认结果顶层的一次性 token：前端刷新后依赖 timeline 中的 result.token 重建确认 / ask_user 卡片。
func keepPendingToken(orig, red any) {
	om, ok := orig.(map[string]any)
	if !ok || om["status"] != "pending" {
		return
	}
	tok, ok := om["token"]
	if !ok {
		return
	}
	if rm, ok := red.(map[string]any); ok {
		rm["token"] = tok
	}
}

// modelCallEventFromBus 将事件总线上的 ModelInvoked/ModelResponded 事件映射为 ModelCallPayload。
// 其他事件返回 nil。
func modelCallEventFromBus(e events.Event, modelName string) *ModelCallPayload {
	var phase string
	switch e.Kind {
	case events.ModelInvoked:
		phase = "invoked"
	case events.ModelResponded:
		phase = "responded"
	default:
		return nil
	}
	p := &ModelCallPayload{Phase: phase, Model: modelName}
	p.Step = intFromAny(e.Payload["step"])
	if v, ok := e.Payload["mode"].(string); ok {
		p.Mode = v
	}
	p.MessageCount = intFromAny(e.Payload["message_count"])
	p.InputTokens = intFromAny(e.Payload["input_tokens"])
	p.OutputTokens = intFromAny(e.Payload["output_tokens"])
	p.EstimatedCostUSD = chat.EstimateCost(modelName, p.InputTokens, p.OutputTokens)
	return p
}

type ChatInputRequest struct {
	ID         string   `json:"id,omitempty"`
	ToolCallID string   `json:"tool_call_id,omitempty"`
	RequestID  string   `json:"request_id"`
	Token      string   `json:"token"`
	Kind       string   `json:"kind"`
	Field      string   `json:"field"`
	Title      string   `json:"title"`
	Prompt     string   `json:"prompt"`
	Options    []string `json:"options,omitempty"`
	Required   bool     `json:"required"`
	ExpiresIn  int      `json:"expires_in,omitempty"`
	Severity   string   `json:"severity"`
}

type ChatConfirmationRequest struct {
	ID          string `json:"id,omitempty"`
	Kind        string `json:"kind"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Token       string `json:"token"`
	DSL         string `json:"dsl"`
	ExpiresIn   int    `json:"expires_in,omitempty"`
	Severity    string `json:"severity"`
	ResourceKey string `json:"resource_key,omitempty"`
	ExpiresAt   string `json:"expires_at,omitempty"` // RFC3339
}

func confirmationRequestsFromResponse(resp *agent.Response) []ChatConfirmationRequest {
	if resp == nil || resp.Metadata == nil {
		return nil
	}
	trace, ok := resp.Metadata["trace"].(*agent.RunTrace)
	if !ok || trace == nil {
		return nil
	}
	// skill_manage tokens that the model already confirmed in this turn must not
	// produce a UI card — clicking would hit already_used.
	selfConfirmedSkillTokens := skillManageSelfConfirmedTokens(trace.ToolCalls)
	items := make([]ChatConfirmationRequest, 0, 1)
	for _, call := range trace.ToolCalls {
		if req := skillManageConfirmationFromCall(call); req != nil {
			if selfConfirmedSkillTokens[req.Token] {
				continue
			}
			items = append(items, *req)
			continue
		}
		if req := executeWriteConfirmationFromCall(call); req != nil {
			items = append(items, *req)
			continue
		}
		if req := caseSaveConfirmationFromCall(call); req != nil {
			items = append(items, *req)
			continue
		}
		if req := terminalConfirmationFromCall(call); req != nil {
			items = append(items, *req)
			continue
		}
		if req := vmRunCmdConfirmationFromCall(call); req != nil {
			items = append(items, *req)
			continue
		}
		if req := workspaceFileConfirmationFromCall(call); req != nil {
			items = append(items, *req)
			continue
		}
		if req := browserConfirmationFromCall(call); req != nil {
			items = append(items, *req)
		}
	}
	return items
}

func skillManageSelfConfirmedTokens(calls []agent.ToolCallRecord) map[string]bool {
	out := make(map[string]bool)
	for _, call := range calls {
		if call.ToolName != "skill_manage" || call.Arguments == nil {
			continue
		}
		tok, _ := call.Arguments["confirm_token"].(string)
		tok = strings.TrimSpace(tok)
		if tok != "" {
			out[tok] = true
		}
	}
	return out
}

func skillManageConfirmationFromCall(call agent.ToolCallRecord) *ChatConfirmationRequest {
	if call.ToolName != "skill_manage" {
		return nil
	}
	status, token, action, name, preview, expiresIn, expiresAt := skillManagePendingFields(call.Result)
	if status != "pending" || token == "" || action == "" || name == "" {
		return nil
	}
	if preview == "" {
		preview = fmt.Sprintf("%s skill: %s", action, name)
	}
	title := "Confirm skill " + action
	return &ChatConfirmationRequest{
		ID:          fmt.Sprintf("%s:%s", call.ToolCallID, token),
		Kind:        "skill_manage",
		Title:       title,
		Description: fmt.Sprintf("Review the skill %q before it is applied.", name),
		Token:       token,
		DSL:         preview,
		ExpiresIn:   expiresIn,
		Severity:    "danger",
		ResourceKey: action + ":" + name,
		ExpiresAt:   expiresAt,
	}
}

// caseSaveConfirmationFromCall 把 case_library propose 的草稿转成确认卡片；草稿已落盘，确认后才参与召回。
func caseSaveConfirmationFromCall(call agent.ToolCallRecord) *ChatConfirmationRequest {
	if call.ToolName != tool.CaseLibraryToolName {
		return nil
	}
	m, ok := call.Result.(map[string]any)
	if !ok {
		return nil
	}
	status, _ := m["status"].(string)
	token, _ := m["token"].(string)
	if status != "pending" || token == "" {
		return nil
	}
	name, _ := m["name"].(string)
	preview, _ := m["preview"].(string)
	return &ChatConfirmationRequest{
		ID:          fmt.Sprintf("%s:%s", call.ToolCallID, token),
		Kind:        tool.CaseSaveConfirmKind,
		Title:       "确认沉淀排障案例",
		Description: fmt.Sprintf("确认后，案例「%s」会在以后遇到相似症状时被优先验证。有误可先在案例库中修改或删除。", name),
		Token:       token,
		DSL:         preview,
		Severity:    "info",
		ResourceKey: "case:" + token,
	}
}

func skillManagePendingFields(result any) (status, token, action, name, preview string, expiresIn int, expiresAt string) {
	switch v := result.(type) {
	case map[string]any:
		status, _ = v["status"].(string)
		token, _ = v["token"].(string)
		action, _ = v["action"].(string)
		name, _ = v["name"].(string)
		preview, _ = v["preview"].(string)
		expiresIn = intFromAny(v["expires_in"])
		expiresAt, _ = v["expires_at"].(string)
		if expiresAt == "" {
			expiresAt = expiresAtFromCreatedTTL(v["created_at"], expiresIn)
		}
	case toolskill.SkillManagePendingResponse:
		status = v.Status
		token = v.Token
		action = v.Action
		name = v.Name
		preview = v.Preview
		expiresIn = v.ExpiresIn
	case *toolskill.SkillManagePendingResponse:
		if v != nil {
			status = v.Status
			token = v.Token
			action = v.Action
			name = v.Name
			preview = v.Preview
			expiresIn = v.ExpiresIn
		}
	}
	return
}

// confirmResultFromSkillManageMap builds the SSE confirm_result payload after applying a skill_manage confirm.
func confirmResultFromSkillManageMap(token string, result map[string]any) *ConfirmResultPayload {
	payload := &ConfirmResultPayload{
		OK:    true,
		Kind:  "skill_manage",
		Token: token,
	}
	if result == nil {
		return payload
	}
	if ev, has := result["error"]; has && ev != nil && fmt.Sprint(ev) != "" {
		payload.OK = false
		payload.Error = fmt.Sprint(ev)
		if code, ok := result["error_code"].(string); ok {
			payload.ErrorCode = code
		}
	}
	return payload
}

func expiresAtFromCreatedTTL(createdAt any, expiresIn int) string {
	if expiresIn <= 0 {
		return ""
	}
	var created time.Time
	switch v := createdAt.(type) {
	case time.Time:
		created = v
	case string:
		if v == "" {
			return ""
		}
		parsed, err := time.Parse(time.RFC3339, v)
		if err != nil {
			return ""
		}
		created = parsed
	default:
		return ""
	}
	if created.IsZero() {
		return ""
	}
	return created.Add(time.Duration(expiresIn) * time.Second).UTC().Format(time.RFC3339)
}

func executeWriteConfirmationFromCall(call agent.ToolCallRecord) *ChatConfirmationRequest {
	if call.ToolName != "execute_write" {
		return nil
	}
	result, ok := call.Result.(map[string]any)
	if !ok {
		return nil
	}
	status, _ := result["status"].(string)
	token, _ := result["token"].(string)
	dsl, _ := result["dsl"].(string)
	if status != "pending" || token == "" || dsl == "" {
		return nil
	}
	return &ChatConfirmationRequest{
		ID:          fmt.Sprintf("%s:%s", call.ToolCallID, token),
		Kind:        "execute_write",
		Title:       "Confirm write operation",
		Description: "Review the operation before it is executed.",
		Token:       token,
		DSL:         dsl,
		ExpiresIn:   intFromAny(result["expires_in"]),
		Severity:    "danger",
	}
}

func terminalConfirmationFromCall(call agent.ToolCallRecord) *ChatConfirmationRequest {
	if call.ToolName != "terminal" {
		return nil
	}
	result, ok := call.Result.(map[string]any)
	if !ok {
		return nil
	}
	status, _ := result["status"].(string)
	token, _ := result["token"].(string)
	command, _ := result["command"].(string)
	if status != "pending" || token == "" || command == "" {
		return nil
	}
	return &ChatConfirmationRequest{
		ID:          fmt.Sprintf("%s:%s", call.ToolCallID, token),
		Kind:        "terminal",
		Title:       "Confirm terminal command",
		Description: "Review the shell command before it is executed.",
		Token:       token,
		DSL:         command,
		ExpiresIn:   intFromAny(result["expires_in"]),
		Severity:    "danger",
	}
}

func vmRunCmdConfirmationFromCall(call agent.ToolCallRecord) *ChatConfirmationRequest {
	if call.ToolName != "vm_run_cmd" {
		return nil
	}
	result, ok := call.Result.(map[string]any)
	if !ok {
		return nil
	}
	status, _ := result["status"].(string)
	token, _ := result["token"].(string)
	command, _ := result["command"].(string)
	if status != "pending" || token == "" || command == "" {
		return nil
	}
	return &ChatConfirmationRequest{
		ID:          fmt.Sprintf("%s:%s", call.ToolCallID, token),
		Kind:        "vm_run_cmd",
		Title:       "Confirm instance command",
		Description: "Review the instance command before it is executed.",
		Token:       token,
		DSL:         command,
		ExpiresIn:   intFromAny(result["expires_in"]),
		Severity:    "danger",
	}
}

func workspaceFileConfirmationFromCall(call agent.ToolCallRecord) *ChatConfirmationRequest {
	if call.ToolName != "write_file" && call.ToolName != "patch" {
		return nil
	}
	result, ok := call.Result.(map[string]any)
	if !ok {
		return nil
	}
	status, _ := result["status"].(string)
	token, _ := result["token"].(string)
	path, _ := result["path"].(string)
	if status != "pending" || token == "" || path == "" {
		return nil
	}
	preview, _ := result["preview"].(string)
	if preview == "" {
		preview = path
	}
	title := "Confirm workspace file write"
	if call.ToolName == "patch" {
		title = "Confirm workspace file patch"
	}
	return &ChatConfirmationRequest{
		ID:          fmt.Sprintf("%s:%s", call.ToolCallID, token),
		Kind:        "workspace_file",
		Title:       title,
		Description: "Review the sensitive file change before it is applied.",
		Token:       token,
		DSL:         preview,
		ExpiresIn:   intFromAny(result["expires_in"]),
		Severity:    "danger",
	}
}

func browserConfirmationFromCall(call agent.ToolCallRecord) *ChatConfirmationRequest {
	switch call.ToolName {
	case "browser_navigate", "browser_click", "browser_type":
	default:
		return nil
	}
	result, ok := call.Result.(map[string]any)
	if !ok {
		return nil
	}
	status, _ := result["status"].(string)
	token, _ := result["token"].(string)
	preview, _ := result["preview"].(string)
	action, _ := result["action"].(string)
	if status != "pending" || token == "" {
		return nil
	}
	if preview == "" {
		preview = action
	}
	if preview == "" {
		preview = call.ToolName
	}
	return &ChatConfirmationRequest{
		ID:          fmt.Sprintf("%s:%s", call.ToolCallID, token),
		Kind:        "browser",
		Title:       "Confirm browser action",
		Description: "Review the browser action before it is applied.",
		Token:       token,
		DSL:         preview,
		ExpiresIn:   intFromAny(result["expires_in"]),
		Severity:    "danger",
	}
}

func inputRequestsFromResponse(resp *agent.Response) []ChatInputRequest {
	if resp == nil || resp.Metadata == nil {
		return nil
	}
	trace, ok := resp.Metadata["trace"].(*agent.RunTrace)
	if !ok || trace == nil {
		return nil
	}
	items := make([]ChatInputRequest, 0, 1)
	for _, call := range trace.ToolCalls {
		if item := inputRequestFromToolRecord(call); item != nil {
			items = append(items, *item)
		}
	}
	return items
}

func inputRequestFromToolRecord(call agent.ToolCallRecord) *ChatInputRequest {
	if call.ToolName != "ask_user" {
		return nil
	}
	result, ok := call.Result.(map[string]any)
	if !ok {
		return nil
	}
	status, _ := result["status"].(string)
	token, _ := result["token"].(string)
	requestID, _ := result["request_id"].(string)
	if status != "pending" || token == "" || requestID == "" {
		return nil
	}
	kind, _ := result["kind"].(string)
	field, _ := result["field"].(string)
	prompt, _ := result["prompt"].(string)
	title, _ := result["title"].(string)
	required, _ := result["required"].(bool)
	var options []string
	if raw, ok := result["options"].([]any); ok {
		for _, item := range raw {
			if s, ok := item.(string); ok && s != "" {
				options = append(options, s)
			}
		}
	}
	severity := "default"
	if kind == "password" {
		severity = "warning"
	}
	return &ChatInputRequest{
		ID:         fmt.Sprintf("%s:%s", call.ToolCallID, token),
		ToolCallID: call.ToolCallID,
		RequestID:  requestID,
		Token:      token,
		Kind:       kind,
		Field:      field,
		Title:      title,
		Prompt:     prompt,
		Options:    options,
		Required:   required,
		ExpiresIn:  intFromAny(result["expires_in"]),
		Severity:   severity,
	}
}

func streamEventsFromResponse(resp *agent.Response) []ChatStreamEvent {
	if resp == nil {
		return nil
	}
	events := make([]ChatStreamEvent, 0, 1)
	if resp.Text != "" {
		events = append(events, ChatStreamEvent{Type: ChatStreamEventChunk, Content: resp.Text})
	}
	for _, item := range inputRequestsFromResponse(resp) {
		input := item
		events = append(events, ChatStreamEvent{
			Type:  ChatStreamEventInputRequired,
			Input: &input,
		})
	}
	for _, item := range confirmationRequestsFromResponse(resp) {
		confirmation := item
		events = append(events, ChatStreamEvent{
			Type:         ChatStreamEventConfirmRequired,
			Confirmation: &confirmation,
		})
	}
	return events
}

func intFromAny(v any) int {
	switch n := v.(type) {
	case int:
		return n
	case int32:
		return int(n)
	case int64:
		return int(n)
	case float64:
		return int(n)
	default:
		return 0
	}
}
