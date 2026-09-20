package tool

import "time"

// 审批与风险（docs/superpowers/plans/2026-09-12-maturity-hardening.md Task 7）
//
// 收敛范围：把四处危险操作确认（terminal / workspace file / browser / skill_manage）
// 中**完全重复**的机制收成一个策略点：
//   - 确认令牌有效期（TTL）判定，区分 not_found / expired；
//   - 过期即删除（一次性语义）。
//
// **不收敛**：各工具"什么算危险"的判定（危险路径模式、命令正则、动作白名单）仍留在
// 各工具内 —— 那是真正的安全边界，把它换成基于工具名的策略会削弱检查粒度。

// DefaultConfirmTTLSeconds 是确认令牌的默认有效期（秒）。唯一来源：此前四处各自
// 硬编码 300。
const DefaultConfirmTTLSeconds = 300

// 确认失败码，与 ConfirmTokenError 的取值保持一致。
const (
	ConfirmNotFound = "not_found"
	ConfirmExpired  = "expired"
)

// RiskLevel 描述工具操作的风险级别（用于文档化与审计，见 RiskLevelOf）。
type RiskLevel string

const (
	// RiskRead 只读：不改变本地/远端状态（读取、检索、列举）。
	RiskRead RiskLevel = "read"
	// RiskWrite 写入：改变本地状态（文件、技能、会话内待办、记忆）。
	RiskWrite RiskLevel = "write"
	// RiskDestructive 破坏性/执行：可执行任意命令或脚本（终端、SSH、脚本、hypertool、进程）。
	RiskDestructive RiskLevel = "destructive"
	// RiskNetwork 外呼：对外发起网络请求（HTTP、搜索、抓取、浏览器导航、出站消息、可观测查询）。
	RiskNetwork RiskLevel = "network"
	// RiskUnknown 未登记：动态注册的工具（如 MCP）或新增未分类工具。
	RiskUnknown RiskLevel = "unknown"
)

// ApprovalPolicy 统一危险操作确认的公共机制。
// 零值可用（TTLSeconds<=0 时使用 DefaultConfirmTTLSeconds）。
type ApprovalPolicy struct {
	TTLSeconds int
}

// DefaultApprovalPolicy 返回使用默认 TTL 的策略。
func DefaultApprovalPolicy() ApprovalPolicy {
	return ApprovalPolicy{TTLSeconds: DefaultConfirmTTLSeconds}
}

// EffectiveTTLSeconds 返回生效的 TTL 秒数。
func (p ApprovalPolicy) EffectiveTTLSeconds() int {
	if p.TTLSeconds <= 0 {
		return DefaultConfirmTTLSeconds
	}
	return p.TTLSeconds
}

// Expired 判断待确认项是否已过期；createdAt 为零值时视为未过期（由调用方保证已设置）。
func (p ApprovalPolicy) Expired(createdAt time.Time, now time.Time) bool {
	if createdAt.IsZero() {
		return false
	}
	if now.IsZero() {
		now = time.Now()
	}
	return now.Sub(createdAt) > time.Duration(p.EffectiveTTLSeconds())*time.Second
}

// ConfirmCheck 描述一次待确认项校验的输入。
type ConfirmCheck struct {
	// Found 为 false 表示令牌不存在或已被消费。
	Found     bool
	CreatedAt time.Time
	// OnExpire 在判定为过期时调用，用于删除过期项以保持一次性语义；可为 nil。
	OnExpire func() error
	// Now 为判定时间；零值取 time.Now()。
	Now time.Time
}

// ConfirmFailure 表示确认校验失败，Code 为 ConfirmNotFound / ConfirmExpired
// （调用方也可能传入 store 侧的原因，如 superseded / already_used）。
type ConfirmFailure struct {
	Code string
}

// Message 返回面向用户的说明（与既有 ConfirmTokenError 文案一致）。
func (f ConfirmFailure) Message() string {
	msg, _ := confirmTokenParts(f.Code)
	return msg
}

// Map 返回与 ConfirmTokenError 完全一致的结果结构。
func (f ConfirmFailure) Map() map[string]any {
	return ConfirmTokenError(f.Code)
}

