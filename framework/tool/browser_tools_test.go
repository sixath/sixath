package tool

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/sixath/framework/tool/browser"
)

func TestRegisterBrowserTools_HealthyFails_ExcludedFromListForAPI(t *testing.T) {
	reg := NewRegistry()
	store := browser.NewSessionStore()
	factory := func() (browser.Backend, error) {
		fb := browser.NewFakeBackend()
		fb.HealthyErr = errors.New("cdp unavailable")
		return fb, nil
	}
	if err := RegisterBrowserTools(reg, store, factory); err != nil {
		t.Fatal(err)
	}

	list := reg.ListForAPI(context.Background(), nil)
	for _, tl := range list {
		if strings.HasPrefix(tl.Name, "browser_") {
			t.Fatalf("expected browser_* excluded when Healthy fails, got %q", tl.Name)
		}
	}
}

func TestRegisterBrowserTools_HealthyOK_NavigateAndClick(t *testing.T) {
	reg := NewRegistry()
	store := browser.NewSessionStore()
	factory := func() (browser.Backend, error) {
		fb := browser.NewFakeBackend()
		fb.PageSnapshot = browser.Snapshot{
			Title: "Example",
			Text:  "hello",
			Refs:  map[string]string{"@e1": "Submit button"},
		}
		return fb, nil
	}
	if err := RegisterBrowserTools(reg, store, factory); err != nil {
		t.Fatal(err)
	}

	list := reg.ListForAPI(context.Background(), nil)
	wantNames := map[string]bool{
		"browser_navigate":   false,
		"browser_snapshot":   false,
		"browser_click":      false,
		"browser_type":       false,
		"browser_scroll":     false,
		"browser_back":       false,
		"browser_press":      false,
		"browser_get_images": false,
		"browser_console":    false,
		"browser_vision":     false,
		"browser_cdp":        false,
		"browser_dialog":     false,
	}
	for _, tl := range list {
		if _, ok := wantNames[tl.Name]; ok {
			wantNames[tl.Name] = true
		}
	}
	for name, found := range wantNames {
		if !found {
			t.Fatalf("ListForAPI missing %q when Healthy ok", name)
		}
	}

	ctx := context.WithValue(context.Background(), ContextKeySessionID, "sess-browser")
	nav, ok := reg.Get("browser_navigate")
	if !ok {
		t.Fatal("missing browser_navigate")
	}
	out, err := nav.Execute(ctx, map[string]any{"url": "https://example.com/"})
	if err != nil {
		t.Fatal(err)
	}
	m := out.(map[string]any)
	if m["ok"] != true {
		t.Fatalf("navigate: %#v", m)
	}
	if m["url"] != "https://example.com/" {
		t.Fatalf("navigate url=%v", m["url"])
	}

	click, ok := reg.Get("browser_click")
	if !ok {
		t.Fatal("missing browser_click")
	}
	out2, err := click.Execute(ctx, map[string]any{"ref": "@e1"})
	if err != nil {
		t.Fatal(err)
	}
	m2 := out2.(map[string]any)
	if m2["ok"] != true {
		t.Fatalf("click: %#v", m2)
	}
	if m2["title"] != "Example" {
		t.Fatalf("click title=%v", m2["title"])
	}
}

func TestRegisterBrowserTools_Navigate_RejectsPrivateIP(t *testing.T) {
	reg := NewRegistry()
	store := browser.NewSessionStore()
	factory := func() (browser.Backend, error) {
		return browser.NewFakeBackend(), nil
	}
	if err := RegisterBrowserTools(reg, store, factory); err != nil {
		t.Fatal(err)
	}

	nav, _ := reg.Get("browser_navigate")
	out, err := nav.Execute(context.Background(), map[string]any{
		"url": "http://127.0.0.1/secret",
	})
	if err != nil {
		t.Fatal(err)
	}
	m := out.(map[string]any)
	if m["ok"] != false {
		t.Fatalf("expected ok=false, got %#v", m)
	}
	if m["error_code"] != ErrorPermanent {
		t.Fatalf("error_code=%v, want permanent", m["error_code"])
	}
	errMsg, _ := m["error"].(string)
	if !strings.Contains(errMsg, "ssrf") && !strings.Contains(errMsg, "blocked") {
		t.Fatalf("expected ssrf error, got %q", errMsg)
	}
}

