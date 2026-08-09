package growth

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// ApplyPatchBatch validates path/op for the whole batch, then applies each patch in order
// against the current filesystem state. If a step fails after earlier steps succeeded,
// completed steps are rolled back in reverse order.
//
// OpPatch: Old must appear exactly once in the target file at apply time (strings.Replace(..., 1)).
// Writes use a temp file in the target directory + rename (same-volume atomicity).
func ApplyPatchBatch(workspaceRoot string, batch []Patch) (err error) {
	if err = ValidatePatchBatch(workspaceRoot, batch); err != nil {
		return err
	}
	if len(batch) == 0 {
		return nil
	}

	rootAbs, err := filepath.Abs(workspaceRoot)
	if err != nil {
		return fmt.Errorf("growth: resolve workspace root: %w", err)
	}
	rootClean := filepath.Clean(rootAbs)

	var rollbacks []rollback
	defer func() {
		if err == nil {
			return
		}
		for i := len(rollbacks) - 1; i >= 0; i-- {
			rollbacks[i].restore()
		}
	}()

	for i := range batch {
		p := batch[i]
		full := resolvedPath(rootClean, p.Path)
		switch p.Op {
		case OpCreate:
			if _, e := os.Stat(full); e == nil {
				err = fmt.Errorf("growth: apply[%d] create %q: file already exists", i, p.Path)
				return err
			} else if !os.IsNotExist(e) {
				err = fmt.Errorf("growth: apply[%d] create %q: %w", i, p.Path, e)
				return err
			}
			rollbacks = append(rollbacks, rollbackCreate(full))
			if err = atomicWriteFile(full, []byte(p.Content), 0o644); err != nil {
				err = fmt.Errorf("growth: apply[%d] create %q: %w", i, p.Path, err)
				return err
			}
		case OpPatch:
			prev, e := os.ReadFile(full)
			if e != nil {
				if os.IsNotExist(e) {
					err = fmt.Errorf("growth: apply[%d] patch %q: file does not exist", i, p.Path)
				} else {
					err = fmt.Errorf("growth: apply[%d] patch %q: read: %w", i, p.Path, e)
				}
				return err
			}
			s := string(prev)
			if c := strings.Count(s, p.Old); c != 1 {
				err = fmt.Errorf("growth: apply[%d] patch %q: Old must appear exactly once, got %d", i, p.Path, c)
				return err
			}
			out := strings.Replace(s, p.Old, p.New, 1)
			rollbacks = append(rollbacks, rollbackPatch(full, prev))
			if err = atomicWriteFile(full, []byte(out), 0o644); err != nil {
				err = fmt.Errorf("growth: apply[%d] patch %q: %w", i, p.Path, err)
				return err
			}
		case OpDelete:
			prev, e := os.ReadFile(full)
			if e != nil {
				if os.IsNotExist(e) {
					err = fmt.Errorf("growth: apply[%d] delete %q: file does not exist", i, p.Path)
				} else {
					err = fmt.Errorf("growth: apply[%d] delete %q: read: %w", i, p.Path, e)
				}
				return err
			}
			rollbacks = append(rollbacks, rollbackDelete(full, prev))
			if e := os.Remove(full); e != nil {
				err = fmt.Errorf("growth: apply[%d] delete %q: %w", i, p.Path, e)
				return err
			}
		}
	}
	return nil
}

func resolvedPath(rootClean, p string) string {
	if filepath.IsAbs(p) {
		full, _ := filepath.Abs(p)
		return filepath.Clean(full)
	}
	return filepath.Clean(filepath.Join(rootClean, p))
}

type rollback struct {
	restore func()
}

func rollbackCreate(full string) rollback {
	return rollback{restore: func() {
		_ = os.Remove(full)
	}}
}

func rollbackPatch(full string, prev []byte) rollback {
	return rollback{restore: func() {
		_ = atomicWriteFile(full, prev, 0o644)
	}}
}

func rollbackDelete(full string, prev []byte) rollback {
	return rollback{restore: func() {
		_ = atomicWriteFile(full, prev, 0o644)
	}}
}

func atomicWriteFile(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, ".growth-")
	if err != nil {
		return err
	}
	tmpName := f.Name()

	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		_ = os.Remove(tmpName)
		return err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		_ = os.Remove(tmpName)
		return err
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmpName)
		return err
	}

	if _, err := os.Stat(path); err == nil {
		if runtime.GOOS == "windows" {
			if err := os.Remove(path); err != nil {
				_ = os.Remove(tmpName)
				return err
			}
		}
	} else if !os.IsNotExist(err) {
		_ = os.Remove(tmpName)
		return err
	}

	if err := os.Rename(tmpName, path); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	if perm != 0 {
		_ = os.Chmod(path, perm)
	}
	return nil
}
