package netx

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestHTTPClient_HTTPProxySendsToProxy(t *testing.T) {
	saw := false
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		saw = true
		if r.Method != http.MethodConnect && r.URL.Host == "" && r.URL.Scheme == "" {
			t.Errorf("proxy got unexpected %s %s", r.Method, r.URL)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer proxy.Close()
	u, _ := url.Parse(proxy.URL)
	port, _ := strconv.Atoi(u.Port())
	c := HTTPClient(Spec{Type: TypeHTTP, Host: u.Hostname(), Port: port}, 5*time.Second)
	req, _ := http.NewRequest(http.MethodGet, "http://target.example/path", nil)
	_, _ = c.Do(req)
	if !saw {
		t.Fatal("request did not hit proxy")
	}
}

func TestHTTPClient_ZeroSpecDirect(t *testing.T) {
	hit := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hit = true
		w.WriteHeader(200)
	}))
	defer srv.Close()
	c := HTTPClient(Spec{}, 5*time.Second)
	resp, err := c.Get(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if !hit {
		t.Fatal("direct miss")
	}
}

func TestDialContext_SOCKS5UsesDialer(t *testing.T) {
	c := HTTPClient(Spec{Type: TypeSOCKS5, Host: "127.0.0.1", Port: 1080}, time.Second)
	tr, ok := c.Transport.(*http.Transport)
	if !ok || tr.DialContext == nil {
		t.Fatal("want socks dial")
	}
	if tr.Proxy != nil {
		u, _ := tr.Proxy(&http.Request{URL: &url.URL{Scheme: "http", Host: "x"}})
		if u != nil {
			t.Fatal("socks must not set HTTP proxy")
		}
	}
}

func TestHTTPClient_HonorNoProxy(t *testing.T) {
	direct := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) }))
	defer direct.Close()
	proxyHit := false
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		proxyHit = true
		w.WriteHeader(200)
	}))
	defer proxy.Close()
	pu, _ := url.Parse(proxy.URL)
	port, _ := strconv.Atoi(pu.Port())
	du, _ := url.Parse(direct.URL)
	c := HTTPClient(Spec{Type: TypeHTTP, Host: pu.Hostname(), Port: port, NoProxy: []string{du.Hostname()}}, 5*time.Second)
	resp, err := c.Get(direct.URL)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if proxyHit {
		t.Fatal("no_proxy must skip proxy")
	}
}

func TestHTTPClientEffective_ForceBypassesNoProxy(t *testing.T) {
	proxyHit := false
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		proxyHit = true
		w.WriteHeader(http.StatusOK)
	}))
	defer proxy.Close()
	pu, _ := url.Parse(proxy.URL)
	port, _ := strconv.Atoi(pu.Port())
	c := HTTPClientEffective(Effective{
		Spec: Spec{
			Type:    TypeHTTP,
			Host:    pu.Hostname(),
			Port:    port,
			NoProxy: []string{"target.example"},
		},
		Force: true,
	}, 5*time.Second)
	req, _ := http.NewRequest(http.MethodGet, "http://target.example/path", nil)
	_, _ = c.Do(req)
	if !proxyHit {
		t.Fatal("force must hit proxy despite no_proxy")
	}
}

func TestHTTPProxyURL_IncludesUserPass(t *testing.T) {
	u, err := Spec{Type: TypeHTTP, Host: "127.0.0.1", Port: 8080, User: "alice", Password: "s3cret"}.HTTPProxyURL()
	if err != nil {
		t.Fatal(err)
	}
	if u == nil {
		t.Fatal("want proxy url")
	}
	if u.Scheme != "http" || u.Hostname() != "127.0.0.1" || u.Port() != "8080" {
		t.Fatalf("url=%s", u)
	}
	if u.User == nil || u.User.Username() != "alice" {
		t.Fatalf("user=%v", u.User)
	}
	pw, ok := u.User.Password()
	if !ok || pw != "s3cret" {
		t.Fatalf("pass=%s ok=%v", pw, ok)
	}
}

func TestHTTPProxyURL_NonHTTP(t *testing.T) {
	u, err := Spec{Type: TypeSOCKS5, Host: "127.0.0.1", Port: 1080}.HTTPProxyURL()
	if err == nil || u != nil {
		t.Fatalf("want error, got %v %v", u, err)
	}
}

func TestSocksDialContext_NoProxyUsesDirect(t *testing.T) {
	var used string
	direct := func(context.Context, string, string) (net.Conn, error) {
		used = "direct"
		return nil, errors.New("direct")
	}
	viaProxy := func(context.Context, string, string) (net.Conn, error) {
		used = "socks"
		return nil, errors.New("socks")
	}
	spec := Spec{Type: TypeSOCKS5, Host: "127.0.0.1", Port: 1080, NoProxy: []string{"es.local"}}
	d := socksDialContext(spec, false, direct, viaProxy)
	_, _ = d(context.Background(), "tcp", "es.local:443")
	if used != "direct" {
		t.Fatalf("used=%s want direct", used)
	}
}

func TestSocksDialContext_ForceUsesProxy(t *testing.T) {
	var used string
	direct := func(context.Context, string, string) (net.Conn, error) {
		used = "direct"
		return nil, errors.New("direct")
	}
	viaProxy := func(context.Context, string, string) (net.Conn, error) {
		used = "socks"
		return nil, errors.New("socks")
	}
	spec := Spec{Type: TypeSOCKS5, Host: "127.0.0.1", Port: 1080, NoProxy: []string{"es.local"}}
	d := socksDialContext(spec, true, direct, viaProxy)
	_, _ = d(context.Background(), "tcp", "es.local:443")
	if used != "socks" {
		t.Fatalf("used=%s want socks", used)
	}
}

func TestSocksDialContext_NonNoProxyUsesProxy(t *testing.T) {
	var used string
	direct := func(context.Context, string, string) (net.Conn, error) {
		used = "direct"
		return nil, errors.New("direct")
	}
	viaProxy := func(context.Context, string, string) (net.Conn, error) {
		used = "socks"
		return nil, errors.New("socks")
	}
	spec := Spec{Type: TypeSOCKS5, Host: "127.0.0.1", Port: 1080, NoProxy: []string{"es.local"}}
	d := socksDialContext(spec, false, direct, viaProxy)
	_, _ = d(context.Background(), "tcp", "gitlab.corp:443")
	if used != "socks" {
		t.Fatalf("used=%s want socks", used)
	}
}

func TestUnavailableHTTPClient_DoesNotDial(t *testing.T) {
	hit := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hit = true
		w.WriteHeader(200)
	}))
	defer srv.Close()
	c := UnavailableHTTPClient("office", time.Second)
	_, err := c.Get(srv.URL)
	if err == nil {
		t.Fatal("want missing-proxy error")
	}
	if hit {
		t.Fatal("unavailable client must not reach dest")
	}
	if !strings.Contains(err.Error(), `proxy "office" not found`) {
		t.Fatalf("err=%v", err)
	}
}

func TestHTTPClient_IllegalTypeDirect(t *testing.T) {
	hit := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hit = true
		w.WriteHeader(200)
	}))
	defer srv.Close()
	c := HTTPClient(Spec{Type: "ftp", Host: "127.0.0.1", Port: 1}, 5*time.Second)
	resp, err := c.Get(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if !hit {
		t.Fatal("illegal type must go direct")
	}
}
