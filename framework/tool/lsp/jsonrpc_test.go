package lsp

import (
	"bytes"
	"fmt"
	"io"
	"testing"
)

func TestJSONRPC_WriteReadRoundTrip(t *testing.T) {
	var buf bytes.Buffer
	conn := NewConn(&buf)

	body := []byte(`{"jsonrpc":"2.0","id":1,"method":"initialize"}`)
	if err := conn.WriteMessage(body); err != nil {
		t.Fatal(err)
	}

	got, err := NewConn(&buf).ReadMessage()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, body) {
		t.Fatalf("got %q want %q", got, body)
	}
}

func TestJSONRPC_StickyPackets(t *testing.T) {
	var buf bytes.Buffer
	conn := NewConn(&buf)

	body1 := []byte(`{"jsonrpc":"2.0","id":1}`)
	body2 := []byte(`{"jsonrpc":"2.0","id":2}`)
	if err := conn.WriteMessage(body1); err != nil {
		t.Fatal(err)
	}
	if err := conn.WriteMessage(body2); err != nil {
		t.Fatal(err)
	}

	reader := NewConn(&buf)
	got1, err := reader.ReadMessage()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got1, body1) {
		t.Fatalf("first message: got %q want %q", got1, body1)
	}

	got2, err := reader.ReadMessage()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got2, body2) {
		t.Fatalf("second message: got %q want %q", got2, body2)
	}
}

func TestJSONRPC_PartialReads(t *testing.T) {
	body := []byte(`{"jsonrpc":"2.0","id":3,"method":"shutdown"}`)
	var buf bytes.Buffer
	if err := NewConn(&buf).WriteMessage(body); err != nil {
		t.Fatal(err)
	}

	reader := NewConnFrom(&chunkReader{data: buf.Bytes(), chunk: 5}, io.Discard)
	got, err := reader.ReadMessage()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, body) {
		t.Fatalf("got %q want %q", got, body)
	}
}

func TestJSONRPC_MissingContentLength(t *testing.T) {
	buf := bytes.NewBufferString("\r\n{\"jsonrpc\":\"2.0\"}")
	_, err := NewConn(buf).ReadMessage()
	if err == nil {
		t.Fatal("expected error for missing Content-Length header")
	}
}

func TestJSONRPC_FramingFormat(t *testing.T) {
	var buf bytes.Buffer
	body := []byte(`{"a":1}`)
	if err := NewConn(&buf).WriteMessage(body); err != nil {
		t.Fatal(err)
	}

	want := fmt.Sprintf("Content-Length: %d\r\n\r\n%s", len(body), body)
	if buf.String() != want {
		t.Fatalf("got framing %q want %q", buf.String(), want)
	}
}

// chunkReader returns data in fixed-size chunks to simulate partial TCP reads.
type chunkReader struct {
	data  []byte
	chunk int
	pos   int
}

func (c *chunkReader) Read(p []byte) (int, error) {
	if c.pos >= len(c.data) {
		return 0, io.EOF
	}
	n := c.chunk
	if n > len(p) {
		n = len(p)
	}
	if remain := len(c.data) - c.pos; n > remain {
		n = remain
	}
	copy(p, c.data[c.pos:c.pos+n])
	c.pos += n
	return n, nil
}
