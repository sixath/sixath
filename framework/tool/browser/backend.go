package browser

import "context"

// Snapshot is a compact page view for agent consumption.
type Snapshot struct {
	URL            string
	Title          string
	Text           string            // accessibility / compact text
	Refs           map[string]string // refID -> brief description e.g. @e1
	PendingDialogs []DialogInfo      // native JS dialogs blocking the page (B3)
}

// DialogInfo is a native JS dialog (alert/confirm/prompt/beforeunload).
type DialogInfo struct {
	ID      string `json:"id"`
	Type    string `json:"type"`
	Message string `json:"message"`
}

// ImageInfo is one image candidate on the page.
type ImageInfo struct {
	URL    string `json:"url"`
	Alt    string `json:"alt"`
	Width  int    `json:"width,omitempty"`
	Height int    `json:"height,omitempty"`
}

// ConsoleResult holds console messages and optional eval output.
type ConsoleResult struct {
	Messages   []string
	Exceptions []string
	EvalResult string
}

// DownloadMode controls CDP file downloads.
type DownloadMode string

const (
	DownloadDeny      DownloadMode = "deny"
	DownloadWorkspace DownloadMode = "workspace"
)

// DownloadConfig configures browser download behavior.
type DownloadConfig struct {
	Mode     DownloadMode
	Dir      string // workspace-relative; default "downloads"
	MaxBytes int64  // 0 → 50 << 20
}

// Backend abstracts browser automation (CDP or test fake).
type Backend interface {
	Navigate(ctx context.Context, url string) (Snapshot, error)
	Snapshot(ctx context.Context, full bool) (Snapshot, error)
	Click(ctx context.Context, ref string) (Snapshot, error)
	Type(ctx context.Context, ref, text string) (Snapshot, error)
	Scroll(ctx context.Context, direction string) (Snapshot, error)
	Back(ctx context.Context) (Snapshot, error)
	Press(ctx context.Context, key string) (Snapshot, error)
	GetImages(ctx context.Context) ([]ImageInfo, error)
	Console(ctx context.Context, clear bool, expression string) (ConsoleResult, error)
	Screenshot(ctx context.Context) ([]byte, error) // PNG bytes
	// CDP sends a raw DevTools Protocol method on the current page target (B3).
	CDP(ctx context.Context, method string, params map[string]any) (any, error)
	// HandleDialog accepts or dismisses a pending JS dialog (B3).
	HandleDialog(ctx context.Context, action, promptText, dialogID string) error
	Close(ctx context.Context) error
	Healthy(ctx context.Context) error
}
