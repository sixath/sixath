package tool

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/sixath/framework/tool/browser"
	fwws "github.com/sixath/framework/workspace"
)

// RegisterBrowserTools registers browser_* tools without confirm (zero-regression default).
func RegisterBrowserTools(reg *Registry, store *browser.SessionStore, factory func() (browser.Backend, error)) error {
	return RegisterBrowserToolsWithConfig(reg, store, factory, nil)
}

// RegisterBrowserToolsWithConfig registers browser tools; when cfg has PendingStore+TokenGen,
// navigate/click/type require confirm_token (snapshot never does).
func RegisterBrowserToolsWithConfig(reg *Registry, store *browser.SessionStore, factory func() (browser.Backend, error), cfg *BrowserToolsConfig) error {
	if reg == nil {
		return errors.New("browser tools: registry is nil")
	}
	if store == nil {
		return errors.New("browser tools: session store is nil")
	}
	if factory == nil {
		return errors.New("browser tools: factory is nil")
	}
	c := browserToolsConfigOrDefault(cfg)

	checkFn := func(ctx context.Context) error {
		b, err := factory()
		if err != nil {
			return err
		}
		defer b.Close(ctx)
		return b.Healthy(ctx)
	}

	backendFor := func(ctx context.Context) (browser.Backend, error) {
		return store.GetOrCreate(browserSessionID(ctx), factory)
	}

	tools := []Tool{
		{
			Name: "browser_navigate",
			Description: "Navigate the browser to a URL and return a compact page snapshot with element refs. " +
				"Prefer this + browser_get_images for collecting page images; avoid browser_cdp for image scraping. " +
				"May require confirm_token only when navigate confirmation is explicitly enabled.",
			Toolset: ToolsetBrowser,
			CheckFn: checkFn,
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"url": map[string]any{
						"type":        "string",
						"description": "Absolute http(s) URL to open.",
					},
					"confirm_token": map[string]any{
						"type":        "string",
						"description": "Confirmation token from a previous navigate proposal (only when navigate confirm is enabled).",
					},
				},
				"required": []string{"url"},
			},
			Execute: func(ctx context.Context, params map[string]any) (any, error) {
				if token, _ := params["confirm_token"].(string); strings.TrimSpace(token) != "" {
					return confirmBrowserAction(ctx, c, backendFor, "navigate", strings.TrimSpace(token))
				}
				url, _ := params["url"].(string)
				if err := ValidateOutboundURL(url); err != nil {
					return browserToolErr(err.Error(), ErrorPermanent), nil
				}
				if c.confirmNavigateEnabled() {
					return proposeBrowserAction(ctx, c, PendingBrowser{Action: "navigate", URL: url})
				}
				return execBrowserNavigate(ctx, backendFor, url)
			},
		},
		{
			Name:        "browser_snapshot",
			Description: "Capture the current page snapshot (accessibility/compact text and element refs).",
			Toolset:     ToolsetBrowser,
			CheckFn:     checkFn,
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"full": map[string]any{
						"type":        "boolean",
						"description": "When true, include a fuller page text dump (default false).",
					},
				},
			},
			Execute: func(ctx context.Context, params map[string]any) (any, error) {
				full, _ := params["full"].(bool)
				b, err := backendFor(ctx)
				if err != nil {
					return browserToolErr(err.Error(), ErrorTransient), nil
				}
				snap, err := b.Snapshot(ctx, full)
				if err != nil {
					return browserToolErr(err.Error(), ErrorTransient), nil
				}
				return browserSnapshotResult(snap), nil
			},
		},
		{
			Name: "browser_click",
			Description: "Click an element by ref from a prior browser_snapshot / navigate result (e.g. @e1). " +
				"May require user confirm via confirm_token when confirmation is enabled.",
			Toolset: ToolsetBrowser,
			CheckFn: checkFn,
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"ref": map[string]any{
						"type":        "string",
						"description": "Element ref from snapshot (e.g. @e1).",
					},
					"confirm_token": map[string]any{
						"type":        "string",
						"description": "Confirmation token from a previous click proposal.",
					},
				},
				"required": []string{"ref"},
			},
			Execute: func(ctx context.Context, params map[string]any) (any, error) {
				if token, _ := params["confirm_token"].(string); strings.TrimSpace(token) != "" {
					return confirmBrowserAction(ctx, c, backendFor, "click", strings.TrimSpace(token))
				}
				ref, _ := params["ref"].(string)
				if strings.TrimSpace(ref) == "" {
					return browserToolErr("ref is required", ErrorPermanent), nil
				}
				if c.confirmEnabled() {
					return proposeBrowserAction(ctx, c, PendingBrowser{Action: "click", Ref: strings.TrimSpace(ref)})
				}
				return execBrowserClick(ctx, backendFor, ref)
			},
		},
		{
			Name: "browser_type",
			Description: "Type text into an element identified by ref from a prior snapshot. " +
				"May require user confirm via confirm_token when confirmation is enabled.",
			Toolset: ToolsetBrowser,
			CheckFn: checkFn,
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"ref": map[string]any{
						"type":        "string",
						"description": "Element ref from snapshot (e.g. @e1).",
					},
					"text": map[string]any{
						"type":        "string",
						"description": "Text to type into the element.",
					},
					"confirm_token": map[string]any{
						"type":        "string",
						"description": "Confirmation token from a previous type proposal.",
					},
				},
				"required": []string{"ref", "text"},
			},
			Execute: func(ctx context.Context, params map[string]any) (any, error) {
				if token, _ := params["confirm_token"].(string); strings.TrimSpace(token) != "" {
					return confirmBrowserAction(ctx, c, backendFor, "type", strings.TrimSpace(token))
				}
				ref, _ := params["ref"].(string)
				text, _ := params["text"].(string)
				if strings.TrimSpace(ref) == "" {
					return browserToolErr("ref is required", ErrorPermanent), nil
				}
				if c.confirmEnabled() {
					return proposeBrowserAction(ctx, c, PendingBrowser{
						Action: "type",
						Ref:    strings.TrimSpace(ref),
						Text:   text,
					})
				}
				return execBrowserType(ctx, backendFor, ref, text)
			},
		},
		{
			Name:        "browser_scroll",
			Description: "Scroll the page up or down to reveal content outside the viewport. Requires browser_navigate first.",
			Toolset:     ToolsetBrowser,
			CheckFn:     checkFn,
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"direction": map[string]any{
						"type":        "string",
						"description": "Scroll direction: up or down.",
						"enum":        []string{"up", "down"},
					},
				},
				"required": []string{"direction"},
			},
			Execute: func(ctx context.Context, params map[string]any) (any, error) {
				dir, _ := params["direction"].(string)
				dir = strings.ToLower(strings.TrimSpace(dir))
				if dir != "up" && dir != "down" {
					return browserToolErr("direction must be up or down", ErrorPermanent), nil
				}
				b, err := backendFor(ctx)
				if err != nil {
					return browserToolErr(err.Error(), ErrorTransient), nil
				}
				snap, err := b.Scroll(ctx, dir)
				if err != nil {
					return browserToolErr(err.Error(), ErrorTransient), nil
				}
				return browserSnapshotResult(snap), nil
			},
		},
		{
			Name:        "browser_back",
			Description: "Navigate back in browser history. Requires browser_navigate first.",
			Toolset:     ToolsetBrowser,
			CheckFn:     checkFn,
			Parameters: map[string]any{
				"type":       "object",
				"properties": map[string]any{},
			},
			Execute: func(ctx context.Context, params map[string]any) (any, error) {
				b, err := backendFor(ctx)
				if err != nil {
					return browserToolErr(err.Error(), ErrorTransient), nil
				}
				snap, err := b.Back(ctx)
				if err != nil {
					return browserToolErr(err.Error(), ErrorTransient), nil
				}
				return browserSnapshotResult(snap), nil
			},
		},
		{
			Name:        "browser_press",
			Description: "Press a keyboard key (Enter, Tab, Escape, ArrowDown, …). Useful for form submit and modals.",
			Toolset:     ToolsetBrowser,
			CheckFn:     checkFn,
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"key": map[string]any{
						"type":        "string",
						"description": "Key name, e.g. Enter, Tab, Escape, ArrowDown.",
					},
				},
				"required": []string{"key"},
			},
			Execute: func(ctx context.Context, params map[string]any) (any, error) {
				key, _ := params["key"].(string)
				if strings.TrimSpace(key) == "" {
					return browserToolErr("key is required", ErrorPermanent), nil
				}
				b, err := backendFor(ctx)
				if err != nil {
					return browserToolErr(err.Error(), ErrorTransient), nil
				}
				snap, err := b.Press(ctx, key)
				if err != nil {
					return browserToolErr(err.Error(), ErrorTransient), nil
				}
				return browserSnapshotResult(snap), nil
			},
		},
		{
			Name: "browser_get_images",
			Description: "List content images on the current page (filters UI icons/SVGs). " +
				"Preferred way to collect photo URLs after navigate — do not use browser_cdp Runtime.evaluate for this. " +
				"Returns url/alt/width/height sorted by size.",
			Toolset: ToolsetBrowser,
			CheckFn: checkFn,
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"limit": map[string]any{
						"type":        "integer",
						"description": "Max images to return (default 20, max 50).",
					},
					"min_width": map[string]any{
						"type":        "integer",
						"description": "Drop images narrower than this when width is known (default 80).",
					},
				},
			},
			Execute: func(ctx context.Context, params map[string]any) (any, error) {
				b, err := backendFor(ctx)
				if err != nil {
					return browserToolErr(err.Error(), ErrorTransient), nil
				}
				imgs, err := b.GetImages(ctx)
				if err != nil {
					return browserToolErr(err.Error(), ErrorTransient), nil
				}
				limit := intFromParam(params["limit"], 20)
				if limit <= 0 {
					limit = 20
				}
				if limit > 50 {
					limit = 50
				}
				minW := intFromParam(params["min_width"], 80)
				if minW < 0 {
					minW = 0
				}
				list := make([]map[string]any, 0, limit)
				skipped := 0
				for _, im := range imgs {
					if !isContentImageURL(im.URL) {
						skipped++
						continue
					}
					if minW > 0 && im.Width > 0 && im.Width < minW {
						skipped++
						continue
					}
					item := map[string]any{"url": im.URL, "alt": im.Alt}
					if im.Width > 0 {
						item["width"] = im.Width
					}
					if im.Height > 0 {
						item["height"] = im.Height
					}
					list = append(list, item)
					if len(list) >= limit {
						break
					}
				}
				out := map[string]any{"ok": true, "images": list, "count": len(list)}
				if skipped > 0 {
					out["filtered_out"] = skipped
				}
				if len(list) == 0 {
					out["hint"] = "No content images found. Page may be a captcha/login wall, or images are lazy-loaded — try browser_scroll then retry, or another URL. Prefer not to use browser_cdp for this."
				}
				return out, nil
			},
		},
		{
			Name: "browser_console",
			Description: "Read browser console logs/JS exceptions. When expression is set, evaluate JavaScript in page context.",
			Toolset: ToolsetBrowser,
			CheckFn: checkFn,
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"clear": map[string]any{
						"type":        "boolean",
						"description": "Clear buffered console after read (default false).",
					},
					"expression": map[string]any{
						"type":        "string",
						"description": "Optional JS to evaluate in the page.",
					},
				},
			},
			Execute: func(ctx context.Context, params map[string]any) (any, error) {
				clear, _ := params["clear"].(bool)
				expr, _ := params["expression"].(string)
				b, err := backendFor(ctx)
				if err != nil {
					return browserToolErr(err.Error(), ErrorTransient), nil
				}
				res, err := b.Console(ctx, clear, expr)
				if err != nil {
					return browserToolErr(err.Error(), ErrorTransient), nil
				}
				out := map[string]any{
					"ok":         true,
					"messages":   res.Messages,
					"exceptions": res.Exceptions,
				}
				if strings.TrimSpace(expr) != "" {
					out["eval_result"] = res.EvalResult
				}
				return out, nil
			},
		},
		{
			Name: "browser_vision",
			Description: "Capture a PNG screenshot of the current page to the workspace. " +
				"When a vision LLM is configured and question is set (or analyze=true), returns analysis text.",
			Toolset: ToolsetBrowser,
			CheckFn: checkFn,
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"question": map[string]any{
						"type":        "string",
						"description": "Question for the vision LLM (triggers analysis when Vision is configured).",
					},
					"analyze": map[string]any{
						"type":        "boolean",
						"description": "If true, run vision analysis even without a custom question (uses a default prompt).",
					},
				},
			},
			Execute: func(ctx context.Context, params map[string]any) (any, error) {
				question, _ := params["question"].(string)
				wantAnalyze := boolFromParam(params["analyze"]) || strings.TrimSpace(question) != ""
				b, err := backendFor(ctx)
				if err != nil {
					return browserToolErr(err.Error(), ErrorTransient), nil
				}
				png, err := b.Screenshot(ctx)
				if err != nil {
					return browserToolErr(err.Error(), ErrorTransient), nil
				}
				ws, _ := ctx.Value(ContextKeyWorkspaceRoot).(string)
				rel, abs, werr := saveBrowserScreenshot(ws, png)
				if werr != nil {
					return browserToolErr(werr.Error(), ErrorTransient), nil
				}
				out := map[string]any{
					"ok":              true,
					"screenshot_path": rel,
					"bytes":           len(png),
				}
				if strings.TrimSpace(question) != "" {
					out["question"] = question
				}
				if wantAnalyze && c.Vision != nil {
					analysis, aerr := analyzeScreenshot(ctx, c.Vision, png, question)
					if aerr != nil {
						out["vision_error"] = aerr.Error()
						out["hint"] = "Screenshot saved; vision analysis failed (" + aerr.Error() + "). " +
							"If the chat model rejects image modality, set SATH_VISION_* to a vision-capable model, or use MEDIA:" + rel
					} else {
						out["analysis"] = analysis
					}
				} else if wantAnalyze && c.Vision == nil {
					out["hint"] = "Screenshot saved. Vision LLM not configured; include MEDIA:" + rel + " or set SATH_VISION_ENABLED."
				} else {
					out["hint"] = "Screenshot saved. Pass question=... or analyze=true to run vision LLM when configured. MEDIA:" + rel
				}
				_ = abs
				return out, nil
			},
		},
		{
			Name: "browser_cdp",
			Description: "Send a raw Chrome DevTools Protocol command on the current page target. " +
				"Escape hatch for cookies, network, etc. Do NOT use Runtime.evaluate to scrape images — use browser_get_images. " +
				"See https://chromedevtools.github.io/devtools-protocol/. " +
				"MVP: operates on the current tab; target_id/frame_id are accepted but not yet routed.",
			Toolset: ToolsetBrowser,
			CheckFn: checkFn,
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"method": map[string]any{
						"type":        "string",
						"description": "CDP method, e.g. Network.getAllCookies, Runtime.evaluate.",
					},
					"params": map[string]any{
						"type":        "object",
						"description": "Optional CDP params object.",
					},
					"target_id": map[string]any{
						"type":        "string",
						"description": "Optional tab target id (not routed in MVP).",
					},
					"frame_id": map[string]any{
						"type":        "string",
						"description": "Optional frame id (not routed in MVP).",
					},
				},
				"required": []string{"method"},
			},
			Execute: func(ctx context.Context, params map[string]any) (any, error) {
				method, _ := params["method"].(string)
				if strings.TrimSpace(method) == "" {
					return browserToolErr("method is required", ErrorPermanent), nil
				}
				var cdpParams map[string]any
				if p, ok := params["params"].(map[string]any); ok {
					cdpParams = p
				}
				b, err := backendFor(ctx)
				if err != nil {
					return browserToolErr(err.Error(), ErrorTransient), nil
				}
				res, err := b.CDP(ctx, method, cdpParams)
				if err != nil {
					return browserToolErr(err.Error(), ErrorTransient), nil
				}
				out := map[string]any{"ok": true, "method": method, "result": res}
				if tid, _ := params["target_id"].(string); strings.TrimSpace(tid) != "" {
					out["hint"] = "target_id ignored in MVP (current tab only)"
				}
				if fid, _ := params["frame_id"].(string); strings.TrimSpace(fid) != "" {
					out["hint"] = "frame_id ignored in MVP (current tab only)"
				}
				return out, nil
			},
		},
		{
			Name: "browser_dialog",
			Description: "Accept or dismiss a native JS dialog (alert/confirm/prompt/beforeunload). " +
				"Call browser_snapshot first; pending dialogs appear in pending_dialogs.",
			Toolset: ToolsetBrowser,
			CheckFn: checkFn,
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"action": map[string]any{
						"type":        "string",
						"description": "accept or dismiss",
						"enum":        []string{"accept", "dismiss"},
					},
					"prompt_text": map[string]any{
						"type":        "string",
						"description": "Text for prompt dialogs when action=accept.",
					},
					"dialog_id": map[string]any{
						"type":        "string",
						"description": "Optional id from pending_dialogs when multiple are queued.",
					},
				},
				"required": []string{"action"},
			},
			Execute: func(ctx context.Context, params map[string]any) (any, error) {
				action, _ := params["action"].(string)
				prompt, _ := params["prompt_text"].(string)
				dialogID, _ := params["dialog_id"].(string)
				action = strings.ToLower(strings.TrimSpace(action))
				if action != "accept" && action != "dismiss" {
					return browserToolErr("action must be accept or dismiss", ErrorPermanent), nil
				}
				b, err := backendFor(ctx)
				if err != nil {
					return browserToolErr(err.Error(), ErrorTransient), nil
				}
				if err := b.HandleDialog(ctx, action, prompt, strings.TrimSpace(dialogID)); err != nil {
					return browserToolErr(err.Error(), ErrorPermanent), nil
				}
				return map[string]any{"ok": true, "action": action}, nil
			},
		},
	}

	for _, t := range tools {
		if err := reg.Register(t); err != nil {
			return err
		}
	}
	if c.Vision != nil {
		if err := RegisterVisionAnalyzeTool(reg, c.Vision); err != nil {
			return err
		}
	}
	return nil
}

