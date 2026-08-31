package fs_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/lengzhao/agentkit/cap/media"
	fsplugin "github.com/lengzhao/agentkit/plugins/tool/fs"
)

func TestReadImageReturnsMetadataOnly(t *testing.T) {
	t.Parallel()

	png := string([]byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a})
	pack, err := fsplugin.NewFSMemory(fsplugin.FSMemoryConfig{
		Files: map[string]string{"shot.png": png},
		Tools: []string{"read"},
	})
	if err != nil {
		t.Fatal(err)
	}

	for _, tool := range pack {
		if tool.Name() != "read" {
			continue
		}
		out, err := tool.Call(context.Background(), json.RawMessage(`{"path":"shot.png"}`))
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(out, "data:") {
			t.Fatalf("read should not inline base64: %q", out)
		}
		if got := media.ParseReadImagePath(out); got != "shot.png" {
			t.Fatalf("path = %q", got)
		}
		return
	}
	t.Fatal("read tool not found")
}
