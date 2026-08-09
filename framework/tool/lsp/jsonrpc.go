package lsp

import (
	"bufio"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// Conn wraps an io.ReadWriter with LSP Content-Length framing.
type Conn struct {
	r *bufio.Reader
	w io.Writer
}

// NewConn returns a Conn over rw.
func NewConn(rw io.ReadWriter) *Conn {
	return NewConnFrom(rw, rw)
}

// NewConnFrom returns a Conn with separate reader and writer.
func NewConnFrom(r io.Reader, w io.Writer) *Conn {
	return &Conn{
		r: bufio.NewReader(r),
		w: w,
	}
}

// WriteMessage writes a JSON-RPC body with Content-Length framing.
func (c *Conn) WriteMessage(body []byte) error {
	header := fmt.Sprintf("Content-Length: %d\r\n\r\n", len(body))
	if _, err := c.w.Write([]byte(header)); err != nil {
		return err
	}
	_, err := c.w.Write(body)
	return err
}

// ReadMessage reads the next framed JSON-RPC body.
func (c *Conn) ReadMessage() ([]byte, error) {
	contentLength := -1
	for {
		line, err := c.r.ReadString('\n')
		if err != nil {
			return nil, err
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			break
		}
		const prefix = "Content-Length:"
		if strings.HasPrefix(line, prefix) {
			n, err := strconv.Atoi(strings.TrimSpace(line[len(prefix):]))
			if err != nil {
				return nil, fmt.Errorf("lsp: invalid Content-Length: %w", err)
			}
			contentLength = n
		}
	}
	if contentLength < 0 {
		return nil, fmt.Errorf("lsp: missing Content-Length header")
	}
	body := make([]byte, contentLength)
	if _, err := io.ReadFull(c.r, body); err != nil {
		return nil, err
	}
	return body, nil
}
