package chat

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/sixath/framework/tool"
	"github.com/sixath/framework/tool/browser"
)

var (
	browserStoreMu sync.RWMutex
	browserStore   = browser.NewSessionStore()

	browserFactoryMu sync.RWMutex
	browserFactory   = defaultChromedpFactory
)

func defaultChromedpFactory() (browser.Backend, error) {
	return browser.NewChromedpBackendWithDownload(context.Background(), browserDownloadConfigFromEnv(""))
}

// browserDownloadConfigFromEnv reads SATH_BROWSER_DOWNLOAD=deny|workspace (default deny).
// workspaceRoot when non-empty resolves downloads under that root.
func browserDownloadConfigFromEnv(workspaceRoot string) browser.DownloadConfig {
	mode := browser.DownloadDeny
	v := strings.ToLower(strings.TrimSpace(os.Getenv("SATH_BROWSER_DOWNLOAD")))
	if v == "workspace" || v == "allow" {
		mode = browser.DownloadWorkspace
	}
	dir := "downloads"
	if workspaceRoot != "" {
		dir = filepath.Join(workspaceRoot, "downloads")
	}
	return browser.DownloadConfig{Mode: mode, Dir: dir}
}

// BrowserSessionStore returns the process-level browser SessionStore shared by
// runtime tool registration and ChatSession end hooks.
func BrowserSessionStore() *browser.SessionStore {
	browserStoreMu.RLock()
	defer browserStoreMu.RUnlock()
	return browserStore
}

// SetBrowserSessionStore replaces the process-level store (for tests).
func SetBrowserSessionStore(store *browser.SessionStore) {
	browserStoreMu.Lock()
	defer browserStoreMu.Unlock()
	if store == nil {
		browserStore = browser.NewSessionStore()
		return
	}
	browserStore = store
}

// SetBrowserBackendFactory replaces the Backend factory used when registering
// browser tools (for tests; Fake factory avoids launching Chrome).
func SetBrowserBackendFactory(factory func() (browser.Backend, error)) {
	browserFactoryMu.Lock()
	defer browserFactoryMu.Unlock()
	if factory == nil {
		browserFactory = defaultChromedpFactory
		return
	}
	browserFactory = factory
}

// BrowserBackendFactory returns the current Backend factory.
func BrowserBackendFactory() func() (browser.Backend, error) {
	browserFactoryMu.RLock()
	defer browserFactoryMu.RUnlock()
	return browserFactory
}

// RegisterBrowserRuntimeTools registers browser_* tools when enabled.
// store/factory nil fall back to process-level defaults.
// Optional vision enables LLM analysis on browser_vision and registers vision_analyze.
func RegisterBrowserRuntimeTools(reg *tool.Registry, enabled bool, store *browser.SessionStore, factory func() (browser.Backend, error), vision ...tool.VisionAnalyzer) error {
	if reg == nil || !enabled {
		return nil
	}
	if store == nil {
		store = BrowserSessionStore()
	}
	if factory == nil {
		factory = BrowserBackendFactory()
	}
	var analyzer tool.VisionAnalyzer
	if len(vision) > 0 {
		analyzer = vision[0]
	}
	return tool.RegisterBrowserToolsWithConfig(reg, store, factory, &tool.BrowserToolsConfig{
		PendingStore:    tool.NewInMemoryBrowserPendingStore(),
		TokenGen:        tool.RandomTokenGenerator{},
		Vision:          analyzer,
		ConfirmNavigate: browserConfirmNavigateFromEnv(),
	})
}

func browserConfirmNavigateFromEnv() bool {
	v := strings.ToLower(strings.TrimSpace(os.Getenv("SATH_BROWSER_CONFIRM_NAVIGATE")))
	return v == "1" || v == "true" || v == "yes"
}