func saveBrowserScreenshot(workspace string, png []byte) (rel, abs string, err error) {
	workspace = strings.TrimSpace(workspace)
	if workspace == "" {
		return "", "", fmt.Errorf("workspace_root not set for screenshot")
	}
	rel = filepath.ToSlash(filepath.Join(".browser", "screenshots", time.Now().Format("20060102-150405.000")+".png"))
	full, err := fwws.ResolveWorkspacePath(workspace, rel)
	if err != nil {
		return "", "", err
	}
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		return "", "", err
	}
	if err := os.WriteFile(full, png, 0o644); err != nil {
		return "", "", err
	}
	return rel, full, nil
}

func proposeBrowserAction(ctx context.Context, c *BrowserToolsConfig, pending PendingBrowser) (any, error) {
	chatSID, _ := ctx.Value(ContextKeySessionID).(string)
	if strings.TrimSpace(chatSID) == "" {
		return browserToolErr("session_id is required for browser confirm", ErrorPermanent), nil
	}
	token, err := c.TokenGen.NewToken()
	if err != nil {
		return browserToolErr(fmt.Sprintf("generate token: %v", err), ErrorTransient), nil
	}
	ttl := c.ConfirmTTLSeconds
	if ttl <= 0 {
		ttl = 300
	}
	pending.Token = token
	pending.CreatedAt = time.Now()
	if err := c.PendingStore.SavePending(ctx, chatSID, pending); err != nil {
		return browserToolErr(err.Error(), ErrorTransient), nil
	}
	preview := pending.Action
	switch pending.Action {
	case "navigate":
		preview = "navigate " + pending.URL
	case "click":
		preview = "click " + pending.Ref
	case "type":
		preview = fmt.Sprintf("type into %s (%d chars)", pending.Ref, len(pending.Text))
	}
	return map[string]any{
		"ok":         true,
		"status":     "pending",
		"token":      token,
		"action":     pending.Action,
		"preview":    preview,
		"url":        pending.URL,
		"ref":        pending.Ref,
		"expires_in": ttl,
		"hint":       "user must confirm; re-call with confirm_token to apply",
	}, nil
}

