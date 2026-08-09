package lsp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// ErrPermanentCapabilityMissing indicates that the language server does not
// advertise a navigation capability required by the caller.
var ErrPermanentCapabilityMissing = errors.New("lsp: permanent capability missing")

const (
	defaultInitTimeout    = time.Minute
	defaultRequestTimeout = 30 * time.Second
)

// IsPermanentCapabilityError reports whether err is a missing LSP capability.
func IsPermanentCapabilityError(err error) bool {
	return errors.Is(err, ErrPermanentCapabilityMissing)
}

// GoplsFactory returns a factory for gopls language servers.
func GoplsFactory(_ ServerOpts) ServerFactory {
	return StartGopls
}

// StartGopls starts gopls for root and initializes its LSP session.
func StartGopls(ctx context.Context, root string, opts ServerOpts) (LanguageServer, error) {
	command := opts.Command
	if command == "" {
		command = "gopls"
	}

	cmd := exec.Command(command)
	cmd.Dir = root
	if len(opts.Env) > 0 {
		cmd.Env = append(os.Environ(), opts.Env...)
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("lsp: create gopls stdin: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("lsp: create gopls stdout: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("lsp: start %s: %w", command, err)
	}

	server := newGoplsServer(NewConnFrom(stdout, stdin), cmd, opts)
	if err := server.EnsureReady(ctx, root); err != nil {
		_ = server.Close(context.Background())
		return nil, err
	}
	return server, nil
}

// GoplsServer is a stdio LSP client backed by a gopls process.
type GoplsServer struct {
	conn *Conn
	cmd  *exec.Cmd
	opts ServerOpts

	mu          sync.Mutex
	root        string
	initialized bool
	closed      bool
	nextID      int

	definitionSupported bool
	referencesSupported bool
}

func newGoplsServer(conn *Conn, cmd *exec.Cmd, opts ServerOpts) *GoplsServer {
	return &GoplsServer{conn: conn, cmd: cmd, opts: opts, nextID: 1}
}

// EnsureReady initializes the gopls session once.
func (s *GoplsServer) EnsureReady(ctx context.Context, root string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return errors.New("lsp: gopls server is closed")
	}
	if s.initialized {
		return nil
	}

	normalizedRoot := NormalizeRoot(root)
	result, err := s.requestLocked(ctx, timeoutOrDefault(s.opts.InitTimeout, defaultInitTimeout), "initialize", map[string]any{
		"processId": nil,
		"rootUri":   PathToURI(normalizedRoot),
		"workspaceFolders": []map[string]string{{
			"uri":  PathToURI(normalizedRoot),
			"name": filepath.Base(normalizedRoot),
		}},
		"capabilities": map[string]any{},
	})
	if err != nil {
		return fmt.Errorf("lsp: initialize gopls: %w", err)
	}

	var initialize struct {
		Capabilities struct {
			DefinitionProvider json.RawMessage `json:"definitionProvider"`
			ReferencesProvider json.RawMessage `json:"referencesProvider"`
		} `json:"capabilities"`
	}
	if err := json.Unmarshal(result, &initialize); err != nil {
		return fmt.Errorf("lsp: decode initialize result: %w", err)
	}
	s.definitionSupported = providerEnabled(initialize.Capabilities.DefinitionProvider)
	s.referencesSupported = providerEnabled(initialize.Capabilities.ReferencesProvider)
	if err := s.notifyLocked("initialized", map[string]any{}); err != nil {
		return fmt.Errorf("lsp: send initialized notification: %w", err)
	}
	s.root = normalizedRoot
	s.initialized = true
	return nil
}

// Definition returns locations for the symbol at pos.
func (s *GoplsServer) Definition(ctx context.Context, root, relPath string, pos Position) ([]Location, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.initialized {
		return nil, errors.New("lsp: gopls server is not initialized")
	}
	if !s.definitionSupported {
		return nil, fmt.Errorf("%w: definitionProvider", ErrPermanentCapabilityMissing)
	}
	if err := s.didOpenLocked(relPath); err != nil {
		return nil, err
	}
	result, err := s.requestLocked(ctx, timeoutOrDefault(s.opts.RequestTimeout, defaultRequestTimeout), "textDocument/definition", textDocumentPositionParams(s.root, relPath, pos))
	if err != nil {
		return nil, fmt.Errorf("lsp: definition: %w", err)
	}
	return decodeLocations(result, root)
}

// References returns all references for the symbol at pos.
func (s *GoplsServer) References(ctx context.Context, root, relPath string, pos Position, includeDeclaration bool) ([]Location, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.initialized {
		return nil, errors.New("lsp: gopls server is not initialized")
	}
	if !s.referencesSupported {
		return nil, fmt.Errorf("%w: referencesProvider", ErrPermanentCapabilityMissing)
	}
	if err := s.didOpenLocked(relPath); err != nil {
		return nil, err
	}
	params := textDocumentPositionParams(s.root, relPath, pos)
	params["context"] = map[string]bool{"includeDeclaration": includeDeclaration}
	result, err := s.requestLocked(ctx, timeoutOrDefault(s.opts.RequestTimeout, defaultRequestTimeout), "textDocument/references", params)
	if err != nil {
		return nil, fmt.Errorf("lsp: references: %w", err)
	}
	return decodeLocations(result, root)
}

