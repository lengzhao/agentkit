package configfile

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// WriteTarget picks the local-scoped config path used for /add writes.
func WriteTarget(files []string) (string, error) {
	for _, f := range files {
		f = strings.TrimSpace(f)
		if strings.HasPrefix(f, "local:") {
			return f, nil
		}
	}
	for _, f := range files {
		if f = strings.TrimSpace(f); f != "" {
			return f, nil
		}
	}
	return "", fmt.Errorf("no config file configured")
}

// WriteAtomic writes data to path via a temp file in the same directory.
func WriteAtomic(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}
	tmp, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tmpName := tmp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpName)
		}
	}()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write temp file: %w", err)
	}
	if err := tmp.Chmod(perm); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("chmod temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp file: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("rename temp file: %w", err)
	}
	cleanup = false
	return nil
}

// Restore replaces path with prev. When prev is nil the file is removed.
func Restore(path string, prev []byte, perm os.FileMode) error {
	if prev == nil {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	return WriteAtomic(path, prev, perm)
}
