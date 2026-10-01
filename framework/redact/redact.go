// Package redact 在展示与持久化前遮盖文本或结构化参数中的凭据；不改变实际执行的参数。
package redact

import (
	"regexp"
	"strings"
)

// Mask 替换凭据的占位符。
const Mask = "***"

var (
	// 纯字母的首词视为 scheme（Bearer/Basic/Token…）保留，其余整体视为凭据。
	reAuthHeader = regexp.MustCompile(`(?i)(authorization"?\s*[:=]\s*"?)([A-Za-z]+\s+)?[^\s'",]+`)
	// 键名允许 \w 前缀（access_token、PGPASSWORD）；键后必须紧跟可选引号与分隔符，因此 max_tokens= 不会命中。
	reKeyValue = regexp.MustCompile(`(?i)(\w*(?:password|passwd|pwd|passphrase|secret|token|credentials?|api[_-]?key|secret[_-]?key|access[_-]?key|private[_-]?key))("?)(\s*[=:]\s*)("[^"]*"|'[^']*'|[^\s&'",}\\]+)`)
	reCurlUser = regexp.MustCompile(`((?:^|\s)(?:-u\s+|--user[=\s]+))(['"]?)([^:\s'"]+):([^\s'"]+)`)
	reURLCreds = regexp.MustCompile(`([a-zA-Z][a-zA-Z0-9+.-]*://[^/\s:@]*):([^@\s/]+)@`)
	reSshpass  = regexp.MustCompile(`(sshpass\s+-p\s*)(['"]?)[^\s'"]+`)
)

var secretKeySubstrings = []string{
	"password", "passwd", "pwd", "passphrase", "token", "secret", "api_key", "apikey",
	"authorization", "cookie", "credential", "private_key", "access_key",
}

// SecretKey 判断结构化参数的键名是否表示凭据。以 tokens 结尾的键（max_tokens、input_tokens）是计量字段，不算。
func SecretKey(key string) bool {
	k := strings.ReplaceAll(strings.ToLower(key), "-", "_")
	if strings.HasSuffix(k, "tokens") {
		return false
	}
	for _, s := range secretKeySubstrings {
		if strings.Contains(k, s) {
			return true
		}
	}
	return false
}

// String 遮盖文本中的常见凭据形式：Authorization 头、key=value、curl -u、URL user:pass@、sshpass -p。
func String(s string) string {
	if s == "" {
		return s
	}
	s = reAuthHeader.ReplaceAllString(s, "${1}${2}"+Mask)
	s = reKeyValue.ReplaceAllStringFunc(s, func(m string) string {
		sub := reKeyValue.FindStringSubmatch(m)
		key, v := sub[1]+sub[2]+sub[3], sub[4]
		if strings.HasPrefix(v, `"`) || strings.HasPrefix(v, `'`) {
			return key + v[:1] + Mask + v[:1]
		}
		return key + Mask
	})
	s = reCurlUser.ReplaceAllString(s, "${1}${2}${3}:"+Mask)
	s = reURLCreds.ReplaceAllString(s, "${1}:"+Mask+"@")
	s = reSshpass.ReplaceAllString(s, "${1}${2}"+Mask)
	return s
}

// Value 递归遮盖 map / slice / string；键名为凭据时整值替换为 Mask。返回新值，不修改输入。
func Value(v any) any {
	switch t := v.(type) {
	case string:
		return String(t)
	case map[string]any:
		if t == nil {
			return t
		}
		out := make(map[string]any, len(t))
		for k, x := range t {
			if SecretKey(k) {
				out[k] = Mask
				continue
			}
			out[k] = Value(x)
		}
		return out
	case map[string]string:
		if t == nil {
			return t
		}
		out := make(map[string]string, len(t))
		for k, x := range t {
			if SecretKey(k) {
				out[k] = Mask
				continue
			}
			out[k] = String(x)
		}
		return out
	case []any:
		if t == nil {
			return t
		}
		out := make([]any, len(t))
		for i, x := range t {
			out[i] = Value(x)
		}
		return out
	case []string:
		if t == nil {
			return t
		}
		out := make([]string, len(t))
		for i, x := range t {
			out[i] = String(x)
		}
		return out
	default:
		return v
	}
}
