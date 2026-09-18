package session

import (
	"context"
	"log/slog"
	"strings"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/cap/workspace"
	rtmedia "github.com/lengzhao/agentkit/runtime/media"
	"github.com/lengzhao/agentkit/runtime/rctx"
)

// PrepareMessagesForLLM applies modality policy and optional vision hydration before an LLM call.
func PrepareMessagesForLLM(ctx context.Context, msgs []agentkit.ModelMessage, ws workspace.Service, maxImageBytes int, modalities []string) ([]agentkit.ModelMessage, error) {
	if len(msgs) == 0 {
		return msgs, nil
	}
	msgs = sanitizeMessagesForLLM(ctx, ws, msgs)
	hydrateImages := agentkit.SupportsModality(modalities, agentkit.ModalityImage)
	if !hydrateImages {
		out := make([]agentkit.ModelMessage, len(msgs))
		for i, msg := range msgs {
			out[i] = demoteVisualParts(ctx, msg, ws)
		}
		return sanitizeMessagesForLLM(ctx, ws, out), nil
	}
	out, err := hydrateLocalAttachments(ctx, msgs, ws, maxImageBytes)
	if err != nil {
		return nil, err
	}
	return sanitizeMessagesForLLM(ctx, ws, out), nil
}

// HydrateLocalAttachments reloads workspace images for LLM vision (text + image modalities).
func HydrateLocalAttachments(ctx context.Context, msgs []agentkit.ModelMessage, ws workspace.Service, maxImageBytes int) ([]agentkit.ModelMessage, error) {
	return PrepareMessagesForLLM(ctx, msgs, ws, maxImageBytes, agentkit.DefaultLLMModalities)
}

func hydrateLocalAttachments(ctx context.Context, msgs []agentkit.ModelMessage, ws workspace.Service, maxImageBytes int) ([]agentkit.ModelMessage, error) {
	if ws == nil || len(msgs) == 0 {
		return msgs, nil
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

	hydrated, err := hydrateMessageAttachments(ctx, msgs[lastUser], ws, maxImageBytes, true)
	if err != nil {
		return nil, err
	}
	out[lastUser] = hydrated

	return injectReadToolVision(ctx, out, lastUser, ws, maxImageBytes)
}

func demoteVisualParts(ctx context.Context, msg agentkit.ModelMessage, ws workspace.Service) agentkit.ModelMessage {
	if len(msg.Content) == 0 {
		return msg
	}
	out := make([]agentkit.ContentPart, 0, len(msg.Content))
	for _, part := range msg.Content {
		switch part.Type {
		case rtmedia.ContentTypeAttachmentRef, "image", "image_url":
			out = append(out, agentkit.ContentPart{Type: "text", Text: attachmentHint(ctx, part, ws)})
		case "audio", "video":
			out = append(out, agentkit.ContentPart{Type: "text", Text: mediaHint(ctx, part, ws)})
		default:
			if part.Type != "text" && part.Type != "" && strings.TrimSpace(part.Text) == "" && strings.TrimSpace(part.URL) != "" {
				out = append(out, agentkit.ContentPart{Type: "text", Text: attachmentHint(ctx, part, ws)})
				continue
			}
			out = append(out, part)
		}
	}
	msg.Content = out
	return msg
}

func mediaHint(ctx context.Context, part agentkit.ContentPart, ws workspace.Service) string {
	if t := strings.TrimSpace(part.Type); t != "" {
		return "[" + t + " attachment omitted for text-only model]"
	}
	return attachmentHint(ctx, part, ws)
}

func hydrateMessageAttachments(ctx context.Context, msg agentkit.ModelMessage, ws workspace.Service, maxImageBytes int, hydrateImages bool) (agentkit.ModelMessage, error) {
	if msg.Role != "user" || len(msg.Content) == 0 {
		return msg, nil
	}
	out := make([]agentkit.ContentPart, 0, len(msg.Content))
	for _, part := range msg.Content {
		switch part.Type {
		case rtmedia.ContentTypeAttachmentRef:
			expanded, err := expandAttachmentRef(ctx, part, ws, maxImageBytes, hydrateImages)
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

func expandAttachmentRef(ctx context.Context, part agentkit.ContentPart, ws workspace.Service, maxImageBytes int, hydrateImages bool) ([]agentkit.ContentPart, error) {
	if !hydrateImages {
		return []agentkit.ContentPart{{Type: "text", Text: attachmentHint(ctx, part, ws)}}, nil
	}
	src := strings.TrimSpace(part.Source)
	mayImage, err := rtmedia.WorkspaceFileMayBeImage(ctx, ws, src)
	if err != nil {
		return nil, err
	}
	if src != "" && mayImage {
		data, mime, err := rtmedia.LoadWorkspaceImage(ctx, ws, src, maxImageBytes)
		if err != nil {
			return nil, err
		}
		if len(data) > 0 {
			if mime == "" {
				mime = rtmedia.DetectMIME(src, data)
			}
			return []agentkit.ContentPart{{
				Type:   "image_url",
				URL:    rtmedia.DataURL(mime, data),
				MIME:   mime,
				Source: src,
			}}, nil
		}
	}
	return []agentkit.ContentPart{{Type: "text", Text: attachmentHint(ctx, part, ws)}}, nil
}

func attachmentHint(ctx context.Context, part agentkit.ContentPart, ws workspace.Service) string {
	if src := strings.TrimSpace(part.Source); src != "" {
		if ws != nil {
			src = rtmedia.AgentLLMPath(ctx, ws, src)
		}
		hint := "[attachment: " + src
		if mime := strings.TrimSpace(part.MIME); mime != "" {
			hint += " mime=" + mime
		}
		return hint + "]"
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
			rawPath := rtmedia.ParseReadImagePath(result.Content)
			if rawPath == "" {
				continue
			}
			path := rtmedia.AgentLLMPath(ctx, ws, rawPath)
			if path == "" {
				continue
			}
			mayImage, err := rtmedia.WorkspaceFileMayBeImage(ctx, ws, path)
			if err != nil {
				return nil, err
			}
			if !mayImage {
				continue
			}
			if _, ok := seen[path]; ok {
				continue
			}
			seen[path] = struct{}{}
			data, mime, err := rtmedia.LoadWorkspaceImage(ctx, ws, path, maxImageBytes)
			if err != nil {
				return nil, err
			}
			if len(data) == 0 {
				slog.Warn("vision hydrate skipped empty image payload",
					"path", path,
					"session_id", rctx.SessionIDFromContext(ctx),
					"agent_id", rctx.AgentIDFromContext(ctx),
				)
				continue
			}
			if mime == "" {
				mime = rtmedia.DetectMIME(path, data)
			}
			parts = append(parts, agentkit.ContentPart{
				Type:   "image_url",
				URL:    rtmedia.DataURL(mime, data),
				MIME:   mime,
				Source: path,
			})
		}
	}
	if len(parts) == 0 {
		return msgs, nil
	}

	// Merge vision parts into the turn's user message. A user message after tool
	// results is invalid for OpenAI-style tool flows and is often ignored by providers.
	out := make([]agentkit.ModelMessage, len(msgs))
	copy(out, msgs)
	user := out[lastUser]
	user.Content = append(user.Content, parts...)
	out[lastUser] = user
	return out, nil
}