type browserBackendFn func(ctx context.Context) (browser.Backend, error)

func confirmBrowserAction(ctx context.Context, c *BrowserToolsConfig, backendFor browserBackendFn, expectedAction, token string) (any, error) {
	if !c.confirmEnabled() {
		return browserToolErr("browser: confirm store not configured", ErrorPermanent), nil
	}
	chatSID, _ := ctx.Value(ContextKeySessionID).(string)
	if strings.TrimSpace(chatSID) == "" {
		return browserToolErr("session_id is required", ErrorPermanent), nil
	}
	pending, err := c.PendingStore.GetPending(ctx, chatSID, token)
	if err != nil {
		return browserToolErr(err.Error(), ErrorTransient), nil
	}
	if pending == nil {
		msg, code := confirmTokenParts("not_found")
		return browserToolErr(msg, code), nil
	}
	if pending.Action != expectedAction {
		return browserToolErr(fmt.Sprintf("confirm_token action mismatch: expected %s got %s", expectedAction, pending.Action), ErrorPermanent), nil
	}
	ttl := c.ConfirmTTLSeconds
	if ttl <= 0 {
		ttl = 300
	}
	if time.Since(pending.CreatedAt) > time.Duration(ttl)*time.Second {
		_ = c.PendingStore.DeletePending(ctx, chatSID, token)
		msg, code := confirmTokenParts("expired")
		return browserToolErr(msg, code), nil
	}
	var out any
	switch pending.Action {
	case "navigate":
		if err := ValidateOutboundURL(pending.URL); err != nil {
			return browserToolErr(err.Error(), ErrorPermanent), nil
		}
		out, err = execBrowserNavigate(ctx, backendFor, pending.URL)
	case "click":
		out, err = execBrowserClick(ctx, backendFor, pending.Ref)
	case "type":
		out, err = execBrowserType(ctx, backendFor, pending.Ref, pending.Text)
	default:
		return browserToolErr("unknown pending action", ErrorPermanent), nil
	}
	if err != nil {
		return out, err
	}
	if m, ok := out.(map[string]any); ok {
		if okFlag, has := m["ok"]; has && okFlag == false {
			return out, nil
		}
	}
	_ = c.PendingStore.DeletePending(ctx, chatSID, token)
	return out, nil
}