func TestRegisterBrowserTools_BuiltinToolset(t *testing.T) {
	for _, name := range []string{
		"browser_navigate", "browser_snapshot", "browser_click", "browser_type",
		"browser_scroll", "browser_back", "browser_press", "browser_get_images",
		"browser_console", "browser_vision", "browser_cdp", "browser_dialog", "vision_analyze",
	} {
		if got := builtinDefaultToolset[name]; got != ToolsetBrowser {
			t.Fatalf("toolset[%s] = %q, want %q", name, got, ToolsetBrowser)
		}
	}
}

func TestRegisterBrowserTools_CheckFnDoesNotCreateSession(t *testing.T) {
	reg := NewRegistry()
	store := browser.NewSessionStore()
	var live int
	factory := func() (browser.Backend, error) {
		fb := browser.NewFakeBackend()
		live++
		return fb, nil
	}
	if err := RegisterBrowserTools(reg, store, factory); err != nil {
		t.Fatal(err)
	}

	_ = reg.ListForAPI(context.Background(), nil)
	// Probe backends from CheckFn must be closed; store must stay empty until an op.
	b, err := store.GetOrCreate("probe-check", factory)
	if err != nil {
		t.Fatal(err)
	}
	fb := b.(*browser.FakeBackend)
	if fb.Closed() {
		t.Fatal("session backend should not be closed; CheckFn must use ephemeral probes")
	}
	if live < 5 {
		// 4 CheckFn probes (one per tool in ListForAPI) + 1 GetOrCreate
		t.Fatalf("factory calls=%d, want >=5 (4 CheckFn + 1 GetOrCreate)", live)
	}
}

func TestBrowserTools_ConfirmNavigateClick(t *testing.T) {
	reg := NewRegistry()
	store := browser.NewSessionStore()
	fb := browser.NewFakeBackend()
	fb.PageSnapshot = browser.Snapshot{Title: "T", Text: "body", Refs: map[string]string{"@e1": "Go"}}
	factory := func() (browser.Backend, error) { return fb, nil }
	pending := NewInMemoryBrowserPendingStore()
	if err := RegisterBrowserToolsWithConfig(reg, store, factory, &BrowserToolsConfig{
		PendingStore:    pending,
		TokenGen:        &fakeTokenGen{next: "tok-nav"},
		ConfirmNavigate: true,
	}); err != nil {
		t.Fatal(err)
	}
	ctx := context.WithValue(context.Background(), ContextKeySessionID, "sess-confirm")
	nav, _ := reg.Get("browser_navigate")
	out, err := nav.Execute(ctx, map[string]any{"url": "https://example.com/"})
	if err != nil {
		t.Fatal(err)
	}
	m := out.(map[string]any)
	if m["status"] != "pending" || m["token"] != "tok-nav" {
		t.Fatalf("propose: %#v", m)
	}
	if len(fb.NavigatedURLs) != 0 {
		t.Fatal("must not navigate before confirm")
	}

	out2, err := nav.Execute(ctx, map[string]any{
		"url":           "https://ignored.example/",
		"confirm_token": "tok-nav",
	})
	if err != nil {
		t.Fatal(err)
	}
	if out2.(map[string]any)["ok"] != true {
		t.Fatalf("confirm: %#v", out2)
	}
	if len(fb.NavigatedURLs) != 1 || fb.NavigatedURLs[0] != "https://example.com/" {
		t.Fatalf("navigated=%v", fb.NavigatedURLs)
	}

	// second token for click
	pending2 := NewInMemoryBrowserPendingStore()
	reg2 := NewRegistry()
	fb2 := browser.NewFakeBackend()
	fb2.PageSnapshot = browser.Snapshot{Title: "T", Refs: map[string]string{"@e1": "Go"}}
	if err := RegisterBrowserToolsWithConfig(reg2, browser.NewSessionStore(), func() (browser.Backend, error) { return fb2, nil }, &BrowserToolsConfig{
		PendingStore: pending2,
		TokenGen:     &fakeTokenGen{next: "tok-click"},
	}); err != nil {
		t.Fatal(err)
	}
	click, _ := reg2.Get("browser_click")
	ctx2 := context.WithValue(context.Background(), ContextKeySessionID, "sess-click")
	cout, _ := click.Execute(ctx2, map[string]any{"ref": "@e1"})
	if cout.(map[string]any)["status"] != "pending" {
		t.Fatalf("%#v", cout)
	}
	if len(fb2.ClickedRefs) != 0 {
		t.Fatal("click before confirm")
	}
	_, _ = click.Execute(ctx2, map[string]any{"ref": "@ignored", "confirm_token": "tok-click"})
	if len(fb2.ClickedRefs) != 1 || fb2.ClickedRefs[0] != "@e1" {
		t.Fatalf("%v", fb2.ClickedRefs)
	}
}

