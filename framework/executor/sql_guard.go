package executor

import (
	"errors"
	"strings"
	"unicode"
)

// ErrUnsupportedSyntax 表示 SQL 含多语句或其它不支持的语法。
var ErrUnsupportedSyntax = errors.New("executor: unsupported SQL syntax")

var writeStarters = map[string]struct{}{
	"INSERT": {}, "UPDATE": {}, "DELETE": {}, "REPLACE": {},
	"CREATE": {}, "DROP": {}, "ALTER": {}, "TRUNCATE": {}, "RENAME": {},
	"CALL": {}, "MERGE": {}, "HANDLER": {}, "GRANT": {}, "LOAD": {}, "LOCK": {},
}

var readStarters = map[string]struct{}{
	"SELECT": {}, "SHOW": {}, "DESCRIBE": {}, "DESC": {}, "EXPLAIN": {},
}

// prepareSQL 剥离注释并拒绝多语句；返回可用于写判定的 SQL 文本。
func prepareSQL(dsl string) (string, error) {
	cleaned, err := stripSQLComments(dsl)
	if err != nil {
		return "", err
	}
	cleaned = strings.TrimSpace(cleaned)
	if cleaned == "" {
		return "", nil
	}
	if err := rejectMultiStatement(cleaned); err != nil {
		return "", err
	}
	return cleaned, nil
}

// isWriteDSL 判断 DSL 是否为写操作（注释剥离、多语句检测之后）。
func isWriteDSL(dsl string) bool {
	cleaned, err := prepareSQL(dsl)
	if err != nil {
		return true
	}
	if cleaned == "" {
		return false
	}
	return isWriteSQL(cleaned)
}

func isWriteSQL(cleaned string) bool {
	kw := firstKeyword(cleaned)
	if kw == "" {
		return false
	}
	if _, ok := readStarters[kw]; ok {
		return false
	}
	if kw == "WITH" {
		return containsWriteVerbOutsideStrings(cleaned)
	}
	if _, ok := writeStarters[kw]; ok {
		return true
	}
	return containsWriteVerbOutsideStrings(cleaned)
}

func firstKeyword(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	i := 0
	for i < len(s) && unicode.IsSpace(rune(s[i])) {
		i++
	}
	start := i
	for i < len(s) {
		r := rune(s[i])
		if !unicode.IsLetter(r) && r != '_' {
			break
		}
		i++
	}
	if start == i {
		return ""
	}
	return strings.ToUpper(s[start:i])
}

func stripSQLComments(s string) (string, error) {
	var b strings.Builder
	b.Grow(len(s))
	i := 0
	for i < len(s) {
		switch {
		case s[i] == '\'':
			j := i + 1
			for j < len(s) {
				if s[j] == '\\' && j+1 < len(s) {
					j += 2
					continue
				}
				if s[j] == '\'' {
					if j+1 < len(s) && s[j+1] == '\'' {
						j += 2
						continue
					}
					j++
					break
				}
				j++
			}
			b.WriteString(s[i:j])
			i = j
		case s[i] == '"' || s[i] == '`':
			quote := s[i]
			j := i + 1
			for j < len(s) && s[j] != quote {
				if s[j] == '\\' && j+1 < len(s) {
					j += 2
					continue
				}
				j++
			}
			if j < len(s) {
				j++
			}
			b.WriteString(s[i:j])
			i = j
		case i+1 < len(s) && s[i] == '-' && s[i+1] == '-':
			for i < len(s) && s[i] != '\n' {
				i++
			}
		case i+1 < len(s) && s[i] == '/' && s[i+1] == '*':
			i += 2
			for i+1 < len(s) && !(s[i] == '*' && s[i+1] == '/') {
				i++
			}
			if i+1 < len(s) {
				i += 2
			}
		default:
			b.WriteByte(s[i])
			i++
		}
	}
	return b.String(), nil
}

func rejectMultiStatement(s string) error {
	inSingle, inDouble, inBacktick := false, false, false
	for i := 0; i < len(s); i++ {
		switch {
		case inSingle:
			if s[i] == '\\' && i+1 < len(s) {
				i++
				continue
			}
			if s[i] == '\'' {
				if i+1 < len(s) && s[i+1] == '\'' {
					i++
					continue
				}
				inSingle = false
			}
		case inDouble:
			if s[i] == '\\' && i+1 < len(s) {
				i++
				continue
			}
			if s[i] == '"' {
				inDouble = false
			}
		case inBacktick:
			if s[i] == '`' {
				inBacktick = false
			}
		case s[i] == '\'':
			inSingle = true
		case s[i] == '"':
			inDouble = true
		case s[i] == '`':
			inBacktick = true
		case s[i] == ';':
			rest := strings.TrimSpace(s[i+1:])
			if rest != "" {
				return ErrUnsupportedSyntax
			}
			return nil
		}
	}
	return nil
}

func containsWriteVerbOutsideStrings(s string) bool {
	upper := strings.ToUpper(s)
	inSingle, inDouble, inBacktick := false, false, false
	for i := 0; i < len(upper); i++ {
		switch {
		case inSingle:
			if upper[i] == '\'' {
				if i+1 < len(upper) && upper[i+1] == '\'' {
					i++
					continue
				}
				inSingle = false
			}
		case inDouble:
			if upper[i] == '"' {
				inDouble = false
			}
		case inBacktick:
			if upper[i] == '`' {
				inBacktick = false
			}
		case upper[i] == '\'':
			inSingle = true
		case upper[i] == '"':
			inDouble = true
		case upper[i] == '`':
			inBacktick = true
		default:
			if !isWordStart(upper, i) {
				continue
			}
			for verb := range writeStarters {
				if matchWord(upper, i, verb) {
					return true
				}
			}
		}
	}
	return false
}

func isWordStart(s string, i int) bool {
	if i > 0 {
		prev := s[i-1]
		if unicode.IsLetter(rune(prev)) || unicode.IsDigit(rune(prev)) || prev == '_' {
			return false
		}
	}
	return unicode.IsLetter(rune(s[i]))
}

func matchWord(s string, i int, word string) bool {
	if i+len(word) > len(s) {
		return false
	}
	if !strings.EqualFold(s[i:i+len(word)], word) {
		return false
	}
	if i+len(word) < len(s) {
		next := s[i+len(word)]
		if unicode.IsLetter(rune(next)) || unicode.IsDigit(rune(next)) || next == '_' {
			return false
		}
	}
	return true
}
