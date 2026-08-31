package chat

import (
	"bufio"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const (
	resultFileMaxBytes = 8 << 20
	resultFileMaxLines = 10000
)

var (
	ErrResultPathInvalid = errors.New("invalid result path")
	ErrResultFileTooBig  = errors.New("result file too large")
)

// ResolveSessionResultAbs maps a workspace-relative spill path to an absolute
// file under {workspace}/tmp/results/{sessionID}/. Rejects traversal and other sessions.
func ResolveSessionResultAbs(workspace, sessionID, rel string) (string, error) {
	workspace = strings.TrimSpace(workspace)
	sessionID = strings.TrimSpace(sessionID)
	rel = strings.ReplaceAll(strings.TrimSpace(rel), "\\", "/")
	rel = strings.TrimPrefix(rel, "/")
	if workspace == "" || sessionID == "" || rel == "" {
		return "", ErrResultPathInvalid
	}
	if strings.Contains(rel, "..") {
		return "", ErrResultPathInvalid
	}
	prefix := "tmp/results/" + sessionID + "/"
	if !strings.HasPrefix(rel, prefix) {
		return "", ErrResultPathInvalid
	}
	if !strings.HasSuffix(strings.ToLower(rel), ".jsonl") {
		return "", ErrResultPathInvalid
	}
	abs := filepath.Clean(filepath.Join(workspace, filepath.FromSlash(rel)))
	wantRoot := filepath.Clean(filepath.Join(workspace, "tmp", "results", sessionID))
	sep := string(os.PathSeparator)
	if abs != wantRoot && !strings.HasPrefix(abs, wantRoot+sep) {
		return "", ErrResultPathInvalid
	}
	return abs, nil
}

// ReadJSONLResultFile reads objects from a jsonl spill (one JSON object per line).
func ReadJSONLResultFile(abs string) ([]map[string]any, error) {
	st, err := os.Stat(abs)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, err
		}
		return nil, err
	}
	if st.Size() > resultFileMaxBytes {
		return nil, ErrResultFileTooBig
	}
	f, err := os.Open(abs)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	buf := make([]byte, 0, 64*1024)
	sc.Buffer(buf, 1<<20)
	out := make([]map[string]any, 0, 256)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var row map[string]any
		if err := json.Unmarshal([]byte(line), &row); err != nil {
			out = append(out, map[string]any{"line": line})
			continue
		}
		out = append(out, row)
		if len(out) >= resultFileMaxLines {
			break
		}
	}
	if err := sc.Err(); err != nil && !errors.Is(err, io.EOF) {
		return nil, err
	}
	return out, nil
}
