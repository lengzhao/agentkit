package chatapi

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/runtime/platform/common"
)

func (p *Platform) emitAssistantMedia(ctx context.Context, run *runState, msg agentkit.ModelMessage) error {
	if run == nil || run.sse == nil || p.workspace == nil {
		return nil
	}
	for _, part := range msg.Content {
		switch part.Type {
		case "image", "document":
			if err := p.emitMediaPart(ctx, run, part); err != nil {
				slog.Warn("chat-api: emit media", "type", part.Type, "error", err)
			}
		}
	}
	return nil
}

func (p *Platform) emitMediaPart(ctx context.Context, run *runState, part agentkit.ContentPart) error {
	data, name, err := common.ReadMediaPart(part)
	if err != nil {
		return err
	}
	mimeType := part.MIME
	if mimeType == "" {
		mimeType = http.DetectContentType(data)
	}
	meta, err := p.saveDownloadFile(ctx, run.channelKey, name, mimeType, data)
	if err != nil {
		return err
	}
	payload := map[string]any{
		"message_id": run.messageID,
		"id":         meta.ID,
		"kind":       meta.Kind,
		"filename":   meta.Filename,
		"mime_type":  meta.MimeType,
		"size":       meta.Size,
	}
	for k, v := range p.fileLinkFields(run.apiBase, run.channelKey, meta.ID) {
		payload[k] = v
	}
	return run.sse.Event("file_ready", payload)
}
