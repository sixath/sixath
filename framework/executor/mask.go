package executor

import "strings"

// MaskLiterals 将 SQL 中单引号字符串字面量替换为 '***'，用于 Info 级日志脱敏。
func MaskLiterals(sql string) string {
	if sql == "" {
		return sql
	}
	var b strings.Builder
	b.Grow(len(sql))
	i := 0
	for i < len(sql) {
		if sql[i] != '\'' {
			b.WriteByte(sql[i])
			i++
			continue
		}
		b.WriteString("'***'")
		i++
		for i < len(sql) {
			if sql[i] == '\\' && i+1 < len(sql) {
				i += 2
				continue
			}
			if sql[i] == '\'' {
				if i+1 < len(sql) && sql[i+1] == '\'' {
					i += 2
					continue
				}
				i++
				break
			}
			i++
		}
	}
	return b.String()
}