// Verify 校验一次确认令牌：不存在 → not_found；超时 → expired 并触发 OnExpire。
// 返回 (failure, true) 时调用方应直接返回 failure.Map()（或按其家族的 map 形状包装）。
//
// 语义提示：Verify 是"令牌可用性"判定。调用方通常会把它放在动作/参数校验之前——
// 令牌已失效时，返回"请重新发起"比暴露 pending 的参数细节更有用。
func (p ApprovalPolicy) Verify(c ConfirmCheck) (ConfirmFailure, bool) {
	if !c.Found {
		return ConfirmFailure{Code: ConfirmNotFound}, true
	}
	if p.Expired(c.CreatedAt, c.Now) {
		if c.OnExpire != nil {
			_ = c.OnExpire()
		}
		return ConfirmFailure{Code: ConfirmExpired}, true
	}
	return ConfirmFailure{}, false
}

// builtinRiskLevels 为内置工具的风险登记表（文档化 + 审计用；见 approval_audit_test.go）。
// 动态注册的工具（MCP 等）不在表内，RiskLevelOf 返回 RiskUnknown。
var builtinRiskLevels = map[string]RiskLevel{
	// 只读
	"read_file":       RiskRead,
	"search_files":    RiskRead,
	"list_tables":     RiskRead,
	"describe_table":  RiskRead,
	"execute_read":    RiskRead,
	"list_tools":      RiskRead,
	"tool_search":     RiskRead,
	"tool_describe":   RiskRead,
	"memory_recall":   RiskRead,
	"memory_get":      RiskRead,
	"rca_grep":        RiskRead,
	"rca_glob":        RiskRead,
	"rca_read":        RiskRead,
	"rca_symbol":      RiskRead,
	"load_skill":      RiskRead,
	"read_skill_file": RiskRead,
	"skills_list":     RiskRead,
	"skill_view":      RiskRead,
	"calculator_add":  RiskRead,
	// vision_analyze 把图片发给模型 provider：属外呼（有成本、走网络），
	// 因此串行（见 builtinRequiresSequential）而非只读。
	"vision_analyze": RiskNetwork,
	"ask_user":       RiskRead, // 交互，但不改变系统状态

	// 破坏性 / 执行
	"terminal":             RiskDestructive,
	"process":              RiskDestructive,
	"ssh_exec":             RiskDestructive,
	"scp":                  RiskDestructive,
	"execute_skill_script": RiskDestructive,
	"hypertool":            RiskDestructive,

	// 写入
	"write_file":      RiskWrite,
	"patch":           RiskWrite,
	"execute_write":   RiskWrite,
	"todo":            RiskWrite,
	"memory_remember": RiskWrite,
	"append_learning": RiskWrite,
	"skill_manage":    RiskWrite,
	"cronjob":         RiskWrite,
	"browser_click":   RiskWrite,
	"browser_type":    RiskWrite,

	// 外呼
	"http_request":       RiskNetwork,
	"web_search":         RiskNetwork,
	"web_extract":        RiskNetwork,
	"browser_navigate":   RiskNetwork,
	"browser_snapshot":   RiskNetwork,
	"browser_scroll":     RiskNetwork,
	"browser_back":       RiskNetwork,
	"browser_press":      RiskNetwork,
	"browser_get_images": RiskNetwork,
	"browser_console":    RiskNetwork,
	"browser_vision":     RiskNetwork,
	"browser_cdp":        RiskNetwork,
	"browser_dialog":     RiskNetwork,
	"send_to_wecom":      RiskNetwork,
	"jaeger_trace":       RiskNetwork,
	"es_log_query":       RiskNetwork,
	"tool_call":          RiskUnknown, // 元工具：真实风险取决于被调用的内层工具
}

// RiskLevelOf 返回工具的风险级别；未登记（含动态 MCP 工具）返回 RiskUnknown。
func RiskLevelOf(toolName string) RiskLevel {
	if lvl, ok := builtinRiskLevels[toolName]; ok {
		return lvl
	}
	return RiskUnknown
}