func execBrowserNavigate(ctx context.Context, backendFor browserBackendFn, url string) (any, error) {
	b, err := backendFor(ctx)
	if err != nil {
		return browserToolErr(err.Error(), ErrorTransient), nil
	}
	snap, err := b.Navigate(ctx, url)
	if err != nil {
		return browserToolErr(err.Error(), ErrorTransient), nil
	}
	return browserSnapshotResult(snap), nil
}

func execBrowserClick(ctx context.Context, backendFor browserBackendFn, ref string) (any, error) {
	b, err := backendFor(ctx)
	if err != nil {
		return browserToolErr(err.Error(), ErrorTransient), nil
	}
	snap, err := b.Click(ctx, ref)
	if err != nil {
		return browserToolErr(err.Error(), ErrorTransient), nil
	}
	return browserSnapshotResult(snap), nil
}

func execBrowserType(ctx context.Context, backendFor browserBackendFn, ref, text string) (any, error) {
	b, err := backendFor(ctx)
	if err != nil {
		return browserToolErr(err.Error(), ErrorTransient), nil
	}
	snap, err := b.Type(ctx, ref, text)
	if err != nil {
		return browserToolErr(err.Error(), ErrorTransient), nil
	}
	return browserSnapshotResult(snap), nil
}

func browserSessionID(ctx context.Context) string {
	if ctx == nil {
		return "default"
	}
	if s, ok := ctx.Value(ContextKeySessionID).(string); ok {
		if sid := strings.TrimSpace(s); sid != "" {
			return sid
		}
	}
	return "default"
}

