package session

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/cap/media"
	"github.com/lengzhao/agentkit/cap/workspace"
)

// DefaultMaxHydratedImageBytes caps workspace images reloaded for vision.
const DefaultMaxHydratedImageBytes = 10 << 20

// HydrateLocalAttachments reloads workspace image refs on the latest user turn.
func HydrateLocalAttachments(ctx context.Context, msgs []agentkit.ModelMessage, ws workspace.Service, maxImageBytes int) ([]agentkit.ModelMessage, error) {
	if ws == nil || len(msgs) == 0 {
		return msgs, nil
	}
	if maxImageBytes <= 0 {
		maxImageBytes = DefaultMaxHydratedImageBytes
	}
	out := make([]agentkit.ModelMessage, len(msgs))
	lastUser := -1
	for i, msg := range msgs {
		if msg.Role == "user" {
			lastUser = i
		}
		out[i] = msg
	}
	if lastUser < 0 {
		return msgs, nil
	}
	hydrated, err := hydrateMessageAttachments(ctx, msgs[lastUser], ws, maxImageBytes)
	if err != nil {
		return nil, err
	}
	out[lastUser] = hydrated
	return out, nil
}

func hydrateMessageAttachments(ctx context.Context, msg agentkit.ModelMessage, ws workspace.Service, maxImageBytes int) (agentkit.ModelMessage, error) {
	if msg.Role != "user" || len(msg.Content) == 0 {
		return msg, nil
	}
	out := make([]agentkit.ContentPart, 0, len(msg.Content))
	for _, part := range msg.Content {
		switch part.Type {
		case media.ContentTypeAttachmentRef:
			expanded, err := expandAttachmentRef(ctx, part, ws, maxImageBytes)
			if err != nil {
				return msg, err
			}
			out = append(out, expanded...)
		default:
			out = append(out, part)
		}
	}
	msg.Content = out
	return msg, nil
}

func expandAttachmentRef(ctx context.Context, part agentkit.ContentPart, ws workspace.Service, maxImageBytes int) ([]agentkit.ContentPart, error) {
	src := strings.TrimSpace(part.Source)
	if src != "" && media.IsImagePath(src) {
		data, mime, err := loadWorkspaceImage(ctx, ws, src, maxImageBytes)
		if err != nil {
			return nil, err
		}
		if len(data) > 0 {
			if mime == "" {
				mime = media.DetectMIME(src, data)
			}
			return []agentkit.ContentPart{{
				Type:   "image_url",
				URL:    media.DataURL(mime, data),
				MIME:   mime,
				Source: src,
			}}, nil
		}
	}
	return []agentkit.ContentPart{{Type: "text", Text: attachmentHint(part)}}, nil
}

func attachmentHint(part agentkit.ContentPart) string {
	if src := strings.TrimSpace(part.Source); src != "" {
		return "[attachment: " + src + "]"
	}
	if url := strings.TrimSpace(part.URL); url != "" {
		return "[attachment: " + url + "]"
	}
	return "[attachment omitted]"
}

func loadWorkspaceImage(ctx context.Context, ws workspace.Service, workRel string, maxBytes int) ([]byte, string, error) {
	workRel = media.NormalizeWorkRel(workRel)
	abs, err := ws.Resolve(ctx, filepath.Join("work", workRel))
	if err != nil {
		return nil, "", err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return nil, "", err
	}
	if info.IsDir() {
		return nil, "", fmt.Errorf("not a file: %s", workRel)
	}
	if info.Size() > int64(maxBytes) {
		return nil, "", nil
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		return nil, "", err
	}
	if len(data) > maxBytes {
		return nil, "", nil
	}
	return data, media.DetectMIME(workRel, data), nil
}
