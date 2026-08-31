package chat

import (
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/sixath/framework/agent"
	"github.com/sixath/framework/model"
)

const (
	MetadataKeyTaskLock  = "task_lock"
	MetadataKeyTaskLockQ = "task_lock_q"
	maxKnownValues       = 16
	maxLockHistoryMsgs   = 8
)

// TurnTaskLock freezes this turn's investigative goal Q (G) and optional delivery D.
type TurnTaskLock struct {
	Q                 string
	Delivery          string
	KnownValues       []string
	HasPriorAssistant bool
}

var (
	kvRightRe  = regexp.MustCompile(`(?i)\b[\w.-]{2,24}\s*[:=]\s*(\S{3,})`)
	backtickRe = regexp.MustCompile("`([^`\n]{4,})`")
	dquoteRe   = regexp.MustCompile(`"([^"\n]{4,})"`)
	squoteRe   = regexp.MustCompile(`'([^'\n]{4,})'`)
	identRe    = regexp.MustCompile(`[A-Za-z0-9][A-Za-z0-9_-]{5,}`)
)

// BuildTurnTaskLock extracts G (Q), optional delivery D, opaque KnownValues, and follow-up.
func BuildTurnTaskLock(userText string, history []model.Message) TurnTaskLock {
	d := strings.TrimSpace(userText)
	msgs := filterLockHistory(history)
	lock := TurnTaskLock{
		Q:                 d,
		KnownValues:       extractKnownValues(d, msgs),
		HasPriorAssistant: hasPriorAssistant(msgs),
	}
	if lock.HasPriorAssistant && isDeliveryUtterance(d) && !hasNewOpaqueIdent(d, msgs) {
		if g := lastNonDeliveryUserText(msgs, d); g != "" {
			lock.Q = g
			if g != d {
				lock.Delivery = d
			}
		}
	}
	return lock
}

// TaskLockFromRequest reads the lock from Request.Metadata.
func TaskLockFromRequest(req *agent.Request) *TurnTaskLock {
	if req == nil || req.Metadata == nil {
		return nil
	}
	switch v := req.Metadata[MetadataKeyTaskLock].(type) {
	case *TurnTaskLock:
		return v
	case TurnTaskLock:
		cp := v
		return &cp
	default:
		return nil
	}
}

// AppendTaskLock appends the lock block after the completed system prompt.
func AppendTaskLock(prompt string, lock TurnTaskLock) string {
	if strings.TrimSpace(lock.Q) == "" {
		return prompt
	}
	block := lock.Format()
	if strings.TrimSpace(prompt) == "" {
		return block
	}
	return prompt + "\n\n---\n\n" + block
}

// MergeTaskLockMetadata writes lock object and Q string into Request.Metadata.
func MergeTaskLockMetadata(md map[string]any, lock TurnTaskLock) map[string]any {
	if md == nil {
		md = map[string]any{}
	}
	cp := lock
	md[MetadataKeyTaskLock] = &cp
	md[MetadataKeyTaskLockQ] = lock.Q
	return md
}

// Format renders the lock as a system prompt suffix.
func (l TurnTaskLock) Format() string {
	var b strings.Builder
	b.WriteString("【本轮任务锁】\n")
	b.WriteString("用户问题（不可改写）：")
	b.WriteString(l.Q)
	b.WriteString("\n")
	if strings.TrimSpace(l.Delivery) != "" {
		b.WriteString("本轮交付（完成此项以回答上述问题，不得把问题换成交付句）：")
		b.WriteString(l.Delivery)
		b.WriteString("\n")
	}
	b.WriteString("上下文已出现的取值（禁止再向用户索取同等信息）：")
	if len(l.KnownValues) == 0 {
		b.WriteString("（无）")
	} else {
		b.WriteString(strings.Join(l.KnownValues, "；"))
	}
	b.WriteString("\n")
	b.WriteString("规则：Skill 与工具只是手段。查询 0 击时用现有证据直接回答上述用户问题，包括明确「未查到」。不要把本轮问题换成另一套排查的 intake。")
	return b.String()
}

func qLooksLikeIntake(q string) bool {
	for _, p := range []string{"请提供", "请给出", "麻烦提供"} {
		if strings.Contains(q, p) {
			return true
		}
	}
	return false
}

var (
	deliveryBanSubstr = []string{"另一条", "换成", "先放放", "改查", "新的流水", "另外一个"}
	deliveryHitSubstr = []string{
		"打印", "贴出来", "打出来", "原文", "更加直观",
		"补查", "再查", "现在补", "换索引", "索引是", "索引名",
		"上面有查", "没有打印", "没打印", "没贴出来", "没有贴",
	}
)

