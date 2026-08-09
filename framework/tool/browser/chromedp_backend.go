package browser

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	cdpbrowser "github.com/chromedp/cdproto/browser"
	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"
)

const browserCDPURLEnv = "BROWSER_CDP_URL"

// chromedpBackend drives a real Chrome/Chromium page via CDP.
type chromedpBackend struct {
	allocCancel context.CancelFunc
	tabCancel   context.CancelFunc
	ctx         context.Context

	mu   sync.Mutex
	refs map[string]string // @eN -> CSS selector

	consoleMu   sync.Mutex
	consoleLogs []string
	consoleErrs []string

	dialogMu sync.Mutex
	dialogs  []DialogInfo
	dialogSeq int
}

// NewChromedpBackend connects to a remote CDP endpoint when BROWSER_CDP_URL is
// set; otherwise it launches a local headless Chrome via ExecAllocator.
func NewChromedpBackend(ctx context.Context) (Backend, error) {
	return NewChromedpBackendWithDownload(ctx, DownloadConfig{Mode: DownloadDeny})
}

// NewChromedpBackendWithDownload is like NewChromedpBackend with download policy.
func NewChromedpBackendWithDownload(ctx context.Context, dl DownloadConfig) (Backend, error) {
	var (
		allocCtx    context.Context
		allocCancel context.CancelFunc
	)
	if cdpURL := os.Getenv(browserCDPURLEnv); cdpURL != "" {
		allocCtx, allocCancel = chromedp.NewRemoteAllocator(ctx, cdpURL)
	} else {
		opts := append(chromedp.DefaultExecAllocatorOptions[:], chromedp.Headless)
		allocCtx, allocCancel = chromedp.NewExecAllocator(ctx, opts...)
	}

	tabCtx, tabCancel := chromedp.NewContext(allocCtx)
	b := &chromedpBackend{
		allocCancel: allocCancel,
		tabCancel:   tabCancel,
		ctx:         tabCtx,
		refs:        map[string]string{},
	}

	chromedp.ListenTarget(tabCtx, func(ev interface{}) {
		switch e := ev.(type) {
		case *runtime.EventConsoleAPICalled:
			msg := formatConsoleAPI(e)
			b.consoleMu.Lock()
			if len(b.consoleLogs) < 200 {
				b.consoleLogs = append(b.consoleLogs, msg)
			}
			b.consoleMu.Unlock()
		case *runtime.EventExceptionThrown:
			msg := "exception"
			if e.ExceptionDetails != nil {
				msg = e.ExceptionDetails.Text
				if e.ExceptionDetails.Exception != nil && e.ExceptionDetails.Exception.Description != "" {
					msg = e.ExceptionDetails.Exception.Description
				}
			}
			b.consoleMu.Lock()
			if len(b.consoleErrs) < 100 {
				b.consoleErrs = append(b.consoleErrs, msg)
			}
			b.consoleMu.Unlock()
		case *page.EventJavascriptDialogOpening:
			b.dialogMu.Lock()
			b.dialogSeq++
			id := fmt.Sprintf("dlg-%d", b.dialogSeq)
			b.dialogs = append(b.dialogs, DialogInfo{
				ID:      id,
				Type:    string(e.Type),
				Message: e.Message,
			})
			b.dialogMu.Unlock()
		}
	})

	// Start the browser/tab eagerly so callers fail fast when Chrome is missing.
	if err := chromedp.Run(tabCtx, chromedp.Navigate("about:blank"), page.Enable()); err != nil {
		_ = b.Close(context.Background())
		return nil, fmt.Errorf("chromedp start: %w", err)
	}
	if err := applyDownloadBehavior(tabCtx, dl); err != nil {
		_ = b.Close(context.Background())
		return nil, err
	}
	return b, nil
}

func formatConsoleAPI(e *runtime.EventConsoleAPICalled) string {
	parts := make([]string, 0, len(e.Args)+1)
	parts = append(parts, string(e.Type))
	for _, a := range e.Args {
		if a == nil {
			continue
		}
		if a.Value != nil {
			parts = append(parts, fmt.Sprint(a.Value))
		} else if a.Description != "" {
			parts = append(parts, a.Description)
		} else if a.UnserializableValue != "" {
			parts = append(parts, string(a.UnserializableValue))
		}
	}
	return strings.Join(parts, " ")
}

