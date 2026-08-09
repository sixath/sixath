package tool

// ToIntNonNegative 尝试从多种数字类型解析出非负 int。
// 供 data 工具（execute_read 行数/超时）与 ssh/scp 工具（端口/超时）等共用。
func ToIntNonNegative(v any) (int, bool) {
	switch x := v.(type) {
	case int:
		if x < 0 {
			return 0, false
		}
		return x, true
	case int32:
		if x < 0 {
			return 0, false
		}
		return int(x), true
	case int64:
		if x < 0 {
			return 0, false
		}
		return int(x), true
	case float32:
		if x < 0 {
			return 0, false
		}
		return int(x), true
	case float64:
		if x < 0 {
			return 0, false
		}
		return int(x), true
	default:
		return 0, false
	}
}
