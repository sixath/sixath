package chat

import "strings"

const defaultMemoryFenceTag = "sixath-memory-context"

// MemoryFenceStreamScrubber 在 SSE chunk 上剔除「记忆围栏」内字节（设计 §4.3）；开标签为 <tag id="...">，
// 闭标签为 </tag> 或 </tag id="...">（id 须与开标签一致才闭合）。
type MemoryFenceStreamScrubber struct {
	tag    string
	inside bool
	openID string
	buf    string
}

// NewMemoryFenceStreamScrubber 使用与 memory.Orchestrator 一致的围栏标签；tag 为空则用默认 sixath-memory-context。
func NewMemoryFenceStreamScrubber(tag string) *MemoryFenceStreamScrubber {
	if tag == "" {
		tag = defaultMemoryFenceTag
	}
	return &MemoryFenceStreamScrubber{tag: tag}
}

// Feed 处理一段模型增量，返回可发往 UI / 参与落库拼接的安全子串（围栏内字节不落 SSE）。
func (s *MemoryFenceStreamScrubber) Feed(chunk string) string {
	if s == nil || chunk == "" {
		return ""
	}
	data := s.buf + chunk
	s.buf = ""
	var b strings.Builder
	for len(data) > 0 {
		if !s.inside {
			i := strings.IndexByte(data, '<')
			if i < 0 {
				b.WriteString(data)
				data = ""
				break
			}
			b.WriteString(data[:i])
			data = data[i:]
			n, id, ok := tryConsumeOpenFence(data, s.tag)
			if ok {
				s.inside = true
				s.openID = id
				data = data[n:]
				continue
			}
			if openPrefixPending(data, s.tag) {
				s.buf = data
				data = ""
				break
			}
			b.WriteByte('<')
			data = data[1:]
			continue
		}
		closePref := "</" + s.tag
		idx := strings.Index(data, closePref)
		if idx < 0 {
			keep := withholdCloseSuffix(data, closePref)
			if keep == 0 {
				data = ""
				break
			}
			s.buf = data[len(data)-keep:]
			data = ""
			break
		}
		rem := data[idx:]
		n, ok := tryConsumeCloseFence(rem, s.tag, s.openID)
		if ok {
			s.inside = false
			s.openID = ""
			data = rem[n:]
			continue
		}
		if len(rem) < len(closePref)+96 {
			s.buf = data[idx:]
			data = ""
			break
		}
		data = data[idx+1:]
	}
	return b.String()
}

// Flush 在流结束时调用：若围栏未闭合，丢弃缓冲且 eofTruncated=true；否则刷出 buf 中安全尾部。
func (s *MemoryFenceStreamScrubber) Flush() (tail string, eofTruncated bool) {
	if s == nil {
		return "", false
	}
	if s.inside {
		s.buf = ""
		s.inside = false
		s.openID = ""
		return "", true
	}
	data := s.buf
	s.buf = ""
	if data == "" {
		return "", false
	}
	var b strings.Builder
	for len(data) > 0 {
		i := strings.IndexByte(data, '<')
		if i < 0 {
			b.WriteString(data)
			break
		}
		b.WriteString(data[:i])
		data = data[i:]
		_, _, ok := tryConsumeOpenFence(data, s.tag)
		if ok {
			s.inside = false
			s.openID = ""
			return b.String(), true
		}
		if openPrefixPending(data, s.tag) {
			return b.String(), true
		}
		b.WriteByte('<')
		data = data[1:]
	}
	return b.String(), false
}

func tryConsumeOpenFence(data, tag string) (n int, id string, ok bool) {
	pref := "<" + tag
	if !strings.HasPrefix(data, pref) {
		return 0, "", false
	}
	i := len(pref)
	for i < len(data) && (data[i] == ' ' || data[i] == '\t' || data[i] == '\n' || data[i] == '\r') {
		i++
	}
	if i >= len(data) {
		return 0, "", false
	}
	if !strings.HasPrefix(data[i:], `id="`) {
		return 0, "", false
	}
	i += len(`id="`)
	j := strings.IndexByte(data[i:], '"')
	if j < 0 {
		return 0, "", false
	}
	id = data[i : i+j]
	i += j + 1
	for i < len(data) && (data[i] == ' ' || data[i] == '\t' || data[i] == '\n' || data[i] == '\r') {
		i++
	}
	if i >= len(data) || data[i] != '>' {
		return 0, "", false
	}
	return i + 1, id, true
}

func tryConsumeCloseFence(rem, tag, openID string) (n int, ok bool) {
	pref := "</" + tag
	if !strings.HasPrefix(rem, pref) {
		return 0, false
	}
	i := len(pref)
	for i < len(rem) && (rem[i] == ' ' || rem[i] == '\t' || rem[i] == '\n' || rem[i] == '\r') {
		i++
	}
	if i >= len(rem) {
		return 0, false
	}
	if rem[i] == '>' {
		return i + 1, true
	}
	if strings.HasPrefix(rem[i:], `id="`) {
		i += len(`id="`)
		j := strings.IndexByte(rem[i:], '"')
		if j < 0 {
			return 0, false
		}
		got := rem[i : i+j]
		i += j + 1
		if openID != "" && got != openID {
			return 0, false
		}
		for i < len(rem) && (rem[i] == ' ' || rem[i] == '\t' || rem[i] == '\n' || rem[i] == '\r') {
			i++
		}
		if i < len(rem) && rem[i] == '>' {
			return i + 1, true
		}
		return 0, false
	}
	return 0, false
}

func openPrefixPending(data, tag string) bool {
	if len(data) > 4096 {
		return false
	}
	full := "<" + tag + ` id="`
	if strings.HasPrefix(full, data) {
		return true
	}
	if strings.HasPrefix(data, "<"+tag) {
		_, _, ok := tryConsumeOpenFence(data, tag)
		return !ok
	}
	if strings.HasPrefix("<"+tag, data) && strings.HasPrefix(data, "<") {
		return true
	}
	return false
}

func withholdCloseSuffix(data, closePref string) int {
	n := len(data)
	max := len(closePref) + 96
	if n < max {
		max = n
	}
	for l := max; l >= 1; l-- {
		suf := data[n-l:]
		if strings.HasPrefix(closePref, suf) {
			return l
		}
	}
	return 0
}
