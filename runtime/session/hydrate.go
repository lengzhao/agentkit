package session

import (
	"context"
	"strings"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/cap/media"
	"github.com/lengzhao/agentkit/cap/workspace"
)

// HydrateLocalAttachments reloads workspace images for LLM vision:
// attachment_ref on the latest user message, and read-tool image paths from the
// current tool batch since that user turn.
func HydrateLocalAttachments(ctx context.Context, msgs []agentkit.ModelMessage, ws workspace.Service, maxImageBytes int) ([]agentkit.ModelMessage, error) {
	if ws == nil || len(msgs) == 0 {
		return msgs, nil
	}
	if maxImageBytes <= 0 {
		maxImageBytes = media.DefaultMaxWorkspaceImageBytes
	}

	lastUser := -1
	for i, msg := range msgs {
		if msg.Role == "user" {
			lastUser = i
		}
	}
	if lastUser < 0 {
		return msgs, nil
	}

	out := make([]agentkit.ModelMessage, len(msgs))
	copy(out, msgs)

	hydrated, err := hydrateMessageAttachments(ctx, msgs[lastUser], ws, maxImageBytes)
	if err != nil {
		return nil, err
	}
	out[lastUser] = hydrated

	return injectReadToolVision(ctx, out, lastUser, ws, maxImageBytes)
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
		data, mime, err := media.LoadWorkspaceImage(ctx, ws, src, maxImageBytes)
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

func injectReadToolVision(ctx context.Context, msgs []agentkit.ModelMessage, lastUser int, ws workspace.Service, maxImageBytes int) ([]agentkit.ModelMessage, error) {
	injectIdx := -1
	for i := len(msgs) - 1; i > lastUser; i-- {
		if msgs[i].Role == "tool" {
			injectIdx = i
			break
		}
	}
	if injectIdx < 0 {
		return msgs, nil
	}

	startIdx := injectIdx
	for startIdx > lastUser+1 && msgs[startIdx-1].Role == "tool" {
		startIdx--
	}

	seen := make(map[string]struct{})
	var parts []agentkit.ContentPart
	for i := startIdx; i <= injectIdx; i++ {
		if msgs[i].Role != "tool" {
			continue
		}
		for _, result := range msgs[i].ToolResults {
			if result.Name != "read" {
				continue
			}
			path := media.ParseReadImagePath(result.Content)
			if path == "" || !media.IsImagePath(path) {
				continue
			}
			if _, ok := seen[path]; ok {
				continue
			}
			seen[path] = struct{}{}
			data, mime, err := media.LoadWorkspaceImage(ctx, ws, path, maxImageBytes)
			if err != nil {
				return nil, err
			}
			if len(data) == 0 {
				continue
			}
			if mime == "" {
				mime = media.DetectMIME(path, data)
			}
			parts = append(parts, agentkit.ContentPart{
				Type:   "image_url",
				URL:    media.DataURL(mime, data),
				MIME:   mime,
				Source: path,
			})
		}
	}
	if len(parts) == 0 {
		return msgs, nil
	}

	out := make([]agentkit.ModelMessage, 0, len(msgs)+1)
	for i, msg := range msgs {
		out = append(out, msg)
		if i == injectIdx {
			out = append(out, agentkit.ModelMessage{Role: "user", Content: parts})
		}
	}
	return out, nil
}
