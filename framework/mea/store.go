package mea

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"unicode/utf8"
)

// ErrNotFound is returned when Load cannot find a session file.
var ErrNotFound = errors.New("mea: session not found")

const maxSessionIDLen = 128

// FileStore persists TaskState as JSON files under a root directory.
type FileStore struct {
	root string
}

// NewFileStore returns a FileStore rooted at root.
// An empty root is allowed at construction; Save/Load will return an error.
func NewFileStore(root string) *FileStore {
	return &FileStore{root: root}
}

func (s *FileStore) Save(state TaskState) error {
	if s == nil || s.root == "" {
		return errors.New("mea: empty store root")
	}
	id, err := sanitizeSessionID(state.SessionID)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(s.root, 0o755); err != nil {
		return err
	}
	path := filepath.Join(s.root, id+".json")
	tmp := path + ".tmp"
	b, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

func (s *FileStore) Load(sessionID string) (TaskState, error) {
	if s == nil || s.root == "" {
		return TaskState{}, errors.New("mea: empty store root")
	}
	id, err := sanitizeSessionID(sessionID)
	if err != nil {
		return TaskState{}, err
	}
	path := filepath.Join(s.root, id+".json")
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return TaskState{}, fmt.Errorf("%w: %s", ErrNotFound, sessionID)
		}
		return TaskState{}, err
	}
	var state TaskState
	if err := json.Unmarshal(b, &state); err != nil {
		return TaskState{}, err
	}
	return state, nil
}

func sanitizeSessionID(id string) (string, error) {
	if id == "" {
		return "", errors.New("mea: empty session id")
	}
	if utf8.RuneCountInString(id) > maxSessionIDLen {
		return "", fmt.Errorf("mea: session id too long (max %d)", maxSessionIDLen)
	}
	for _, r := range id {
		switch {
		case r >= 'A' && r <= 'Z':
		case r >= 'a' && r <= 'z':
		case r >= '0' && r <= '9':
		case r == '.' || r == '_' || r == '-':
		default:
			return "", fmt.Errorf("mea: invalid session id %q", id)
		}
	}
	// Defense in depth: reject path separators and ".." even if charset already blocks them.
	for i := 0; i < len(id); i++ {
		c := id[i]
		if c == '/' || c == '\\' {
			return "", fmt.Errorf("mea: invalid session id %q", id)
		}
	}
	if id == ".." || containsDotDot(id) {
		return "", fmt.Errorf("mea: invalid session id %q", id)
	}
	return id, nil
}

func containsDotDot(id string) bool {
	// Charset allows '.' so ".." as a segment (or whole id) must be rejected explicitly.
	for i := 0; i+1 < len(id); i++ {
		if id[i] == '.' && id[i+1] == '.' {
			return true
		}
	}
	return false
}
