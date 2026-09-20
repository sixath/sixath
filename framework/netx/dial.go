package netx

import (
	"context"
	"fmt"
	"net"
	"strconv"

	"golang.org/x/net/proxy"
)

// DialContext returns a SOCKS5 Context dialer for MySQL and HTTP Transport.
// Constructor failures surface on use so HTTPClient always returns a client.
func DialContext(spec Spec) func(ctx context.Context, network, addr string) (net.Conn, error) {
	return func(ctx context.Context, network, addr string) (net.Conn, error) {
		d, err := socks5Dialer(spec)
		if err != nil {
			return nil, err
		}
		if cd, ok := d.(proxy.ContextDialer); ok {
			return cd.DialContext(ctx, network, addr)
		}
		return d.Dial(network, addr)
	}
}

func socks5Dialer(spec Spec) (proxy.Dialer, error) {
	if specType(spec) != TypeSOCKS5 {
		return nil, fmt.Errorf("DialContext requires type %q", TypeSOCKS5)
	}
	addr := net.JoinHostPort(spec.Host, strconv.Itoa(spec.Port))
	var auth *proxy.Auth
	if spec.User != "" || spec.Password != "" {
		auth = &proxy.Auth{User: spec.User, Password: spec.Password}
	}
	return proxy.SOCKS5("tcp", addr, auth, proxy.Direct)
}
