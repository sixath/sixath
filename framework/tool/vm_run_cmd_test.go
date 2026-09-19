package tool

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestClassifyVMRunCmd(t *testing.T) {
	if got := classifyVMRunCmd(`format C:`); got != vmRunCmdDeny {
		t.Fatalf("format: %v", got)
	}
	if got := classifyVMRunCmd(`Stop-Computer`); got != vmRunCmdDeny {
		t.Fatalf("Stop-Computer: %v", got)
	}
	if got := classifyVMRunCmd(`taskkill /PID 1`); got != vmRunCmdConfirm {
		t.Fatalf("taskkill: %v", got)
	}
	if got := classifyVMRunCmd(`Remove-Item -Recurse C:\`); got != vmRunCmdDeny {
		t.Fatalf("drive root: %v", got)
	}
	if got := classifyVMRunCmd(`Remove-Item D:\tmp\a`); got != vmRunCmdConfirm {
		t.Fatalf("Remove-Item: %v", got)
	}
	if got := classifyVMRunCmd(`Get-Process`); got != vmRunCmdAllow {
		t.Fatalf("Get-Process: %v", got)
	}
	if got := classifyVMRunCmd(`powershell -Command "taskkill /F /IM a.exe"`); got != vmRunCmdConfirm {
		t.Fatalf("wrapped taskkill: %v", got)
	}
}

func TestParseVMRunCmdHost(t *testing.T) {
	if _, err := parseVMRunCmdHost("10.141.12.13"); err != nil {
		t.Fatal(err)
	}
	if _, err := parseVMRunCmdHost("http://10.141.12.13:53000/runCmd"); err == nil {
		t.Fatal("url host must fail")
	}
	if _, err := parseVMRunCmdHost("10.1.1.1/runCmd"); err == nil {
		t.Fatal("path must fail")
	}
}

func TestParseVMRunCmdVMID(t *testing.T) {
	n, err := parseVMRunCmdVMID("199306")
	if err != nil || n != 199306 {
		t.Fatalf("n=%d err=%v", n, err)
	}
	if _, err := parseVMRunCmdVMID("x"); err == nil {
		t.Fatal("want error")
	}
}

func TestVMRunCmd_Description(t *testing.T) {
	reg := &Registry{tools: map[string]Tool{}}
	if err := RegisterVMRunCmd(reg, VMRunCmdConfig{}); err != nil {
		t.Fatal(err)
	}
	tl, ok := reg.Get("vm_run_cmd")
	if !ok {
		t.Fatal("vm_run_cmd not registered")
	}
	for _, phrase := range []string{
		"Windows PowerShell",
		"http_request",
		":53000",
		"output_empty",
		"host",
		"vmid",
	} {
		if !strings.Contains(tl.Description, phrase) {
			t.Fatalf("description missing %q: %q", phrase, tl.Description)
		}
	}
}

func hostPort(t *testing.T, rawURL string) (string, int) {
	t.Helper()
	u, err := url.Parse(rawURL)
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(u.Port())
	if err != nil {
		t.Fatal(err)
	}
	return u.Hostname(), port
}

func TestVMRunCmd_HostPostsJSON(t *testing.T) {
	var gotBody string
	var gotPath string
	var gotCT string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotCT = r.Header.Get("Content-Type")
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(200)
		_, _ = w.Write([]byte("hello"))
	}))
	t.Cleanup(srv.Close)
	host, port := hostPort(t, srv.URL)
	reg := NewRegistry()
	if err := RegisterVMRunCmd(reg, VMRunCmdConfig{HTTPClient: srv.Client()}); err != nil {
		t.Fatal(err)
	}
	tl, _ := reg.Get("vm_run_cmd")
	out, err := tl.Execute(context.Background(), map[string]any{
		"host": host, "port": port, "cmd": "Get-Process",
	})
	if err != nil {
		t.Fatal(err)
	}
	m := out.(map[string]any)
	if m["ok"] != true || m["stdout"] != "hello" || m["output_empty"] == true {
		t.Fatalf("%#v", m)
	}
	if m["port"] != port {
		t.Fatalf("port=%#v want %d", m["port"], port)
	}
	if gotPath != "/runCmd" || gotBody != `{"cmd":"Get-Process"}` || gotCT != "application/json" {
		t.Fatalf("path=%s body=%s ct=%s", gotPath, gotBody, gotCT)
	}
}

func TestVMRunCmd_Empty200(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}))
	t.Cleanup(srv.Close)
	host, port := hostPort(t, srv.URL)
	reg := NewRegistry()
	if err := RegisterVMRunCmd(reg, VMRunCmdConfig{HTTPClient: srv.Client()}); err != nil {
		t.Fatal(err)
	}
	tl, _ := reg.Get("vm_run_cmd")
	out, err := tl.Execute(context.Background(), map[string]any{
		"host": host, "port": port, "cmd": "Get-Process",
	})
	if err != nil {
		t.Fatal(err)
	}
	m := out.(map[string]any)
	if m["ok"] != true || m["output_empty"] != true {
		t.Fatalf("%#v", m)
	}
	if stdout, _ := m["stdout"].(string); stdout != "" {
		t.Fatalf("stdout=%q", stdout)
	}
	if errMsg, ok := m["error"]; ok && errMsg != nil && errMsg != "" {
		t.Fatalf("error should be empty/absent: %#v", m)
	}
}

func TestVMRunCmd_RejectsURLParam(t *testing.T) {
	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
	}))
	t.Cleanup(srv.Close)
	host, port := hostPort(t, srv.URL)
	reg := NewRegistry()
	if err := RegisterVMRunCmd(reg, VMRunCmdConfig{HTTPClient: srv.Client()}); err != nil {
		t.Fatal(err)
	}
	tl, _ := reg.Get("vm_run_cmd")
	out, err := tl.Execute(context.Background(), map[string]any{
		"host": host, "port": port, "cmd": "Get-Process", "url": "http://evil.example/runCmd",
	})
	if err != nil {
		t.Fatal(err)
	}
	m := out.(map[string]any)
	if m["ok"] != false || m["error_code"] != ErrorPermanent {
		t.Fatalf("%#v", m)
	}
	if hits != 0 {
		t.Fatalf("handler hit count=%d, want 0", hits)
	}
}

func TestVMRunCmd_HTTPStatusCodes(t *testing.T) {
	t.Run("404_permanent", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(404)
			_, _ = w.Write([]byte("not found"))
		}))
		t.Cleanup(srv.Close)
		host, port := hostPort(t, srv.URL)
		reg := NewRegistry()
		if err := RegisterVMRunCmd(reg, VMRunCmdConfig{HTTPClient: srv.Client()}); err != nil {
			t.Fatal(err)
		}
		tl, _ := reg.Get("vm_run_cmd")
		out, err := tl.Execute(context.Background(), map[string]any{
			"host": host, "port": port, "cmd": "Get-Process",
		})
		if err != nil {
			t.Fatal(err)
		}
		m := out.(map[string]any)
		if m["ok"] != false || m["error_code"] != ErrorPermanent {
			t.Fatalf("%#v", m)
		}
		if m["http_status"] != 404 {
			t.Fatalf("http_status=%#v", m["http_status"])
		}
	})
	t.Run("503_transient", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(503)
			_, _ = w.Write([]byte("unavailable"))
		}))
		t.Cleanup(srv.Close)
		host, port := hostPort(t, srv.URL)
		reg := NewRegistry()
		if err := RegisterVMRunCmd(reg, VMRunCmdConfig{HTTPClient: srv.Client()}); err != nil {
			t.Fatal(err)
		}
		tl, _ := reg.Get("vm_run_cmd")
		out, err := tl.Execute(context.Background(), map[string]any{
			"host": host, "port": port, "cmd": "Get-Process",
		})
		if err != nil {
			t.Fatal(err)
		}
		m := out.(map[string]any)
		if m["ok"] != false || m["error_code"] != ErrorTransient {
			t.Fatalf("%#v", m)
		}
		if m["http_status"] != 503 {
			t.Fatalf("http_status=%#v", m["http_status"])
		}
	})
}

func TestVMRunCmd_TimeoutTransient(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(3 * time.Second):
		}
	}))
	t.Cleanup(srv.Close)
	host, port := hostPort(t, srv.URL)
	reg := NewRegistry()
	if err := RegisterVMRunCmd(reg, VMRunCmdConfig{HTTPClient: srv.Client()}); err != nil {
		t.Fatal(err)
	}
	tl, _ := reg.Get("vm_run_cmd")
	out, err := tl.Execute(context.Background(), map[string]any{
		"host": host, "port": port, "cmd": "Get-Process", "timeout_sec": 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	m := out.(map[string]any)
	if m["ok"] == true || m["error_code"] != ErrorTransient {
		t.Fatalf("%#v", m)
	}
}

func TestVMRunCmd_TimeoutSecOverridesInjectedClientTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(3 * time.Second):
		}
	}))
	t.Cleanup(srv.Close)
	host, port := hostPort(t, srv.URL)

	injected := srv.Client()
	injected.Timeout = 60 * time.Second

	cfg := VMRunCmdConfig{
		ClientForHost: func(h string, p int) (*http.Client, error) {
			return injected, nil
		},
	}
	selected, err := selectVMRunCmdClient(cfg, host, port, 1)
	if err != nil {
		t.Fatal(err)
	}
	if selected == nil {
		t.Fatal("selectVMRunCmdClient returned nil")
	}
	if selected.Timeout != time.Second {
		t.Fatalf("selected Timeout=%v want 1s (timeout_sec ignored)", selected.Timeout)
	}
	if injected.Timeout != 60*time.Second {
		t.Fatalf("injected client was mutated: Timeout=%v", injected.Timeout)
	}

	reg := NewRegistry()
	if err := RegisterVMRunCmd(reg, cfg); err != nil {
		t.Fatal(err)
	}
	tl, _ := reg.Get("vm_run_cmd")
	out, err := tl.Execute(context.Background(), map[string]any{
		"host": host, "port": port, "cmd": "Get-Process", "timeout_sec": 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	m := out.(map[string]any)
	if m["ok"] == true || m["error_code"] != ErrorTransient {
		t.Fatalf("%#v", m)
	}
	if injected.Timeout != 60*time.Second {
		t.Fatalf("injected client was mutated after Execute: Timeout=%v", injected.Timeout)
	}
}

func TestVMRunCmd_UsesClientForHost(t *testing.T) {
	var gotHost string
	var gotPort int
	called := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		_, _ = w.Write([]byte("via-client-for-host"))
	}))
	t.Cleanup(srv.Close)
	host, port := hostPort(t, srv.URL)
	reg := NewRegistry()
	if err := RegisterVMRunCmd(reg, VMRunCmdConfig{
		ClientForHost: func(h string, p int) (*http.Client, error) {
			called++
			gotHost = h
			gotPort = p
			return srv.Client(), nil
		},
	}); err != nil {
		t.Fatal(err)
	}
	tl, _ := reg.Get("vm_run_cmd")
	out, err := tl.Execute(context.Background(), map[string]any{
		"host": host, "port": port, "cmd": "Get-Process",
	})
	if err != nil {
		t.Fatal(err)
	}
	m := out.(map[string]any)
	if m["ok"] != true || m["stdout"] != "via-client-for-host" {
		t.Fatalf("%#v", m)
	}
	if called != 1 || gotHost != host || gotPort != port {
		t.Fatalf("ClientForHost called=%d host=%s port=%d want host=%s port=%d", called, gotHost, gotPort, host, port)
	}
}

func TestVMRunCmd_HostWinsOverVMID(t *testing.T) {
	lookupCalls := 0
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.WriteHeader(200)
		_, _ = w.Write([]byte("from-host"))
	}))
	t.Cleanup(srv.Close)
	host, port := hostPort(t, srv.URL)
	reg := NewRegistry()
	if err := RegisterVMRunCmd(reg, VMRunCmdConfig{
		HTTPClient: srv.Client(),
		Lookup: func(ctx context.Context, datasourceID string, vmid int64) (string, bool, error) {
			lookupCalls++
			t.Fatal("Lookup must not be called when host is set")
			return "", false, nil
		},
	}); err != nil {
		t.Fatal(err)
	}
	tl, _ := reg.Get("vm_run_cmd")
	out, err := tl.Execute(context.Background(), map[string]any{
		"host": host, "port": port, "cmd": "Get-Process", "vmid": 199306,
	})
	if err != nil {
		t.Fatal(err)
	}
	m := out.(map[string]any)
	if m["ok"] != true || m["stdout"] != "from-host" {
		t.Fatalf("%#v", m)
	}
	if lookupCalls != 0 {
		t.Fatalf("Lookup called %d times", lookupCalls)
	}
	if gotPath != "/runCmd" {
		t.Fatalf("path=%s", gotPath)
	}
	switch v := m["vmid"].(type) {
	case int:
		if v != 199306 {
			t.Fatalf("vmid=%d", v)
		}
	case int64:
		if v != 199306 {
			t.Fatalf("vmid=%d", v)
		}
	case float64:
		if v != 199306 {
			t.Fatalf("vmid=%v", v)
		}
	default:
		t.Fatalf("vmid type %T value %#v", m["vmid"], m["vmid"])
	}
}

func TestVMRunCmd_TaskkillPendingThenConfirm(t *testing.T) {
	var hits int
	var gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.WriteHeader(200)
	}))
	t.Cleanup(srv.Close)
	host, port := hostPort(t, srv.URL)
	store := NewInMemoryVMRunCmdPendingStore()
	reg := NewRegistry()
	_ = RegisterVMRunCmd(reg, VMRunCmdConfig{
		HTTPClient:   srv.Client(),
		PendingStore: store,
		TokenGen:     &fakeTokenGen{next: "tok-vm"},
	})
	tl, _ := reg.Get("vm_run_cmd")
	ctx := context.WithValue(context.Background(), ContextKeySessionID, "sess-1")
	out, err := tl.Execute(ctx, map[string]any{"host": host, "port": port, "cmd": "taskkill /PID 1"})
	if err != nil {
		t.Fatal(err)
	}
	m := out.(map[string]any)
	if m["status"] != "pending" || m["token"] != "tok-vm" || m["command"] != "taskkill /PID 1" {
		t.Fatalf("%#v", m)
	}
	if m["expires_in"] != 300 {
		t.Fatalf("expires_in=%#v", m["expires_in"])
	}
	if hits != 0 {
		t.Fatal("must not POST before confirm")
	}
	out2, err := tl.Execute(ctx, map[string]any{
		"host": "1.2.3.4", "cmd": "taskkill /PID 999", "confirm_token": "tok-vm",
	})
	if err != nil {
		t.Fatal(err)
	}
	m2 := out2.(map[string]any)
	if m2["ok"] != true || hits != 1 {
		t.Fatalf("hits=%d out=%#v", hits, m2)
	}
	if gotBody != `{"cmd":"taskkill /PID 1"}` {
		t.Fatalf("POST body=%s want original pending command", gotBody)
	}
}

func TestVMRunCmd_UnconfiguredDanger(t *testing.T) {
	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
	}))
	t.Cleanup(srv.Close)
	host, port := hostPort(t, srv.URL)
	reg := NewRegistry()
	_ = RegisterVMRunCmd(reg, VMRunCmdConfig{HTTPClient: srv.Client()})
	tl, _ := reg.Get("vm_run_cmd")
	out, err := tl.Execute(context.Background(), map[string]any{
		"host": host, "port": port, "cmd": "taskkill /PID 1",
	})
	if err != nil {
		t.Fatal(err)
	}
	m := out.(map[string]any)
	errMsg, _ := m["error"].(string)
	if !strings.Contains(errMsg, "confirm_required_but_unconfigured") {
		t.Fatalf("%#v", m)
	}
	if hits != 0 {
		t.Fatalf("hits=%d want 0", hits)
	}
}

func TestVMRunCmd_FormatBlocked(t *testing.T) {
	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
	}))
	t.Cleanup(srv.Close)
	host, port := hostPort(t, srv.URL)
	reg := NewRegistry()
	_ = RegisterVMRunCmd(reg, VMRunCmdConfig{HTTPClient: srv.Client()})
	tl, _ := reg.Get("vm_run_cmd")
	out, err := tl.Execute(context.Background(), map[string]any{
		"host": host, "port": port, "cmd": "format C:",
	})
	if err != nil {
		t.Fatal(err)
	}
	m := out.(map[string]any)
	if m["error_code"] != ErrorPermanent {
		t.Fatalf("%#v", m)
	}
	errMsg, _ := m["error"].(string)
	if !strings.Contains(errMsg, "blocked_by_policy") {
		t.Fatalf("%#v", m)
	}
	if hits != 0 {
		t.Fatalf("hits=%d want 0", hits)
	}
}

func TestVMRunCmd_BadToken(t *testing.T) {
	store := NewInMemoryVMRunCmdPendingStore()
	reg := NewRegistry()
	_ = RegisterVMRunCmd(reg, VMRunCmdConfig{
		PendingStore: store,
		TokenGen:     &fakeTokenGen{next: "tok"},
	})
	tl, _ := reg.Get("vm_run_cmd")
	ctx := context.WithValue(context.Background(), ContextKeySessionID, "sess-1")
	out, err := tl.Execute(ctx, map[string]any{"confirm_token": "nope"})
	if err != nil {
		t.Fatal(err)
	}
	want := ConfirmTokenError("not_found")
	if !reflect.DeepEqual(out, want) {
		t.Fatalf("got %#v want %#v", out, want)
	}
}

func TestVMRunCmd_VmidDangerConfirm(t *testing.T) {
	var hits int
	var gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.WriteHeader(200)
	}))
	t.Cleanup(srv.Close)
	host, port := hostPort(t, srv.URL)
	lookupCalls := 0
	store := NewInMemoryVMRunCmdPendingStore()
	reg := NewRegistry()
	_ = RegisterVMRunCmd(reg, VMRunCmdConfig{
		HTTPClient:   srv.Client(),
		PendingStore: store,
		TokenGen:     &fakeTokenGen{next: "tok-vmid"},
		MySQLIDs:     []string{"ds1"},
		Lookup: func(ctx context.Context, datasourceID string, vmid int64) (string, bool, error) {
			lookupCalls++
			if datasourceID != "ds1" {
				t.Fatalf("Lookup ds=%q want ds1", datasourceID)
			}
			return host, false, nil
		},
	})
	tl, _ := reg.Get("vm_run_cmd")
	ctx := context.WithValue(context.Background(), ContextKeySessionID, "sess-vmid")
	out, err := tl.Execute(ctx, map[string]any{"vmid": 199306, "port": port, "cmd": "taskkill /PID 1"})
	if err != nil {
		t.Fatal(err)
	}
	m := out.(map[string]any)
	if m["status"] != "pending" || m["token"] != "tok-vmid" || m["command"] != "taskkill /PID 1" {
		t.Fatalf("%#v", m)
	}
	if lookupCalls != 0 {
		t.Fatalf("Lookup calls=%d want 0", lookupCalls)
	}
	if hits != 0 {
		t.Fatal("must not POST before confirm")
	}
	out2, err := tl.Execute(ctx, map[string]any{"confirm_token": "tok-vmid"})
	if err != nil {
		t.Fatal(err)
	}
	m2 := out2.(map[string]any)
	if m2["ok"] != true || hits != 1 || lookupCalls != 1 {
		t.Fatalf("hits=%d lookup=%d out=%#v", hits, lookupCalls, m2)
	}
	if gotBody != `{"cmd":"taskkill /PID 1"}` {
		t.Fatalf("POST body=%s want original pending command", gotBody)
	}
}

func TestVMRunCmd_ConfirmKeepsTokenOnLookupFail(t *testing.T) {
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		w.WriteHeader(200)
	}))
	t.Cleanup(srv.Close)
	host, port := hostPort(t, srv.URL)
	lookupCalls := 0
	store := NewInMemoryVMRunCmdPendingStore()
	reg := NewRegistry()
	_ = RegisterVMRunCmd(reg, VMRunCmdConfig{
		HTTPClient:   srv.Client(),
		PendingStore: store,
		TokenGen:     &fakeTokenGen{next: "tok-retry"},
		MySQLIDs:     []string{"ds1"},
		Lookup: func(ctx context.Context, datasourceID string, vmid int64) (string, bool, error) {
			lookupCalls++
			if datasourceID != "ds1" {
				t.Fatalf("Lookup ds=%q want ds1", datasourceID)
			}
			if lookupCalls == 1 {
				return "", false, errors.New("lookup down")
			}
			return host, false, nil
		},
	})
	tl, _ := reg.Get("vm_run_cmd")
	ctx := context.WithValue(context.Background(), ContextKeySessionID, "sess-retry")
	out, err := tl.Execute(ctx, map[string]any{"vmid": 199306, "port": port, "cmd": "taskkill /PID 1"})
	if err != nil {
		t.Fatal(err)
	}
	m := out.(map[string]any)
	if m["status"] != "pending" || m["token"] != "tok-retry" {
		t.Fatalf("propose: %#v", m)
	}

	out1, err := tl.Execute(ctx, map[string]any{"confirm_token": "tok-retry"})
	if err != nil {
		t.Fatal(err)
	}
	m1 := out1.(map[string]any)
	if m1["ok"] == true {
		t.Fatalf("first confirm should fail: %#v", m1)
	}
	if hits != 0 {
		t.Fatalf("hits=%d want 0", hits)
	}
	if lookupCalls != 1 {
		t.Fatalf("lookupCalls=%d want 1", lookupCalls)
	}

	out2, err := tl.Execute(ctx, map[string]any{"confirm_token": "tok-retry"})
	if err != nil {
		t.Fatal(err)
	}
	m2 := out2.(map[string]any)
	if m2["ok"] != true || hits != 1 || lookupCalls != 2 {
		t.Fatalf("second confirm: hits=%d lookup=%d out=%#v", hits, lookupCalls, m2)
	}
	pending, _ := store.GetPending(ctx, "sess-retry", "tok-retry")
	if pending != nil {
		t.Fatal("token should be deleted after successful confirm")
	}
}

func TestVMRunCmd_VmidLookupPostsToReturnedHost(t *testing.T) {
	var gotHost string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHost = r.Host
		w.WriteHeader(200)
		_, _ = w.Write([]byte("from-lookup"))
	}))
	t.Cleanup(srv.Close)
	host, port := hostPort(t, srv.URL)
	lookupCalls := 0
	var lookupDS string
	var lookupVMID int64
	reg := NewRegistry()
	if err := RegisterVMRunCmd(reg, VMRunCmdConfig{
		HTTPClient: srv.Client(),
		MySQLIDs:   []string{"ds1"},
		Lookup: func(ctx context.Context, datasourceID string, vmid int64) (string, bool, error) {
			lookupCalls++
			lookupDS = datasourceID
			lookupVMID = vmid
			return host, false, nil
		},
	}); err != nil {
		t.Fatal(err)
	}
	tl, _ := reg.Get("vm_run_cmd")
	out, err := tl.Execute(context.Background(), map[string]any{
		"vmid": 199306, "port": port, "cmd": "Get-Process",
	})
	if err != nil {
		t.Fatal(err)
	}
	m := out.(map[string]any)
	if m["ok"] != true || m["stdout"] != "from-lookup" {
		t.Fatalf("%#v", m)
	}
	if lookupCalls != 1 || lookupDS != "ds1" || lookupVMID != 199306 {
		t.Fatalf("lookup calls=%d ds=%s vmid=%d", lookupCalls, lookupDS, lookupVMID)
	}
	u, err := url.Parse("http://" + gotHost)
	if err != nil {
		t.Fatalf("parse Host %q: %v", gotHost, err)
	}
	if u.Hostname() != host {
		t.Fatalf("POST host=%q want %q (raw Host=%q)", u.Hostname(), host, gotHost)
	}
}

func TestVMRunCmd_LookupZeroRowsNoPOST(t *testing.T) {
	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
	}))
	t.Cleanup(srv.Close)
	_, port := hostPort(t, srv.URL)
	reg := NewRegistry()
	_ = RegisterVMRunCmd(reg, VMRunCmdConfig{
		HTTPClient: srv.Client(),
		MySQLIDs:   []string{"ds1"},
		Lookup: func(ctx context.Context, datasourceID string, vmid int64) (string, bool, error) {
			return "", false, errors.New("no ip for vmid")
		},
	})
	tl, _ := reg.Get("vm_run_cmd")
	out, err := tl.Execute(context.Background(), map[string]any{
		"vmid": 199306, "port": port, "cmd": "Get-Process",
	})
	if err != nil {
		t.Fatal(err)
	}
	m := out.(map[string]any)
	if m["ok"] != false || m["error_code"] != ErrorPermanent {
		t.Fatalf("%#v", m)
	}
	if hits != 0 {
		t.Fatalf("hits=%d want 0", hits)
	}
}

func TestVMRunCmd_TwoMySQLIDsNoPreferred(t *testing.T) {
	hits := 0
	lookupCalls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
	}))
	t.Cleanup(srv.Close)
	_, port := hostPort(t, srv.URL)
	reg := NewRegistry()
	_ = RegisterVMRunCmd(reg, VMRunCmdConfig{
		HTTPClient: srv.Client(),
		MySQLIDs:   []string{"ds1", "ds2"},
		Lookup: func(ctx context.Context, datasourceID string, vmid int64) (string, bool, error) {
			lookupCalls++
			return "10.0.0.1", false, nil
		},
	})
	tl, _ := reg.Get("vm_run_cmd")
	out, err := tl.Execute(context.Background(), map[string]any{
		"vmid": 199306, "port": port, "cmd": "Get-Process",
	})
	if err != nil {
		t.Fatal(err)
	}
	m := out.(map[string]any)
	if m["ok"] != false || m["error_code"] != ErrorPermanent {
		t.Fatalf("%#v", m)
	}
	if lookupCalls != 0 {
		t.Fatalf("Lookup calls=%d want 0", lookupCalls)
	}
	if hits != 0 {
		t.Fatalf("hits=%d want 0", hits)
	}
}

func TestVMRunCmd_LookupAmbiguous(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		_, _ = w.Write([]byte("ok"))
	}))
	t.Cleanup(srv.Close)
	host, port := hostPort(t, srv.URL)
	reg := NewRegistry()
	_ = RegisterVMRunCmd(reg, VMRunCmdConfig{
		HTTPClient: srv.Client(),
		MySQLIDs:   []string{"ds1"},
		Lookup: func(ctx context.Context, datasourceID string, vmid int64) (string, bool, error) {
			return host, true, nil
		},
	})
	tl, _ := reg.Get("vm_run_cmd")
	out, err := tl.Execute(context.Background(), map[string]any{
		"vmid": 199306, "port": port, "cmd": "Get-Process",
	})
	if err != nil {
		t.Fatal(err)
	}
	m := out.(map[string]any)
	if m["ok"] != true {
		t.Fatalf("%#v", m)
	}
	if m["ip_ambiguous"] != true {
		t.Fatalf("ip_ambiguous=%#v want true; %#v", m["ip_ambiguous"], m)
	}
	if m["evidence_refs"] == nil {
		t.Fatalf("evidence_refs missing: %#v", m)
	}
}

func TestVMRunCmd_LookupDeadlineTransient(t *testing.T) {
	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
	}))
	t.Cleanup(srv.Close)
	_, port := hostPort(t, srv.URL)
	reg := NewRegistry()
	_ = RegisterVMRunCmd(reg, VMRunCmdConfig{
		HTTPClient: srv.Client(),
		MySQLIDs:   []string{"ds1"},
		Lookup: func(ctx context.Context, datasourceID string, vmid int64) (string, bool, error) {
			return "", false, context.DeadlineExceeded
		},
	})
	tl, _ := reg.Get("vm_run_cmd")
	out, err := tl.Execute(context.Background(), map[string]any{
		"vmid": 199306, "port": port, "cmd": "Get-Process",
	})
	if err != nil {
		t.Fatal(err)
	}
	m := out.(map[string]any)
	if m["ok"] != false || m["error_code"] != ErrorTransient {
		t.Fatalf("%#v", m)
	}
	if hits != 0 {
		t.Fatalf("hits=%d want 0", hits)
	}
}
