package tool

import (
	"regexp"
	"time"
)

var quoteTimeRe = regexp.MustCompile(`\d{4}-\d{2}-\d{2}[ T]\d{2}:\d{2}(:\d{2}(\.\d+)?)?(Z|[+-]\d{2}:?\d{2})?`)

// QuoteTime 取出文本中第一个完整日期时间（如日志行开头的时间戳）；
// 不带时区的按 UTC 解析，因此只应与同样来自日志原文的时间比较。
func QuoteTime(s string) (time.Time, bool) {
	m := quoteTimeRe.FindString(s)
	if m == "" {
		return time.Time{}, false
	}
	return parseTimelineTime(m)
}
