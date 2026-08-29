package common

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/lengzhao/agentkit"
)

// ReadMediaPart loads bytes for an outbound image or document content part.
// tool/send stores the resolved workspace path in URL.
func ReadMediaPart(part agentkit.ContentPart) (data []byte, fileName string, err error) {
	switch part.Type {
	case "image", "document":
		path := part.URL
		if path == "" {
			return nil, "", fmt.Errorf("media part %q missing url", part.Type)
		}
		data, err = os.ReadFile(path)
		if err != nil {
			return nil, "", fmt.Errorf("read media %q: %w", path, err)
		}
		name := filepath.Base(path)
		if name == "" || name == "." {
			name = "attachment"
		}
		return data, name, nil
	default:
		return nil, "", fmt.Errorf("unsupported media type %q", part.Type)
	}
}