// Close shuts down gopls and kills it if graceful exit does not complete.
func (s *GoplsServer) Close(ctx context.Context) error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true

	var firstErr error
	initialized := s.initialized
	if s.initialized {
		if _, err := s.requestLocked(ctx, timeoutOrDefault(s.opts.RequestTimeout, defaultRequestTimeout), "shutdown", nil); err != nil {
			firstErr = fmt.Errorf("lsp: shutdown gopls: %w", err)
		}
		if err := s.notifyLocked("exit", nil); err != nil && firstErr == nil {
			firstErr = fmt.Errorf("lsp: send gopls exit: %w", err)
		}
	}
	s.mu.Unlock()

	if s.cmd != nil && s.cmd.Process != nil {
		if !initialized {
			if err := s.cmd.Process.Kill(); err != nil && firstErr == nil {
				firstErr = fmt.Errorf("lsp: kill uninitialized gopls: %w", err)
			}
		}
		waited := make(chan error, 1)
		go func() { waited <- s.cmd.Wait() }()
		waitCtx, cancel := context.WithTimeout(ctx, timeoutOrDefault(s.opts.RequestTimeout, defaultRequestTimeout))
		defer cancel()
		select {
		case err := <-waited:
			if err != nil && firstErr == nil {
				firstErr = fmt.Errorf("lsp: wait for gopls: %w", err)
			}
		case <-waitCtx.Done():
			if err := s.cmd.Process.Kill(); err != nil && firstErr == nil {
				firstErr = fmt.Errorf("lsp: kill gopls: %w", err)
			}
			<-waited
		}
	}
	return firstErr
}

func timeoutOrDefault(value, fallback time.Duration) time.Duration {
	if value > 0 {
		return value
	}
	return fallback
}

func (s *GoplsServer) didOpenLocked(relPath string) error {
	path := filepath.Join(s.root, relPath)
	text, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("lsp: read %s: %w", relPath, err)
	}
	return s.notifyLocked("textDocument/didOpen", map[string]any{
		"textDocument": map[string]any{
			"uri":        PathToURI(path),
			"languageId": "go",
			"version":    1,
			"text":       string(text),
		},
	})
}

func (s *GoplsServer) requestLocked(ctx context.Context, timeout time.Duration, method string, params any) (json.RawMessage, error) {
	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}
	id := s.nextID
	s.nextID++
	if err := s.writeLocked(map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params}); err != nil {
		return nil, err
	}

	type response struct {
		JSONRPC string          `json:"jsonrpc"`
		ID      int             `json:"id"`
		Result  json.RawMessage `json:"result"`
		Error   *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	for {
		read := make(chan struct {
			body []byte
			err  error
		}, 1)
		go func() {
			body, err := s.conn.ReadMessage()
			read <- struct {
				body []byte
				err  error
			}{body, err}
		}()
		select {
		case message := <-read:
			if message.err != nil {
				return nil, message.err
			}
			var reply response
			if err := json.Unmarshal(message.body, &reply); err != nil {
				return nil, fmt.Errorf("decode JSON-RPC response: %w", err)
			}
			if reply.ID != id {
				continue // Ignore LSP notifications and responses to abandoned requests.
			}
			if reply.Error != nil {
				return nil, fmt.Errorf("JSON-RPC error %d: %s", reply.Error.Code, reply.Error.Message)
			}
			return reply.Result, nil
		case <-ctx.Done():
			if s.cmd != nil && s.cmd.Process != nil {
				_ = s.cmd.Process.Kill()
			}
			return nil, ctx.Err()
		}
	}
}

func (s *GoplsServer) notifyLocked(method string, params any) error {
	return s.writeLocked(map[string]any{"jsonrpc": "2.0", "method": method, "params": params})
}

func (s *GoplsServer) writeLocked(message any) error {
	body, err := json.Marshal(message)
	if err != nil {
		return err
	}
	return s.conn.WriteMessage(body)
}

func providerEnabled(raw json.RawMessage) bool {
	if len(raw) == 0 || string(raw) == "null" || string(raw) == "false" {
		return false
	}
	return true
}

func textDocumentPositionParams(root, relPath string, pos Position) map[string]any {
	return map[string]any{
		"textDocument": map[string]string{"uri": PathToURI(filepath.Join(root, relPath))},
		"position":     pos,
	}
}

func decodeLocations(result json.RawMessage, root string) ([]Location, error) {
	if string(result) == "null" || len(result) == 0 {
		return nil, nil
	}
	var values []json.RawMessage
	if result[0] == '[' {
		if err := json.Unmarshal(result, &values); err != nil {
			return nil, fmt.Errorf("lsp: decode locations: %w", err)
		}
	} else {
		values = []json.RawMessage{result}
	}

	locations := make([]Location, 0, len(values))
	for _, value := range values {
		var raw struct {
			URI       string `json:"uri"`
			TargetURI string `json:"targetUri"`
			Range     *struct {
				Start Position `json:"start"`
			} `json:"range"`
			TargetRange *struct {
				Start Position `json:"start"`
			} `json:"targetRange"`
		}
		if err := json.Unmarshal(value, &raw); err != nil {
			return nil, fmt.Errorf("lsp: decode location: %w", err)
		}
		uri := raw.URI
		position := raw.Range
		if uri == "" {
			uri = raw.TargetURI
			position = raw.TargetRange
		}
		if uri == "" || position == nil {
			return nil, errors.New("lsp: location is missing URI or range")
		}
		path, err := URIToPath(uri)
		if err != nil {
			return nil, fmt.Errorf("lsp: decode location URI: %w", err)
		}
		file := filepath.ToSlash(path)
		if relative, err := filepath.Rel(root, path); err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			file = filepath.ToSlash(relative)
		}
		locations = append(locations, Location{
			Repo:      filepath.Base(root),
			File:      file,
			Line:      position.Start.Line + 1,
			Character: position.Start.Character,
		})
	}
	return locations, nil
}

var _ LanguageServer = (*GoplsServer)(nil)
