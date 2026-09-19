package netx

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type dialContextFunc func(ctx context.Context, network, addr string) (net.Conn, error)

// HTTPClient builds an outbound client. Zero or illegal Spec is direct.
func HTTPClient(spec Spec, timeout time.Duration) *http.Client {
	return httpClient(spec, timeout, false)
}

// HTTPClientEffective builds a client from Resolve output. Force skips no_proxy.
func HTTPClientEffective(eff Effective, timeout time.Duration) *http.Client {
	return httpClient(eff.Spec, timeout, eff.Force)
}

// UnavailableHTTPClient always fails RoundTrip/Dial with proxy "<id>" not found.
func UnavailableHTTPClient(id string, timeout time.Duration) *http.Client {
	id = strings.TrimSpace(id)
	return &http.Client{Timeout: timeout, Transport: unavailableTransport{id: id}}
}

type unavailableTransport struct {
	id string
}

func (t unavailableTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, missingProxyErr(t.id)
}

func (t unavailableTransport) DialContext(context.Context, string, string) (net.Conn, error) {
	return nil, missingProxyErr(t.id)
}

func missingProxyErr(id string) error {
	return fmt.Errorf("proxy %q not found", id)
}

func httpClient(spec Spec, timeout time.Duration, force bool) *http.Client {
	tr := cloneDirectTransport()
	switch specType(spec) {
	case TypeHTTP:
		tr.Proxy = func(req *http.Request) (*url.URL, error) {
			if !force && req != nil && req.URL != nil && MatchNoProxy(req.URL.Hostname(), spec.NoProxy) {
				return nil, nil
			}
			return spec.HTTPProxyURL()
		}
	case TypeSOCKS5:
		tr.Proxy = nil
		tr.DialContext = socksDialContext(spec, force, tr.DialContext, DialContext(spec))
	}
	return &http.Client{Timeout: timeout, Transport: tr}
}

func socksDialContext(spec Spec, force bool, direct, viaProxy dialContextFunc) dialContextFunc {
	if direct == nil {
		direct = (&net.Dialer{Timeout: 5 * time.Second}).DialContext
	}
	if viaProxy == nil {
		viaProxy = DialContext(spec)
	}
	return func(ctx context.Context, network, addr string) (net.Conn, error) {
		host, _, err := net.SplitHostPort(addr)
		if err != nil {
			host = addr
		}
		if !force && MatchNoProxy(host, spec.NoProxy) {
			return direct(ctx, network, addr)
		}
		return viaProxy(ctx, network, addr)
	}
}

func cloneDirectTransport() *http.Transport {
	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.DialContext = (&net.Dialer{Timeout: 5 * time.Second}).DialContext
	return tr
}
