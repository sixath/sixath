package executor

import (
	"fmt"
	"strings"
	"unicode"
)

// bindNamed 将 :name 占位符替换为 ?，并按出现顺序收集参数值。
// 不替换单引号/双引号字符串字面量内的 :name。
func bindNamed(dsl string, named map[string]any) (string, []any, error) {
	if len(named) == 0 {
		return dsl, nil, nil
	}
	var b strings.Builder
	var args []any
	i := 0
	inSingle := false
	inDouble := false
	for i < len(dsl) {
		c := dsl[i]
		if inSingle {
			b.WriteByte(c)
			if c == '\'' && (i+1 >= len(dsl) || dsl[i+1] != '\'') {
				inSingle = false
			} else if c == '\'' && i+1 < len(dsl) && dsl[i+1] == '\'' {
				b.WriteByte('\'')
				i += 2
				continue
			}
			i++
			continue
		}
		if inDouble {
			b.WriteByte(c)
			if c == '"' && (i == 0 || dsl[i-1] != '\\') {
				inDouble = false
			}
			i++
			continue
		}
		if c == '\'' {
			inSingle = true
			b.WriteByte(c)
			i++
			continue
		}
		if c == '"' {
			inDouble = true
			b.WriteByte(c)
			i++
			continue
		}
		if c == ':' && i+1 < len(dsl) && isIdentChar(rune(dsl[i+1])) {
			j := i + 1
			for j < len(dsl) && isIdentChar(rune(dsl[j])) {
				j++
			}
			name := dsl[i+1 : j]
			v, ok := named[name]
			if !ok {
				return "", nil, fmt.Errorf("executor: missing named param %q", name)
			}
			b.WriteByte('?')
			args = append(args, v)
			i = j
			continue
		}
		b.WriteByte(c)
		i++
	}
	return b.String(), args, nil
}

func isIdentChar(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_'
}

func querySQLWithArgs(cleaned string, opts QueryOptions) (string, []any, error) {
	if len(opts.NamedParams) > 0 {
		return bindNamed(cleaned, opts.NamedParams)
	}
	return cleaned, append([]any(nil), opts.PositionalParams...), nil
}
