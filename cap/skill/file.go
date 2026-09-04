package skill

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// SanitizeRelativePath rejects paths that escape a skill bundle directory.
func SanitizeRelativePath(rel string) (string, error) {
	rel = strings.TrimSpace(filepath.ToSlash(rel))
	if rel == "" {
		return "", fmt.Errorf("file path is required")
	}
	clean := filepath.Clean(rel)
	if clean == "." || strings.HasPrefix(clean, "..") || filepath.IsAbs(clean) {
		return "", fmt.Errorf("path must stay within the skill directory")
	}
	return clean, nil
}

// ReadFile loads a supporting file from the skill directory identified by name.
func ReadFile(ctx context.Context, reg Registry, name, rel string) (Content, error) {
	meta, err := reg.Load(ctx, name)
	if err != nil {
		return Content{}, err
	}
	if strings.TrimSpace(meta.Path) == "" {
		return Content{}, fmt.Errorf("skill %q has no on-disk directory", name)
	}
	clean, err := SanitizeRelativePath(rel)
	if err != nil {
		return Content{}, err
	}
	full := filepath.Join(meta.Path, clean)
	data, err := os.ReadFile(full)
	if err != nil {
		return Content{}, fmt.Errorf("skill file %q: %w", rel, err)
	}
	return Content{
		Name:        meta.Name,
		Description: meta.Description,
		Body:        string(data),
		Path:        meta.Path,
	}, nil
}
