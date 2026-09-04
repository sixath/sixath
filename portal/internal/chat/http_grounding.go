package chat

import (
	"encoding/json"
	"net/url"
	"regexp"
	"strings"

	"github.com/sixath/framework/agent"
	"github.com/sixath/framework/model"
)

var (
	httpURLRe      = regexp.MustCompile(`(?i)https?://[^\s"'<>\\]+`)
	httpPortPathRe = regexp.MustCompile(`:[0-9]{2,5}/[A-Za-z0-9._~!$&'()*+,;=:@/\-]+`)
	ipv4Re         = regexp.MustCompile(`\b(?:\d{1,3}\.){3}\d{1,3}\b`)
)

const httpUngroundedRetryPrompt = "不要编造对话、技能或工具结果里没出现过的 HTTP 接口。已有 host:port（或技能里的端口+路径）时，只改 Body/查询参数/文件路径，不要换端口、不要扫 /health、不要另编 /api/... 。用户补充了本地路径时尤其如此。"

type httpAnchorSet struct {
	hosts     map[string]struct{}
	hostPorts map[string]struct{}
	ports     map[string]struct{}
	paths     map[string]struct{}
}

func newHTTPAnchorSet() httpAnchorSet {
	return httpAnchorSet{
		hosts:     map[string]struct{}{},
		hostPorts: map[string]struct{}{},
		ports:     map[string]struct{}{},
		paths:     map[string]struct{}{},
	}
}

func (a httpAnchorSet) active() bool {
	return len(a.ports) > 0 || len(a.hostPorts) > 0 || len(a.paths) > 0
}

func addHTTPAnchorHost(a *httpAnchorSet, host string) {
	host = strings.ToLower(strings.TrimSpace(host))
	if host == "" || strings.ContainsAny(host, "{}<>$") {
		return
	}
	a.hosts[host] = struct{}{}
}

func addHTTPAnchorPort(a *httpAnchorSet, port string) {
	port = strings.TrimSpace(port)
	if port == "" || port == "80" || port == "443" {
		return
	}
	a.ports[port] = struct{}{}
}

func addHTTPAnchorPath(a *httpAnchorSet, path string) {
	path = normalizeHTTPPath(path)
	if path == "" || path == "/" {
		return
	}
	a.paths[path] = struct{}{}
}

func normalizeHTTPPath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	if len(path) > 1 {
		path = strings.TrimRight(path, "/")
	}
	return path
}

func ingestHTTPText(a *httpAnchorSet, text string) {
	if strings.TrimSpace(text) == "" {
		return
	}
	for _, raw := range httpURLRe.FindAllString(text, -1) {
		ingestParsedURL(a, raw)
	}
	for _, m := range httpPortPathRe.FindAllString(text, -1) {
		// ":49997/v1/taskmanager/exec"
		m = strings.TrimPrefix(m, ":")
		i := strings.IndexByte(m, '/')
		if i <= 0 {
			continue
		}
		addHTTPAnchorPort(a, m[:i])
		addHTTPAnchorPath(a, m[i:])
	}
	for _, ip := range ipv4Re.FindAllString(text, -1) {
		addHTTPAnchorHost(a, ip)
	}
}

func ingestParsedURL(a *httpAnchorSet, raw string) {
	raw = strings.TrimRight(raw, ".,);]`\"'")
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return
	}
	host := strings.ToLower(u.Hostname())
	port := u.Port()
	addHTTPAnchorHost(a, host)
	addHTTPAnchorPort(a, port)
	if host != "" && port != "" && !strings.ContainsAny(host, "{}<>$") {
		a.hostPorts[host+":"+port] = struct{}{}
	}
	addHTTPAnchorPath(a, u.Path)
}

func ingestHTTPValue(a *httpAnchorSet, v any) {
	switch t := v.(type) {
	case string:
		ingestHTTPText(a, t)
	default:
		b, err := json.Marshal(t)
		if err != nil {
			return
		}
		ingestHTTPText(a, string(b))
	}
}

func collectHTTPAnchors(req *agent.Request, trace *agent.RunTrace) httpAnchorSet {
	a := newHTTPAnchorSet()
	if req != nil {
		ingestHTTPText(&a, req.SystemPrompt)
		for _, m := range req.Messages {
			ingestHTTPText(&a, m.Content)
		}
	}
	if trace != nil {
		for _, tc := range trace.ToolCalls {
			ingestHTTPValue(&a, tc.Arguments)
			ingestHTTPValue(&a, tc.Result)
			ingestHTTPText(&a, tc.Error)
		}
	}
	return a
}

func httpRequestGrounded(raw string, a httpAnchorSet) bool {
	raw = strings.TrimSpace(raw)
	if raw == "" || !a.active() {
		return true
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return false
	}
	host := strings.ToLower(u.Hostname())
	port := u.Port()
	if port == "" {
		if strings.EqualFold(u.Scheme, "https") {
			port = "443"
		} else {
			port = "80"
		}
	}
	path := normalizeHTTPPath(u.Path)
	if host == "" {
		return false
	}
	hp := host + ":" + port
	if _, ok := a.hostPorts[hp]; ok {
		return true
	}
	_, portKnown := a.ports[port]
	if port == "80" || port == "443" {
		portKnown = true
	}
	return portKnown && pathAllowed(path, a)
}

func pathAllowed(path string, a httpAnchorSet) bool {
	if path == "" || path == "/" {
		return true
	}
	if len(a.paths) == 0 {
		return true
	}
	for p := range a.paths {
		if p == path || strings.HasPrefix(path, p+"/") || strings.HasPrefix(p, path+"/") {
			return true
		}
	}
	return false
}

func httpRequestURL(args map[string]any) string {
	if args == nil {
		return ""
	}
	if s, ok := args["url"].(string); ok {
		return s
	}
	return ""
}

func isOfferedTool(name string, toolFamily map[string]string) bool {
	name = strings.TrimSpace(name)
	if name == "" {
		return false
	}
	if len(toolFamily) == 0 {
		return true
	}
	if _, ok := toolFamily[name]; ok {
		return true
	}
	_, builtin := builtinToolFamily[name]
	return builtin
}

func filterUnofferedTools(calls []model.ToolCall, toolFamily map[string]string) ([]model.ToolCall, int) {
	if len(toolFamily) == 0 {
		return calls, 0
	}
	kept := make([]model.ToolCall, 0, len(calls))
	dropped := 0
	for _, c := range calls {
		if isOfferedTool(c.Name, toolFamily) {
			kept = append(kept, c)
			continue
		}
		dropped++
	}
	return kept, dropped
}

func filterUngroundedHTTP(calls []model.ToolCall, anchors httpAnchorSet) ([]model.ToolCall, int) {
	kept := make([]model.ToolCall, 0, len(calls))
	dropped := 0
	for _, c := range calls {
		if strings.TrimSpace(c.Name) == "http_request" && !httpRequestGrounded(httpRequestURL(c.Arguments), anchors) {
			dropped++
			continue
		}
		kept = append(kept, c)
	}
	return kept, dropped
}
