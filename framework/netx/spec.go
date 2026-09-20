package netx

import (
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
)

const (
	TypeHTTP    = "http"
	TypeSOCKS5  = "socks5"
	ModeInherit = "inherit"
	ModeOff     = "off"
	ModeProxy   = "proxy"
)

type Spec struct {
	ID, Type, Host, User, Password string
	Port                           int
	NoProxy                        []string
}

type Binding struct {
	Mode, ProxyID string
}

// Effective is the outbound tunnel. Nil from Resolve means direct.
type Effective struct {
	Spec  Spec
	Force bool // tool ModeProxy: do not apply NoProxy
}

func NormalizeMode(m string) string {
	switch strings.ToLower(strings.TrimSpace(m)) {
	case "", ModeInherit:
		return ModeInherit
	case ModeOff, "direct":
		return ModeOff
	case ModeProxy:
		return ModeProxy
	default:
		return strings.ToLower(strings.TrimSpace(m))
	}
}

func specType(s Spec) string {
	return strings.ToLower(strings.TrimSpace(s.Type))
}

// Validate reports an illegal proxy type. Empty type is allowed (direct).
func (s Spec) Validate() error {
	switch specType(s) {
	case "", TypeHTTP, TypeSOCKS5:
		return nil
	default:
		return fmt.Errorf("unsupported proxy type %q", s.Type)
	}
}

// HTTPProxyURL builds an HTTP forward-proxy URL. Only type=http.
// Never panics; user:pass is included when either is set.
func (s Spec) HTTPProxyURL() (*url.URL, error) {
	if specType(s) != TypeHTTP {
		return nil, fmt.Errorf("HTTPProxyURL requires type %q", TypeHTTP)
	}
	u := &url.URL{
		Scheme: "http",
		Host:   net.JoinHostPort(s.Host, strconv.Itoa(s.Port)),
	}
	if s.User != "" || s.Password != "" {
		u.User = url.UserPassword(s.User, s.Password)
	}
	return u, nil
}
