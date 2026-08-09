package lsp

import (
	"fmt"
	"net/url"
	"path/filepath"
	"strings"
)

// PathToURI converts a local filesystem path to a file:// URI.
func PathToURI(path string) string {
	if path == "" {
		return ""
	}
	path = filepath.ToSlash(filepath.Clean(path))
	u := url.URL{Scheme: "file"}
	if strings.HasPrefix(path, "/") {
		u.Path = path
		return u.String()
	}
	u.Path = "/" + path
	return u.String()
}

// URIToPath converts a file:// URI to a local filesystem path.
func URIToPath(uri string) (string, error) {
	u, err := url.Parse(uri)
	if err != nil {
		return "", err
	}
	if u.Scheme != "file" {
		return "", fmt.Errorf("lsp: URI scheme is not file: %q", u.Scheme)
	}
	path := u.Path
	if path == "" {
		path = u.Opaque
	}
	if len(path) >= 3 && path[0] == '/' && path[2] == ':' {
		path = path[1:]
	}
	return filepath.FromSlash(path), nil
}