func applyDownloadBehavior(ctx context.Context, dl DownloadConfig) error {
	mode := dl.Mode
	if mode == "" {
		mode = DownloadDeny
	}
	switch mode {
	case DownloadDeny:
		return chromedp.Run(ctx, chromedp.ActionFunc(func(ctx context.Context) error {
			return cdpbrowser.SetDownloadBehavior(cdpbrowser.SetDownloadBehaviorBehaviorDeny).Do(ctx)
		}))
	case DownloadWorkspace:
		dir := strings.TrimSpace(dl.Dir)
		if dir == "" {
			dir = "downloads"
		}
		// Absolute path required by Chrome; caller should pass resolved workspace path in Dir.
		abs := dir
		if !filepath.IsAbs(abs) {
			var err error
			abs, err = filepath.Abs(abs)
			if err != nil {
				return err
			}
		}
		if err := os.MkdirAll(abs, 0o755); err != nil {
			return err
		}
		return chromedp.Run(ctx, chromedp.ActionFunc(func(ctx context.Context) error {
			return cdpbrowser.SetDownloadBehavior(cdpbrowser.SetDownloadBehaviorBehaviorAllowAndName).
				WithDownloadPath(abs).
				WithEventsEnabled(true).
				Do(ctx)
		}))
	default:
		return fmt.Errorf("unknown download mode %q", mode)
	}
}

func (b *chromedpBackend) Navigate(ctx context.Context, url string) (Snapshot, error) {
	if err := ctx.Err(); err != nil {
		return Snapshot{}, err
	}
	b.mu.Lock()
	defer b.mu.Unlock()

	runCtx, cancel := context.WithTimeout(b.ctx, 30*time.Second)
	defer cancel()

	if err := chromedp.Run(runCtx, chromedp.Navigate(url)); err != nil {
		return Snapshot{}, fmt.Errorf("navigate %q: %w", url, err)
	}
	// Brief settle so SPA/lazy galleries populate beyond the shell.
	_ = chromedp.Run(runCtx, chromedp.Sleep(800*time.Millisecond))
	return b.snapshotLocked(runCtx, false)
}

func (b *chromedpBackend) Snapshot(ctx context.Context, full bool) (Snapshot, error) {
	if err := ctx.Err(); err != nil {
		return Snapshot{}, err
	}
	b.mu.Lock()
	defer b.mu.Unlock()

	runCtx, cancel := context.WithTimeout(b.ctx, 15*time.Second)
	defer cancel()

	return b.snapshotLocked(runCtx, full)
}

func (b *chromedpBackend) Click(ctx context.Context, ref string) (Snapshot, error) {
	if err := ctx.Err(); err != nil {
		return Snapshot{}, err
	}
	b.mu.Lock()
	defer b.mu.Unlock()

	sel, err := b.resolveRef(ref)
	if err != nil {
		return Snapshot{}, err
	}

	runCtx, cancel := context.WithTimeout(b.ctx, 15*time.Second)
	defer cancel()

	if err := chromedp.Run(runCtx, chromedp.Click(sel, chromedp.ByQuery)); err != nil {
		return Snapshot{}, fmt.Errorf("click %q (%s): %w", ref, sel, err)
	}
	return b.snapshotLocked(runCtx, false)
}

func (b *chromedpBackend) Type(ctx context.Context, ref, text string) (Snapshot, error) {
	if err := ctx.Err(); err != nil {
		return Snapshot{}, err
	}
	b.mu.Lock()
	defer b.mu.Unlock()

	sel, err := b.resolveRef(ref)
	if err != nil {
		return Snapshot{}, err
	}

	runCtx, cancel := context.WithTimeout(b.ctx, 15*time.Second)
	defer cancel()

	if err := chromedp.Run(runCtx, chromedp.SendKeys(sel, text, chromedp.ByQuery)); err != nil {
		return Snapshot{}, fmt.Errorf("type %q (%s): %w", ref, sel, err)
	}
	return b.snapshotLocked(runCtx, false)
}

