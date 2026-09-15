package telemetry_test

import (
	"encoding/json"
	"testing"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/runtime/telemetry"
)

func TestToolObservationAttrsReadPath(t *testing.T) {
	t.Parallel()
	attrs := telemetry.ToolObservationAttrs(agentkit.ToolCall{
		ID:    "call-1",
		Name:  "read",
		Input: json.RawMessage(`{"path":"local:work/upload/a.jpg"}`),
	})
	if attrs["read_path"] != "local:work/upload/a.jpg" {
		t.Fatalf("read_path = %q", attrs["read_path"])
	}
}

func TestAttachmentSourcesFromMessage(t *testing.T) {
	t.Parallel()
	paths := telemetry.AttachmentSources(agentkit.ModelMessage{
		Content: []agentkit.ContentPart{{
			Type:   "image_url",
			Source: "local:work/upload/x.jpg",
		}},
	})
	if len(paths) != 1 || paths[0] != "local:work/upload/x.jpg" {
		t.Fatalf("paths = %#v", paths)
	}
}
