package netx

import (
	"net"
	"strings"
)

// MatchNoProxy reports whether dest host is excluded from the proxy.
// Port is ignored. Unresolvable hosts skip CIDR rules without error.
func MatchNoProxy(host string, rules []string) bool {
	host = destHost(host)
	if host == "" {
		return false
	}
	for _, raw := range rules {
		rule := strings.TrimSpace(raw)
		if rule == "" {
			continue
		}
		if rule == "*" {
			return true
		}
		if matchCIDR(host, rule) {
			return true
		}
		if matchHostRule(host, rule) {
			return true
		}
	}
	return false
}

func destHost(host string) string {
	host = strings.TrimSpace(host)
	if host == "" {
		return ""
	}
	if h, _, err := net.SplitHostPort(host); err == nil {
		return h
	}
	return host
}

func matchHostRule(host, rule string) bool {
	if strings.HasPrefix(rule, ".") {
		return len(host) > len(rule) && strings.EqualFold(host[len(host)-len(rule):], rule)
	}
	return strings.EqualFold(host, rule)
}

func matchCIDR(host, rule string) bool {
	_, network, err := net.ParseCIDR(rule)
	if err != nil {
		return false
	}
	if ip := net.ParseIP(host); ip != nil {
		return network.Contains(ip)
	}
	ips, err := net.LookupIP(host)
	if err != nil {
		return false
	}
	for _, ip := range ips {
		if network.Contains(ip) {
			return true
		}
	}
	return false
}