func (b *chromedpBackend) Scroll(ctx context.Context, direction string) (Snapshot, error) {
	if err := ctx.Err(); err != nil {
		return Snapshot{}, err
	}
	b.mu.Lock()
	defer b.mu.Unlock()

	dy := 600
	switch strings.ToLower(strings.TrimSpace(direction)) {
	case "up":
		dy = -600
	case "down":
		dy = 600
	default:
		return Snapshot{}, fmt.Errorf("direction must be up or down")
	}

	runCtx, cancel := context.WithTimeout(b.ctx, 15*time.Second)
	defer cancel()
	js := fmt.Sprintf(`window.scrollBy(0, %d)`, dy)
	if err := chromedp.Run(runCtx, chromedp.Evaluate(js, nil)); err != nil {
		return Snapshot{}, fmt.Errorf("scroll: %w", err)
	}
	return b.snapshotLocked(runCtx, false)
}

func (b *chromedpBackend) Back(ctx context.Context) (Snapshot, error) {
	if err := ctx.Err(); err != nil {
		return Snapshot{}, err
	}
	b.mu.Lock()
	defer b.mu.Unlock()

	runCtx, cancel := context.WithTimeout(b.ctx, 15*time.Second)
	defer cancel()
	if err := chromedp.Run(runCtx, chromedp.NavigateBack()); err != nil {
		return Snapshot{}, fmt.Errorf("back: %w", err)
	}
	return b.snapshotLocked(runCtx, false)
}

func (b *chromedpBackend) Press(ctx context.Context, key string) (Snapshot, error) {
	if err := ctx.Err(); err != nil {
		return Snapshot{}, err
	}
	b.mu.Lock()
	defer b.mu.Unlock()

	key = strings.TrimSpace(key)
	if key == "" {
		return Snapshot{}, fmt.Errorf("key is required")
	}
	runCtx, cancel := context.WithTimeout(b.ctx, 15*time.Second)
	defer cancel()
	if err := chromedp.Run(runCtx, chromedp.KeyEvent(key)); err != nil {
		return Snapshot{}, fmt.Errorf("press %q: %w", key, err)
	}
	return b.snapshotLocked(runCtx, false)
}

func (b *chromedpBackend) GetImages(ctx context.Context) ([]ImageInfo, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	b.mu.Lock()
	defer b.mu.Unlock()

	runCtx, cancel := context.WithTimeout(b.ctx, 20*time.Second)
	defer cancel()

	// Nudge lazy-loaders before scraping.
	_ = chromedp.Run(runCtx,
		chromedp.Evaluate(`window.scrollBy(0, Math.min(1200, document.body.scrollHeight||1200))`, nil),
		chromedp.Sleep(400*time.Millisecond),
	)

	var imgs []ImageInfo
	js := `(function(){
  function abs(u){
    try { return new URL(u, document.baseURI).href; } catch(e){ return u||''; }
  }
  function push(out, seen, url, alt, w, h){
    url = abs((url||'').trim());
    if (!url || seen[url]) return;
    var low = url.toLowerCase();
    if (low.indexOf('data:') === 0) return;
    if (low.indexOf('blob:') === 0) return;
    if (/\.svg(\?|#|$)/i.test(low)) return;
    if (/\/rp\/|chrome-extension:|favicon|sprite|logo-?icon|1x1|pixel\.|tracking/i.test(low)) return;
    w = w|0; h = h|0;
    // Prefer content photos; keep unknown-size candidates but rank later.
    if ((w > 0 && w < 48) || (h > 0 && h < 48)) return;
    seen[url] = 1;
    out.push({url: url, alt: (alt||'').slice(0,200), width: w, height: h});
  }
  var out = [], seen = {};
  Array.from(document.querySelectorAll('img')).forEach(function(el){
    var w = el.naturalWidth || el.width || 0;
    var h = el.naturalHeight || el.height || 0;
    var src = el.currentSrc || el.src || el.getAttribute('data-src') || el.getAttribute('data-original') || '';
    push(out, seen, src, el.alt, w, h);
    var ss = el.getAttribute('srcset') || '';
    if (ss) {
      ss.split(',').forEach(function(part){
        var u = (part.trim().split(/\s+/)[0]||'');
        push(out, seen, u, el.alt, w, h);
      });
    }
  });
  // Common gallery / OG meta.
  Array.from(document.querySelectorAll('meta[property="og:image"], meta[name="twitter:image"]')).forEach(function(m){
    push(out, seen, m.getAttribute('content')||'', 'og:image', 0, 0);
  });
  Array.from(document.querySelectorAll('a[href*="mediaurl="], a[href*="/photo/"], a[href*="images/search"]')).forEach(function(a){
    try {
      var href = a.href || '';
      var m = href.match(/[?&]mediaurl=([^&]+)/i);
      if (m) push(out, seen, decodeURIComponent(m[1]), a.getAttribute('aria-label')||'', 0, 0);
    } catch(e){}
  });
  out.sort(function(a,b){
    var aa=(a.width||0)*(a.height||0), bb=(b.width||0)*(b.height||0);
    return bb-aa;
  });
  return out.slice(0, 80);
})()`
	if err := chromedp.Run(runCtx, chromedp.Evaluate(js, &imgs)); err != nil {
		return nil, fmt.Errorf("get_images: %w", err)
	}
	return imgs, nil
}

