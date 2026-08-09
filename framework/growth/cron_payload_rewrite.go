package growth

import "strings"

// RewriteSkillExecutePayload 将 skill_execute 的 payload_content 首段（逻辑 skill name）按 renames 替换。
// 格式约定：{name}/scripts/{file}（见 portal cron executor）。
func RewriteSkillExecutePayload(payload string, renames map[string]string) (string, bool) {
	if len(renames) == 0 {
		return payload, false
	}
	payload = strings.TrimSpace(payload)
	if payload == "" {
		return payload, false
	}
	parts := strings.SplitN(payload, "/", 3)
	if len(parts) < 2 {
		return payload, false
	}
	skillName := parts[0]
	newName, ok := renames[skillName]
	if !ok || newName == "" || newName == skillName {
		return payload, false
	}
	parts[0] = newName
	return strings.Join(parts, "/"), true
}