func TestBrowserTools_SnapshotNeverPending(t *testing.T) {
	reg := NewRegistry()
	fb := browser.NewFakeBackend()
	fb.PageSnapshot = browser.Snapshot{Title: "snap"}
	if err := RegisterBrowserToolsWithConfig(reg, browser.NewSessionStore(), func() (browser.Backend, error) { return fb, nil }, &BrowserToolsConfig{
		PendingStore: NewInMemoryBrowserPendingStore(),
		TokenGen:     &fakeTokenGen{next: "x"},
	}); err != nil {
		t.Fatal(err)
	}
	snap, _ := reg.Get("browser_snapshot")
	ctx := context.WithValue(context.Background(), ContextKeySessionID, "s")
	out, err := snap.Execute(ctx, map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	m := out.(map[string]any)
	if m["status"] == "pending" {
		t.Fatal("snapshot must not require confirm")
	}
	if m["ok"] != true {
		t.Fatalf("%#v", m)
	}
}

func TestBrowserTools_B2Extras_ScrollPressConsoleVision(t *testing.T) {
	root := t.TempDir()
	reg := NewRegistry()
	fb := browser.NewFakeBackend()
	fb.PageSnapshot = browser.Snapshot{Title: "page", Refs: map[string]string{}}
	fb.Images = []browser.ImageInfo{{URL: "https://example.com/a.png", Alt: "a"}}
	fb.ConsoleMsgs = []string{"log hello"}
	fb.EvalResults = map[string]string{"1+1": "2"}
	if err := RegisterBrowserTools(reg, browser.NewSessionStore(), func() (browser.Backend, error) { return fb, nil }); err != nil {
		t.Fatal(err)
	}
	ctx := context.WithValue(context.Background(), ContextKeySessionID, "s")
	ctx = context.WithValue(ctx, ContextKeyWorkspaceRoot, root)

	scroll, _ := reg.Get("browser_scroll")
	out, err := scroll.Execute(ctx, map[string]any{"direction": "down"})
	if err != nil {
		t.Fatal(err)
	}
	if out.(map[string]any)["ok"] != true {
		t.Fatalf("%#v", out)
	}
	if len(fb.ScrollDirs) != 1 || fb.ScrollDirs[0] != "down" {
		t.Fatalf("scroll recorded %#v", fb.ScrollDirs)
	}

	press, _ := reg.Get("browser_press")
	_, _ = press.Execute(ctx, map[string]any{"key": "Enter"})
	if len(fb.PressedKeys) != 1 || fb.PressedKeys[0] != "Enter" {
		t.Fatalf("press %#v", fb.PressedKeys)
	}

	back, _ := reg.Get("browser_back")
	_, _ = back.Execute(ctx, map[string]any{})
	if fb.BackCount != 1 {
		t.Fatalf("back=%d", fb.BackCount)
	}

	imgs, _ := reg.Get("browser_get_images")
	imgOut, _ := imgs.Execute(ctx, map[string]any{})
	if imgOut.(map[string]any)["count"] != 1 {
		t.Fatalf("%#v", imgOut)
	}

	con, _ := reg.Get("browser_console")
	cOut, _ := con.Execute(ctx, map[string]any{"expression": "1+1"})
	cm := cOut.(map[string]any)
	if cm["eval_result"] != "2" {
		t.Fatalf("%#v", cm)
	}

	vis, _ := reg.Get("browser_vision")
	vOut, err := vis.Execute(ctx, map[string]any{"question": "what?"})
	if err != nil {
		t.Fatal(err)
	}
	vm := vOut.(map[string]any)
	if vm["ok"] != true {
		t.Fatalf("%#v", vm)
	}
	path, _ := vm["screenshot_path"].(string)
	if path == "" {
		t.Fatal("missing screenshot_path")
	}
	if vm["analysis"] != nil {
		t.Fatal("analysis should be absent without Vision analyzer")
	}
}

func TestBrowserTools_B3_CDPAndDialog(t *testing.T) {
	reg := NewRegistry()
	fb := browser.NewFakeBackend()
	fb.DialogQueue = []browser.DialogInfo{{ID: "dlg-1", Type: "alert", Message: "hi"}}
	fb.CDPResult = map[string]any{"cookies": []any{}}
	if err := RegisterBrowserTools(reg, browser.NewSessionStore(), func() (browser.Backend, error) { return fb, nil }); err != nil {
		t.Fatal(err)
	}
	ctx := context.WithValue(context.Background(), ContextKeySessionID, "s")

	snap, _ := reg.Get("browser_snapshot")
	sout, _ := snap.Execute(ctx, map[string]any{})
	sm := sout.(map[string]any)
	if sm["pending_dialogs"] == nil {
		t.Fatalf("expected pending_dialogs: %#v", sm)
	}

	dlg, _ := reg.Get("browser_dialog")
	dout, err := dlg.Execute(ctx, map[string]any{"action": "accept", "dialog_id": "dlg-1"})
	if err != nil {
		t.Fatal(err)
	}
	if dout.(map[string]any)["ok"] != true {
		t.Fatalf("%#v", dout)
	}
	if len(fb.DialogQueue) != 0 {
		t.Fatalf("dialog not cleared: %#v", fb.DialogQueue)
	}

	cdp, _ := reg.Get("browser_cdp")
	cout, err := cdp.Execute(ctx, map[string]any{
		"method": "Network.getAllCookies",
		"params": map[string]any{},
	})
	if err != nil {
		t.Fatal(err)
	}
	cm := cout.(map[string]any)
	if cm["ok"] != true || cm["method"] != "Network.getAllCookies" {
		t.Fatalf("%#v", cm)
	}
	if len(fb.CDPCalls) != 1 {
		t.Fatalf("cdp calls %#v", fb.CDPCalls)
	}
}

func TestBrowserTools_ConfirmErrorCode_NotFound(t *testing.T) {
	reg := NewRegistry()
	if err := RegisterBrowserToolsWithConfig(reg, browser.NewSessionStore(), func() (browser.Backend, error) {
		return browser.NewFakeBackend(), nil
	}, &BrowserToolsConfig{
		PendingStore:    NewInMemoryBrowserPendingStore(),
		TokenGen:        &fakeTokenGen{next: "tok"},
		ConfirmNavigate: true,
	}); err != nil {
		t.Fatal(err)
	}
	ctx := context.WithValue(context.Background(), ContextKeySessionID, "sess")
	nav, _ := reg.Get("browser_navigate")
	res, err := nav.Execute(ctx, map[string]any{
		"url":           "https://example.com/",
		"confirm_token": "no-such-token",
	})
	if err != nil {
		t.Fatal(err)
	}
	m := res.(map[string]any)
	if m["error_code"] != "not_found" {
		t.Fatalf("error_code: %#v", m)
	}
	if m["error"] != "确认已失效（可能已被替换、已使用或服务重启），请重新发起" {
		t.Fatalf("error: %#v", m)
	}
	if m["ok"] != false {
		t.Fatalf("ok: %#v", m)
	}
}

func TestBrowserTools_ConfirmErrorCode_Expired(t *testing.T) {
	reg := NewRegistry()
	pending := NewInMemoryBrowserPendingStore()
	if err := RegisterBrowserToolsWithConfig(reg, browser.NewSessionStore(), func() (browser.Backend, error) {
		return browser.NewFakeBackend(), nil
	}, &BrowserToolsConfig{
		PendingStore:      pending,
		TokenGen:          &fakeTokenGen{next: "tok-exp"},
		ConfirmNavigate:   true,
		ConfirmTTLSeconds: 60,
	}); err != nil {
		t.Fatal(err)
	}
	ctx := context.WithValue(context.Background(), ContextKeySessionID, "sess-exp")
	nav, _ := reg.Get("browser_navigate")
	_, err := nav.Execute(ctx, map[string]any{"url": "https://example.com/"})
	if err != nil {
		t.Fatal(err)
	}
	p, _ := pending.GetPending(ctx, "sess-exp", "tok-exp")
	if p == nil {
		t.Fatal("pending missing")
	}
	p.CreatedAt = time.Now().Add(-10 * time.Minute)
	_ = pending.SavePending(ctx, "sess-exp", *p)

	res, err := nav.Execute(ctx, map[string]any{
		"url":           "https://ignored.example/",
		"confirm_token": "tok-exp",
	})
	if err != nil {
		t.Fatal(err)
	}
	m := res.(map[string]any)
	if m["error_code"] != "expired" {
		t.Fatalf("error_code: %#v", m)
	}
	if m["error"] != "确认已过期，请让助手重新发起操作" {
		t.Fatalf("error: %#v", m)
	}
	if m["ok"] != false {
		t.Fatalf("ok: %#v", m)
	}
}