func browserSnapshotResult(snap browser.Snapshot) map[string]any {
	refs := snap.Refs
	if refs == nil {
		refs = map[string]string{}
	}
	out := map[string]any{
		"ok":    true,
		"url":   snap.URL,
		"title": snap.Title,
		"text":  snap.Text,
		"refs":  refs,
	}
	if len(snap.PendingDialogs) > 0 {
		dialogs := make([]map[string]any, 0, len(snap.PendingDialogs))
		for _, d := range snap.PendingDialogs {
			dialogs = append(dialogs, map[string]any{
				"id": d.ID, "type": d.Type, "message": d.Message,
			})
		}
		out["pending_dialogs"] = dialogs
	}
	if warn := browserChallengeWarning(snap.URL, snap.Title, snap.Text); warn != "" {
		out["warning"] = warn
		out["hint"] = "Page looks like a captcha/login/anti-bot interstitial — pick another source URL or ask the user to resolve the challenge."
	}
	return out
}

func browserChallengeWarning(pageURL, title, text string) string {
	u := strings.ToLower(pageURL)
	t := strings.ToLower(title + "\n" + text)
	switch {
	case strings.Contains(u, "sec.douban.com"),
		strings.Contains(u, "/captcha"),
		strings.Contains(u, "challenge"),
		strings.Contains(u, "cdn-cgi/challenge"):
		return "anti_bot_or_captcha"
	case strings.Contains(t, "验证码"),
		strings.Contains(t, "captcha"),
		strings.Contains(t, "unusual traffic"),
		strings.Contains(t, "are you a robot"),
		strings.Contains(t, "security check"):
		return "anti_bot_or_captcha"
	default:
		return ""
	}
}

// isContentImageURL filters chrome UI / tracking / tiny-asset URLs.
func isContentImageURL(raw string) bool {
	u := strings.TrimSpace(strings.ToLower(raw))
	if u == "" {
		return false
	}
	if strings.HasPrefix(u, "data:") || strings.HasPrefix(u, "blob:") {
		return false
	}
	if strings.Contains(u, ".svg") {
		return false
	}
	deny := []string{
		"/rp/", "favicon", "sprite", "1x1", "pixel.", "tracking",
		"chrome-extension:", "logo-icon", "grey.gif",
	}
	for _, d := range deny {
		if strings.Contains(u, d) {
			return false
		}
	}
	return true
}

func browserToolErr(msg, code string) map[string]any {
	return map[string]any{
		"ok":         false,
		"error":      msg,
		"error_code": code,
	}
}
