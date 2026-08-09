package service

import (
	"fmt"
	"strings"

	"github.com/go-kratos/kratos/v2/log"
)

func logServiceError(logger *log.Helper, op string, err error, kv ...any) {
	if logger == nil || err == nil {
		return
	}
	if len(kv) == 0 {
		logger.Errorf("%s failed: err=%v", op, err)
		return
	}
	logger.Errorf("%s failed: %s err=%v", op, formatLogKV(kv...), err)
}

func formatLogKV(kv ...any) string {
	if len(kv) == 0 {
		return ""
	}
	var b strings.Builder
	for i := 0; i < len(kv); i += 2 {
		if i > 0 {
			b.WriteString(" ")
		}
		key := fmt.Sprint(kv[i])
		var val any = "<missing>"
		if i+1 < len(kv) {
			val = kv[i+1]
		}
		b.WriteString(key)
		b.WriteString("=")
		b.WriteString(fmt.Sprint(val))
	}
	return b.String()
}
