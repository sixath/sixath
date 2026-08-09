package browser

import (
	"context"
	"fmt"
	"sync"
)

// TypedInput records a Type call on FakeBackend.
type TypedInput struct {
	Ref  string
	Text string
}

// FakeBackend is a test double that records calls and returns configurable snapshots.
type FakeBackend struct {
	PageSnapshot   Snapshot
	HealthyErr     error
	NavigatedURLs  []string
	ClickedRefs    []string
	TypedInputs    []TypedInput
	ScrollDirs     []string
	BackCount      int
	PressedKeys    []string
	Images         []ImageInfo
	ConsoleMsgs    []string
	ConsoleExc     []string
	EvalResults    map[string]string
	ScreenshotPNG  []byte
	LastExpression string
	CDPCalls       []CDPCall
	CDPResult      any
	DialogQueue    []DialogInfo
	HandledDialogs []DialogHandle

	mu     sync.Mutex
	closed bool
}

// CDPCall records a FakeBackend.CDP invocation.
type CDPCall struct {
	Method string
	Params map[string]any
}

// DialogHandle records HandleDialog.
type DialogHandle struct {
	Action     string
	PromptText string
	DialogID   string
}

// NewFakeBackend returns a FakeBackend with empty defaults.
func NewFakeBackend() *FakeBackend {
	return &FakeBackend{
		EvalResults:   map[string]string{},
		ScreenshotPNG: []byte{0x89, 0x50, 0x4e, 0x47}, // minimal PNG magic
	}
}

func (f *FakeBackend) Navigate(ctx context.Context, url string) (Snapshot, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.NavigatedURLs = append(f.NavigatedURLs, url)
	snap := f.PageSnapshot
	snap.URL = url
	if snap.Refs == nil {
		snap.Refs = map[string]string{}
	}
	return snap, nil
}

func (f *FakeBackend) Snapshot(ctx context.Context, full bool) (Snapshot, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	snap := f.PageSnapshot
	if snap.Refs == nil {
		snap.Refs = map[string]string{}
	}
	if len(f.DialogQueue) > 0 {
		snap.PendingDialogs = append([]DialogInfo(nil), f.DialogQueue...)
	}
	return snap, nil
}

func (f *FakeBackend) Click(ctx context.Context, ref string) (Snapshot, error) {
	f.mu.Lock()
	f.ClickedRefs = append(f.ClickedRefs, ref)
	f.mu.Unlock()
	return f.Snapshot(ctx, false)
}

func (f *FakeBackend) Type(ctx context.Context, ref, text string) (Snapshot, error) {
	f.mu.Lock()
	f.TypedInputs = append(f.TypedInputs, TypedInput{Ref: ref, Text: text})
	f.mu.Unlock()
	return f.Snapshot(ctx, false)
}

func (f *FakeBackend) Scroll(ctx context.Context, direction string) (Snapshot, error) {
	f.mu.Lock()
	f.ScrollDirs = append(f.ScrollDirs, direction)
	f.mu.Unlock()
	return f.Snapshot(ctx, false)
}

func (f *FakeBackend) Back(ctx context.Context) (Snapshot, error) {
	f.mu.Lock()
	f.BackCount++
	f.mu.Unlock()
	return f.Snapshot(ctx, false)
}

func (f *FakeBackend) Press(ctx context.Context, key string) (Snapshot, error) {
	f.mu.Lock()
	f.PressedKeys = append(f.PressedKeys, key)
	f.mu.Unlock()
	return f.Snapshot(ctx, false)
}

func (f *FakeBackend) GetImages(ctx context.Context) ([]ImageInfo, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]ImageInfo, len(f.Images))
	copy(out, f.Images)
	return out, nil
}

func (f *FakeBackend) Console(ctx context.Context, clear bool, expression string) (ConsoleResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.LastExpression = expression
	res := ConsoleResult{
		Messages:   append([]string(nil), f.ConsoleMsgs...),
		Exceptions: append([]string(nil), f.ConsoleExc...),
	}
	if expression != "" {
		if f.EvalResults != nil {
			res.EvalResult = f.EvalResults[expression]
		}
	}
	if clear {
		f.ConsoleMsgs = nil
		f.ConsoleExc = nil
	}
	return res, nil
}

func (f *FakeBackend) Screenshot(ctx context.Context) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]byte, len(f.ScreenshotPNG))
	copy(out, f.ScreenshotPNG)
	return out, nil
}

func (f *FakeBackend) CDP(ctx context.Context, method string, params map[string]any) (any, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.CDPCalls = append(f.CDPCalls, CDPCall{Method: method, Params: params})
	if f.CDPResult != nil {
		return f.CDPResult, nil
	}
	return map[string]any{"ok": true, "method": method}, nil
}

func (f *FakeBackend) HandleDialog(ctx context.Context, action, promptText, dialogID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.HandledDialogs = append(f.HandledDialogs, DialogHandle{
		Action: action, PromptText: promptText, DialogID: dialogID,
	})
	if len(f.DialogQueue) == 0 {
		return fmt.Errorf("no pending dialog")
	}
	if dialogID != "" {
		found := -1
		for i, d := range f.DialogQueue {
			if d.ID == dialogID {
				found = i
				break
			}
		}
		if found < 0 {
			return fmt.Errorf("dialog_id %q not found", dialogID)
		}
		f.DialogQueue = append(f.DialogQueue[:found], f.DialogQueue[found+1:]...)
		return nil
	}
	f.DialogQueue = f.DialogQueue[1:]
	return nil
}

func (f *FakeBackend) Close(ctx context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.closed = true
	return nil
}

func (f *FakeBackend) Healthy(ctx context.Context) error {
	return f.HealthyErr
}

func (f *FakeBackend) Closed() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.closed
}
