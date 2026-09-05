package skill

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	capsskill "github.com/lengzhao/agentkit/cap/skill"
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
func ReadFile(ctx context.Context, reg capsskill.Registry, name, rel string) (capsskill.Content, error) {
	meta, err := reg.Load(ctx, name)
	if err != nil {
		return capsskill.Content{}, err
	}
	if strings.TrimSpace(meta.Path) == "" {
		return capsskill.Content{}, fmt.Errorf("skill %q has no on-disk directory", name)
	}
	clean, err := SanitizeRelativePath(rel)
	if err != nil {
		return capsskill.Content{}, err
	}
	full := filepath.Join(meta.Path, clean)
	data, err := os.ReadFile(full)
	if err != nil {
		return capsskill.Content{}, fmt.Errorf("skill file %q: %w", rel, err)
	}
	return capsskill.Content{
		Name:        meta.Name,
		Description: meta.Description,
		Body:        string(data),
		Path:        meta.Path,
	}, nil
}
