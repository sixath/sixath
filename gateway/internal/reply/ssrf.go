package reply

import (
	"fmt"
	"net"
	"net/url"
	"os"
	"strings"
)

const envAllowLoopbackReply = "GATEWAY_ALLOW_LOOPBACK_REPLY"

// ValidateReplyURL rejects non-http(s) URLs and hosts that resolve to
// private, link-local, or loopback addresses (basic SSRF guard).
// Set GATEWAY_ALLOW_LOOPBACK_REPLY=1 to permit loopback (e.g. httptest in tests).
func ValidateReplyURL(raw string) error {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("invalid reply_url: %w", err)
	}
	scheme := strings.ToLower(u.Scheme)
	if scheme != "http" && scheme != "https" {
		return fmt.Errorf("reply_url scheme must be http or https")
	}
	host := u.Hostname()
	if host == "" {
		return fmt.Errorf("reply_url host is required")
	}

	allowLoopback := os.Getenv(envAllowLoopbackReply) == "1"

	if ip := net.ParseIP(host); ip != nil {
		if blockedIP(ip, allowLoopback) {
			return fmt.Errorf("reply_url host is not allowed")
		}
		return nil
	}

	ips, err := net.LookupIP(host)
	if err != nil {
		return fmt.Errorf("reply_url host lookup failed: %w", err)
	}
	if len(ips) == 0 {
		return fmt.Errorf("reply_url host has no addresses")
	}
	for _, ip := range ips {
		if blockedIP(ip, allowLoopback) {
			return fmt.Errorf("reply_url host is not allowed")
		}
	}
	return nil
}

func blockedIP(ip net.IP, allowLoopback bool) bool {
	if ip == nil {
		return true
	}
	if ip.IsLoopback() {
		return !allowLoopback
	}
	if ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
		ip.IsUnspecified() || ip.IsMulticast() {
		return true
	}
	return false
}
