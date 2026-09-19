package telemetry

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/lengzhao/agentkit"
)

// AttachmentSources collects workspace paths from message content (Source fields).
func AttachmentSources(msg agentkit.ModelMessage) []string {
	var out []string
	seen := make(map[string]struct{})
	for _, part := range msg.Content {
		src := strings.TrimSpace(part.Source)
		if src == "" {
			continue
		}
		if _, ok := seen[src]; ok {
			continue
		}
		seen[src] = struct{}{}
		out = append(out, src)
	}
	return out
}

// JoinAttachmentSources formats paths for trace metadata.
func JoinAttachmentSources(paths []string) string {
	if len(paths) == 0 {
		return ""
	}
	return strings.Join(paths, ",")
}

// AttachmentInfo is one inbound attachment's metadata, exported to telemetry
// backends (e.g. a dedicated Langfuse span) so size/mime/type are queryable
// without re-parsing the trace input text.
type AttachmentInfo struct {
	Type     string `json:"type"`
	Mime     string `json:"mime,omitempty"`
	Source   string `json:"source,omitempty"`
	OrigName string `json:"origName,omitempty"`
	// Bytes is the real on-disk file size recorded at ingest time.
	Bytes int `json:"bytes"`
}

// InboundAttachmentInfos converts platform-recorded inbound attachment metadata
// (real on-disk sizes) into telemetry records.
func InboundAttachmentInfos(attachments []agentkit.InboundAttachment) []AttachmentInfo {
	if len(attachments) == 0 {
		return nil
	}
	out := make([]AttachmentInfo, 0, len(attachments))
	for _, a := range attachments {
		typ := "file"
		if a.Image {
			typ = "image"
		}
		out = append(out, AttachmentInfo{
			Type:     typ,
			Mime:     strings.TrimSpace(a.MIME),
			Source:   strings.TrimSpace(a.Path),
			OrigName: strings.TrimSpace(a.OrigName),
			Bytes:    a.Size,
		})
	}
	return out
}

// AttachmentSpanAttrs builds the metadata map for a dedicated inbound.attachments
// span: a structured per-attachment JSON list plus aggregate count/bytes
// metrics for easy filtering in telemetry backends. Returns nil when there are
// no attachments so no span is emitted.
func AttachmentSpanAttrs(attachments []agentkit.InboundAttachment) map[string]string {
	infos := InboundAttachmentInfos(attachments)
	if len(infos) == 0 {
		return nil
	}
	raw, err := json.Marshal(infos)
	if err != nil {
		return nil
	}
	totalBytes := 0
	for _, a := range attachments {
		totalBytes += a.Size
	}
	return map[string]string{
		"attachments":      string(raw),
		"attachment_count": fmt.Sprint(len(infos)),
		"attachment_bytes": fmt.Sprint(totalBytes),
	}
}