func (b *chromedpBackend) Console(ctx context.Context, clear bool, expression string) (ConsoleResult, error) {
	if err := ctx.Err(); err != nil {
		return ConsoleResult{}, err
	}
	b.mu.Lock()
	defer b.mu.Unlock()

	runCtx, cancel := context.WithTimeout(b.ctx, 15*time.Second)
	defer cancel()

	res := ConsoleResult{}
	b.consoleMu.Lock()
	res.Messages = append([]string(nil), b.consoleLogs...)
	res.Exceptions = append([]string(nil), b.consoleErrs...)
	if clear {
		b.consoleLogs = nil
		b.consoleErrs = nil
	}
	b.consoleMu.Unlock()

	expression = strings.TrimSpace(expression)
	if expression != "" {
		var out any
		if err := chromedp.Run(runCtx, chromedp.Evaluate(expression, &out)); err != nil {
			return res, fmt.Errorf("console eval: %w", err)
		}
		res.EvalResult = fmt.Sprint(out)
	}
	return res, nil
}

func (b *chromedpBackend) Screenshot(ctx context.Context) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	b.mu.Lock()
	defer b.mu.Unlock()

	runCtx, cancel := context.WithTimeout(b.ctx, 20*time.Second)
	defer cancel()
	var buf []byte
	if err := chromedp.Run(runCtx, chromedp.CaptureScreenshot(&buf)); err != nil {
		return nil, fmt.Errorf("screenshot: %w", err)
	}
	return buf, nil
}

func (b *chromedpBackend) CDP(ctx context.Context, method string, params map[string]any) (any, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	method = strings.TrimSpace(method)
	if method == "" {
		return nil, fmt.Errorf("method is required")
	}
	b.mu.Lock()
	defer b.mu.Unlock()

	runCtx, cancel := context.WithTimeout(b.ctx, 30*time.Second)
	defer cancel()

	var result any
	err := chromedp.Run(runCtx, chromedp.ActionFunc(func(ctx context.Context) error {
		c := chromedp.FromContext(ctx)
		if c == nil || c.Target == nil {
			return fmt.Errorf("no CDP target")
		}
		var raw map[string]any
		if err := c.Target.Execute(ctx, method, params, &raw); err != nil {
			return err
		}
		result = raw
		return nil
	}))
	if err != nil {
		return nil, fmt.Errorf("cdp %s: %w", method, err)
	}
	if result == nil {
		result = map[string]any{}
	}
	return result, nil
}

func (b *chromedpBackend) HandleDialog(ctx context.Context, action, promptText, dialogID string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	action = strings.ToLower(strings.TrimSpace(action))
	accept := action == "accept"
	if action != "accept" && action != "dismiss" {
		return fmt.Errorf("action must be accept or dismiss")
	}

	b.dialogMu.Lock()
	if len(b.dialogs) == 0 {
		b.dialogMu.Unlock()
		return fmt.Errorf("no pending dialog")
	}
	if dialogID != "" {
		found := false
		for _, d := range b.dialogs {
			if d.ID == dialogID {
				found = true
				break
			}
		}
		if !found {
			b.dialogMu.Unlock()
			return fmt.Errorf("dialog_id %q not found", dialogID)
		}
	}
	// Pop matching or first dialog after successful handle.
	b.dialogMu.Unlock()

	b.mu.Lock()
	defer b.mu.Unlock()
	runCtx, cancel := context.WithTimeout(b.ctx, 15*time.Second)
	defer cancel()

	act := page.HandleJavaScriptDialog(accept)
	if promptText != "" {
		act = act.WithPromptText(promptText)
	}
	if err := chromedp.Run(runCtx, act); err != nil {
		return fmt.Errorf("handle dialog: %w", err)
	}

	b.dialogMu.Lock()
	defer b.dialogMu.Unlock()
	if dialogID == "" {
		if len(b.dialogs) > 0 {
			b.dialogs = b.dialogs[1:]
		}
		return nil
	}
	out := b.dialogs[:0]
	for _, d := range b.dialogs {
		if d.ID != dialogID {
			out = append(out, d)
		}
	}
	b.dialogs = out
	return nil
}

