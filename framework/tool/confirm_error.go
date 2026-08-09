package tool

// ConfirmTokenError returns the structured confirm-failure map shared by danger tools
// (terminal / workspace_file / browser / execute_write). Messages match skill_manage for expired/not_found.
func ConfirmTokenError(code string) map[string]any {
	msg, code := confirmTokenParts(code)
	return map[string]any{"error": msg, "error_code": code}
}

func confirmTokenParts(code string) (msg, errCode string) {
	msg = "确认已失效（可能已被替换、已使用或服务重启），请重新发起"
	switch code {
	case "expired":
		return "确认已过期，请让助手重新发起操作", "expired"
	case "not_found":
		return msg, "not_found"
	default:
		return msg, "not_found"
	}
}
