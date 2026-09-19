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

// 平台入站记录的真实文件信息（size/mime/path/origName/image）必须转成结构化
// telemetry 记录，并汇总 count/bytes，供独立 span 使用。
func TestInboundAttachmentInfosAndSpanAttrs(t *testing.T) {
	t.Parallel()

	attachments := []agentkit.InboundAttachment{
		{Path: "upload/x.png", MIME: "image/png", Size: 12345, OrigName: "x.png", Image: true},
		{Path: "upload/big.log", MIME: "text/plain", Size: 200000, OrigName: "big.log"},
	}
	infos := telemetry.InboundAttachmentInfos(attachments)
	if len(infos) != 2 {
		t.Fatalf("infos len = %d, want 2", len(infos))
	}
	if infos[0].Type != "image" || infos[0].Mime != "image/png" || infos[0].Source != "upload/x.png" || infos[0].Bytes != 12345 {
		t.Fatalf("infos[0] = %#v", infos[0])
	}
	if infos[1].Type != "file" || infos[1].Bytes != 200000 {
		t.Fatalf("infos[1] = %#v", infos[1])
	}

	attrs := telemetry.AttachmentSpanAttrs(attachments)
	if attrs == nil {
		t.Fatal("AttachmentSpanAttrs returned nil for non-empty attachments")
	}
	if attrs["attachment_count"] != "2" {
		t.Fatalf("attachment_count = %q, want 2", attrs["attachment_count"])
	}
	if attrs["attachment_bytes"] != "212345" {
		t.Fatalf("attachment_bytes = %q, want 212345", attrs["attachment_bytes"])
	}
	var decoded []telemetry.AttachmentInfo
	if err := json.Unmarshal([]byte(attrs["attachments"]), &decoded); err != nil {
		t.Fatalf("attachments JSON invalid: %v", err)
	}
	if len(decoded) != 2 || decoded[0].Bytes != 12345 {
		t.Fatalf("decoded = %#v", decoded)
	}
}

func TestAttachmentSpanAttrsNilOnEmpty(t *testing.T) {
	t.Parallel()
	if attrs := telemetry.AttachmentSpanAttrs(nil); attrs != nil {
		t.Fatalf("AttachmentSpanAttrs(nil) = %#v, want nil", attrs)
	}
	if attrs := telemetry.AttachmentSpanAttrs([]agentkit.InboundAttachment{}); attrs != nil {
		t.Fatalf("AttachmentSpanAttrs(empty) = %#v, want nil", attrs)
	}
	if infos := telemetry.InboundAttachmentInfos(nil); infos != nil {
		t.Fatalf("InboundAttachmentInfos(nil) = %#v, want nil", infos)
	}
}