func isDeliveryUtterance(s string) bool {
	n := strings.ToLower(strings.TrimSpace(s))
	if n == "" {
		return false
	}
	for _, ban := range deliveryBanSubstr {
		if strings.Contains(n, ban) {
			return false
		}
	}
	if n == "继续" || n == "需要" || n == "请继续" || n == "需要请继续" ||
		n == "接着" || n == "然后" || n == "然后呢" || n == "往下" || n == "没做完" ||
		n == "剩下的" || n == "查完" {
		return true
	}
	for _, hit := range deliveryHitSubstr {
		if strings.Contains(n, hit) {
			return true
		}
	}
	return false
}

func hasNewOpaqueIdent(d string, msgs []model.Message) bool {
	prior := extractKnownValues("", msgsWithoutCurrentUser(msgs, d))
	seen := make(map[string]struct{}, len(prior))
	for _, v := range prior {
		seen[v] = struct{}{}
	}
	found := false
	collectOpaque(d, func(tok string) {
		tok = strings.Trim(tok, " \t\r\n,;:。，；、)(（）[]{}<>\"'`")
		if tok == "" {
			return
		}
		if _, ok := seen[tok]; !ok {
			found = true
		}
	})
	return found
}

func msgsWithoutCurrentUser(msgs []model.Message, d string) []model.Message {
	last := -1
	for i := len(msgs) - 1; i >= 0; i-- {
		if strings.EqualFold(strings.TrimSpace(msgs[i].Role), "user") && strings.TrimSpace(msgs[i].Content) == d {
			last = i
			break
		}
	}
	if last < 0 {
		return msgs
	}
	out := make([]model.Message, 0, len(msgs)-1)
	out = append(out, msgs[:last]...)
	out = append(out, msgs[last+1:]...)
	return out
}

func lastNonDeliveryUserText(msgs []model.Message, d string) string {
	for i := len(msgs) - 1; i >= 0; i-- {
		if !strings.EqualFold(strings.TrimSpace(msgs[i].Role), "user") {
			continue
		}
		body := strings.TrimSpace(msgs[i].Content)
		if body == "" || body == d || isDeliveryUtterance(body) {
			continue
		}
		return body
	}
	return ""
}

func filterLockHistory(history []model.Message) []model.Message {
	var out []model.Message
	for _, m := range history {
		role := strings.ToLower(strings.TrimSpace(m.Role))
		if role != "user" && role != "assistant" {
			continue
		}
		if strings.TrimSpace(m.Content) == "" {
			continue
		}
		out = append(out, m)
	}
	if len(out) > maxLockHistoryMsgs {
		out = out[len(out)-maxLockHistoryMsgs:]
	}
	return out
}

func hasPriorAssistant(msgs []model.Message) bool {
	lastUser := -1
	for i := len(msgs) - 1; i >= 0; i-- {
		if strings.EqualFold(strings.TrimSpace(msgs[i].Role), "user") {
			lastUser = i
			break
		}
	}
	if lastUser < 0 {
		lastUser = len(msgs)
	}
	for i := 0; i < lastUser; i++ {
		if strings.EqualFold(strings.TrimSpace(msgs[i].Role), "assistant") && strings.TrimSpace(msgs[i].Content) != "" {
			return true
		}
	}
	return false
}

func extractKnownValues(q string, msgs []model.Message) []string {
	seen := make(map[string]struct{})
	var out []string
	add := func(v string) {
		v = strings.Trim(v, " \t\r\n,;:。，；、)(（）[]{}<>\"'`")
		if v == "" {
			return
		}
		if _, ok := seen[v]; ok {
			return
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	collectOpaque(q, add)
	for _, m := range msgs {
		collectOpaque(m.Content, add)
	}
	if len(out) > maxKnownValues {
		out = out[:maxKnownValues]
	}
	return out
}

func collectOpaque(text string, add func(string)) {
	if strings.TrimSpace(text) == "" {
		return
	}
	for _, m := range kvRightRe.FindAllStringSubmatch(text, -1) {
		if len(m) > 1 {
			add(m[1])
		}
	}
	for _, re := range []*regexp.Regexp{backtickRe, dquoteRe, squoteRe} {
		for _, m := range re.FindAllStringSubmatch(text, -1) {
			if len(m) > 1 {
				add(m[1])
			}
		}
	}
	for _, tok := range identRe.FindAllString(text, -1) {
		if isIdentOpaque(tok) {
			add(tok)
		}
	}
}

func isIdentOpaque(v string) bool {
	if utf8.RuneCountInString(v) < 6 {
		return false
	}
	hasLetter, hasDigit, hasSep := false, false, false
	for _, r := range v {
		switch {
		case r == '_' || r == '-':
			hasSep = true
		case unicode.IsDigit(r):
			hasDigit = true
		case (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z'):
			hasLetter = true
		}
	}
	if hasLetter && hasDigit {
		return true
	}
	return hasSep && utf8.RuneCountInString(v) >= 6
}
