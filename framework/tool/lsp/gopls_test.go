package lsp

import (
	"context"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestGoplsServer_NavigationParsesLocationsAndOpensDocument(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	file := filepath.Join(root, "main.go")
	if err := os.WriteFile(file, []byte("package example\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "outside.go")
	client, server := net.Pipe()
	defer server.Close()

	go serveNavigation(t, server, PathToURI(filepath.Join(NormalizeRoot(root), "main.go")), PathToURI(outside))

	gopls := newGoplsServer(NewConn(client), nil, ServerOpts{
		InitTimeout:    time.Second,
		RequestTimeout: time.Second,
	})
	defer gopls.Close(context.Background())

	ctx := context.Background()
	if err := gopls.EnsureReady(ctx, root); err != nil {
		t.Fatalf("EnsureReady: %v", err)
	}

	definitions, err := gopls.Definition(ctx, root, "main.go", Position{Line: 2, Character: 3})
	if err != nil {
		t.Fatalf("Definition: %v", err)
	}
	if len(definitions) != 1 || definitions[0].File != "main.go" || definitions[0].Line != 5 {
		t.Fatalf("unexpected definition locations: %+v", definitions)
	}

	references, err := gopls.References(ctx, root, "main.go", Position{Line: 2, Character: 3}, true)
	if err != nil {
		t.Fatalf("References: %v", err)
	}
	if len(references) != 2 || references[0].Line != 8 || references[1].File != filepath.ToSlash(outside) {
		t.Fatalf("unexpected reference locations: %+v", references)
	}
	again, err := gopls.Definition(ctx, root, "main.go", Position{Line: 2, Character: 3})
	if err != nil {
		t.Fatalf("second Definition: %v", err)
	}
	if len(again) != 1 || again[0].Line != 12 {
		t.Fatalf("unexpected single definition location: %+v", again)
	}
}

func TestGoplsServer_MissingCapabilityIsPermanent(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	client, server := net.Pipe()
	defer server.Close()
	go func() {
		conn := NewConn(server)
		request := readRPC(t, conn)
		writeRPC(t, conn, map[string]any{
			"jsonrpc": "2.0",
			"id":      request["id"],
			"result": map[string]any{
				"capabilities": map[string]any{},
			},
		})
		_ = readRPC(t, conn) // initialized
		shutdown := readRPC(t, conn)
		writeRPC(t, conn, map[string]any{"jsonrpc": "2.0", "id": shutdown["id"], "result": nil})
		_ = readRPC(t, conn) // exit
	}()

	gopls := newGoplsServer(NewConn(client), nil, ServerOpts{InitTimeout: time.Second})
	defer gopls.Close(context.Background())
	if err := gopls.EnsureReady(context.Background(), root); err != nil {
		t.Fatalf("EnsureReady: %v", err)
	}
	if _, err := gopls.Definition(context.Background(), root, "main.go", Position{}); !IsPermanentCapabilityError(err) {
		t.Fatalf("Definition error = %v, want permanent capability error", err)
	}
}

func serveNavigation(t *testing.T, rw net.Conn, fileURI, outsideURI string) {
	t.Helper()
	defer rw.Close()
	conn := NewConn(rw)

	initialize := readRPC(t, conn)
	if initialize["method"] != "initialize" {
		t.Errorf("first method = %v, want initialize", initialize["method"])
		return
	}
	writeRPC(t, conn, map[string]any{
		"jsonrpc": "2.0",
		"id":      initialize["id"],
		"result": map[string]any{
			"capabilities": map[string]any{
				"definitionProvider": true,
				"referencesProvider": true,
			},
		},
	})
	if initialized := readRPC(t, conn); initialized["method"] != "initialized" {
		t.Errorf("method = %v, want initialized", initialized["method"])
		return
	}

	methods := []string{"textDocument/definition", "textDocument/references", "textDocument/definition"}
	for index, response := range []any{
		[]any{map[string]any{
			"targetUri":   fileURI,
			"targetRange": map[string]any{"start": map[string]any{"line": 4, "character": 2}},
		}},
		[]any{
			map[string]any{
				"uri":   fileURI,
				"range": map[string]any{"start": map[string]any{"line": 7, "character": 1}},
			},
			map[string]any{
				"uri":   outsideURI,
				"range": map[string]any{"start": map[string]any{"line": 9, "character": 0}},
			},
		},
		map[string]any{
			"uri":   fileURI,
			"range": map[string]any{"start": map[string]any{"line": 11, "character": 0}},
		},
	} {
		opened := readRPC(t, conn)
		if opened["method"] != "textDocument/didOpen" {
			t.Errorf("method = %v, want textDocument/didOpen", opened["method"])
			return
		}
		document := opened["params"].(map[string]any)["textDocument"].(map[string]any)
		if document["uri"] != fileURI || document["text"] != "package example\n" {
			t.Errorf("unexpected didOpen document: %#v", document)
			return
		}
		request := readRPC(t, conn)
		if request["method"] != methods[index] {
			t.Errorf("method = %v, want %s", request["method"], methods[index])
			return
		}
		position := request["params"].(map[string]any)["position"].(map[string]any)
		if position["line"] != float64(2) || position["character"] != float64(3) {
			t.Errorf("unexpected request position: %#v", position)
			return
		}
		writeRPC(t, conn, map[string]any{"jsonrpc": "2.0", "id": request["id"], "result": response})
	}
	shutdown := readRPC(t, conn)
	writeRPC(t, conn, map[string]any{"jsonrpc": "2.0", "id": shutdown["id"], "result": nil})
	_ = readRPC(t, conn) // exit
}

func readRPC(t *testing.T, conn *Conn) map[string]any {
	t.Helper()
	body, err := conn.ReadMessage()
	if err != nil {
		t.Errorf("read message: %v", err)
		return nil
	}
	var message map[string]any
	if err := json.Unmarshal(body, &message); err != nil {
		t.Errorf("decode message: %v", err)
	}
	return message
}

func writeRPC(t *testing.T, conn *Conn, message map[string]any) {
	t.Helper()
	body, err := json.Marshal(message)
	if err != nil {
		t.Errorf("marshal message: %v", err)
		return
	}
	if err := conn.WriteMessage(body); err != nil {
		t.Errorf("write message: %v", err)
	}
}
