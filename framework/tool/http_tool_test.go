package tool

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"
	"time"

	"github.com/sixath/framework/netx"
)

func TestHTTPRequest_UsesRegistryHTTPClient(t *testing.T) {
	saw := false
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		saw = true
		if r.Method != http.MethodConnect && r.URL.Host == "" && r.URL.Scheme == "" {
			t.Errorf("proxy got unexpected %s %s", r.Method, r.URL)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	defer proxy.Close()
	u, _ := url.Parse(proxy.URL)
	port, _ := strconv.Atoi(u.Port())
	spec := netx.Spec{ID: "office", Type: netx.TypeHTTP, Host: u.Hostname(), Port: port}

	reg := NewRegistry()
	reg.SetHTTPClient(netx.HTTPClient(spec, 5*time.Second), spec)

	tl, ok := reg.Get("http_request")
	if !ok {
		t.Fatal("missing http_request")
	}
	_, err := tl.Execute(context.Background(), map[string]any{
		"method": http.MethodGet,
		"url":    "http://target.example/path",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !saw {
		t.Fatal("request did not hit proxy")
	}
}