func (b *chromedpBackend) pendingDialogsCopy() []DialogInfo {
	b.dialogMu.Lock()
	defer b.dialogMu.Unlock()
	if len(b.dialogs) == 0 {
		return nil
	}
	out := make([]DialogInfo, len(b.dialogs))
	copy(out, b.dialogs)
	return out
}

func (b *chromedpBackend) Close(ctx context.Context) error {
	_ = ctx
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.tabCancel != nil {
		b.tabCancel()
		b.tabCancel = nil
	}
	if b.allocCancel != nil {
		b.allocCancel()
		b.allocCancel = nil
	}
	b.refs = nil
	return nil
}

// Healthy probes CDP with a short about:blank navigation.
func (b *chromedpBackend) Healthy(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	b.mu.Lock()
	defer b.mu.Unlock()

	runCtx, cancel := context.WithTimeout(b.ctx, 8*time.Second)
	defer cancel()

	if err := chromedp.Run(runCtx, chromedp.Navigate("about:blank")); err != nil {
		return fmt.Errorf("chromedp unhealthy: %w", err)
	}
	return nil
}

func (b *chromedpBackend) resolveRef(ref string) (string, error) {
	if sel, ok := b.refs[ref]; ok && sel != "" {
		return sel, nil
	}
	return "", fmt.Errorf("unknown ref %q (run Snapshot/Navigate first)", ref)
}

type snapshotElement struct {
	Ref      string `json:"ref"`
	Selector string `json:"selector"`
	Label    string `json:"label"`
}

func (b *chromedpBackend) snapshotLocked(runCtx context.Context, full bool) (Snapshot, error) {
	var (
		url, title, bodyText string
		elements             []snapshotElement
	)

	collectJS := `(function(){
  const out = [];
  const nodes = document.querySelectorAll(
    'a[href], button, input, textarea, select, [role="button"], [onclick]'
  );
  nodes.forEach(function(el, i) {
    const ref = '@e' + (i + 1);
    el.setAttribute('data-sixath-ref', ref);
    const label = (
      el.getAttribute('aria-label') ||
      el.getAttribute('placeholder') ||
      el.getAttribute('name') ||
      el.getAttribute('value') ||
      (el.innerText || '') ||
      el.tagName
    ).toString().trim().replace(/\s+/g, ' ').slice(0, 80);
    out.push({
      ref: ref,
      selector: '[data-sixath-ref="' + ref + '"]',
      label: el.tagName.toLowerCase() + (label ? ': ' + label : '')
    });
  });
  return out;
})()`

	actions := []chromedp.Action{
		chromedp.Location(&url),
		chromedp.Title(&title),
		chromedp.Evaluate(collectJS, &elements),
	}
	if full {
		actions = append(actions, chromedp.Text("body", &bodyText, chromedp.ByQuery))
	} else {
		actions = append(actions, chromedp.Evaluate(
			`document.body ? (document.body.innerText || '').slice(0, 8000) : ''`,
			&bodyText,
		))
	}

	if err := chromedp.Run(runCtx, actions...); err != nil {
		return Snapshot{}, fmt.Errorf("snapshot: %w", err)
	}

	refs := make(map[string]string, len(elements))
	refIndex := ""
	for _, el := range elements {
		refs[el.Ref] = el.Selector
		if refIndex != "" {
			refIndex += "\n"
		}
		refIndex += el.Ref + " " + el.Label
	}
	b.refs = refs

	text := bodyText
	if refIndex != "" {
		if text != "" {
			text = text + "\n\n" + refIndex
		} else {
			text = refIndex
		}
	}

	return Snapshot{
		URL:            url,
		Title:          title,
		Text:           text,
		Refs:           refs,
		PendingDialogs: b.pendingDialogsCopy(),
	}, nil
}
