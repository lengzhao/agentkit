package common

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/lengzhao/agentkit"
	cw "github.com/lengzhao/agentkit/cap/workspace"
	rtmedia "github.com/lengzhao/agentkit/runtime/media"
	"github.com/lengzhao/agentkit/runtime/rctx"
)

// PrepareInboundAttachments materializes fresh uploads using the caller's fixed
// turn context. Already-saved references are never moved or reinterpreted.
// Custom runners must call this after session routing and cwd preparation.
func PrepareInboundAttachments(ctx context.Context, ws cw.Service, event agentkit.MessageEvent) (agentkit.MessageEvent, error) {
	defer ReleaseInboundUploads(event)
	env := rctx.EnvelopeFromContext(ctx)
	for _, a := range event.Attachments {
		if a.Owner == nil {
			continue
		}
		owner := a.Owner
		if owner.Unknown {
			strict := false
			if resolver, ok := ws.(cw.DefaultDirectoryResolver); ok {
				dir, err := resolver.DefaultWorkingDirectory(ctx)
				strict = dir != "" || err != nil
			}
			if strict {
				return event, fmt.Errorf("buffered attachment owner could not be resolved; configure the platform sessionStore dependency and resend with the text")
			}
			slog.Warn("legacy buffered attachment has no logical session owner; configure platform sessionStore", "path", a.Path)
		}
		oldTarget, oldOK := rctx.RouteTargetFromRoute(owner.Route)
		target, ok := rctx.RouteTargetFromRoute(env.Route)
		if (!owner.Unknown && owner.Conversation != env.Conversation) || owner.Route.DestinationID() != env.Route.DestinationID() || oldOK != ok || oldTarget.ChannelID != target.ChannelID || oldTarget.ThreadID != target.ThreadID {
			return event, fmt.Errorf("buffered attachment %q belongs to session %q and its original route; resend it with the text in the intended session", a.OrigName, owner.Conversation)
		}
	}
	if len(event.Uploads) == 0 {
		return event, nil
	}
	if err := ctx.Err(); err != nil {
		return event, err
	}
	// Validate all content references before performing any filesystem writes.
	for _, u := range event.Uploads {
		if u.ContentIndex < -1 || u.ContentIndex >= len(event.Message.Content) {
			return event, fmt.Errorf("invalid inbound upload content index %d", u.ContentIndex)
		}
		if u.ContentIndex >= 0 && event.Message.Content[u.ContentIndex].Type != "image_url" {
			return event, fmt.Errorf("inbound image upload requires image content")
		}
	}
	event.Message.Content = append([]agentkit.ContentPart(nil), event.Message.Content...)
	event.Attachments = append([]agentkit.InboundAttachment(nil), event.Attachments...)
	var notes []inboundSavedAttachment
	for _, u := range event.Uploads {
		var reader io.ReadCloser
		if u.Source != nil {
			var err error
			reader, err = u.Source.Open()
			if err != nil {
				return event, fmt.Errorf("open staged inbound upload: %w", err)
			}
		} else {
			if len(u.Data) == 0 {
				continue
			}
			reader = io.NopCloser(bytes.NewReader(u.Data))
		}
		a, saveErr := saveInboundFileAt(ctx, ws, reader, u.OrigName, u.MIME)
		closeErr := reader.Close()
		if saveErr != nil {
			return event, saveErr
		}
		if closeErr != nil {
			return event, closeErr
		}
		a.path = rtmedia.AgentLLMPath(ctx, ws, a.path)
		if !filepath.IsAbs(a.path) {
			return event, fmt.Errorf("inbound attachment path is not absolute: %s", a.path)
		}
		if u.ContentIndex >= 0 {
			a.image = true
			event.Message.Content[u.ContentIndex].Source = a.path
			if event.Message.Content[u.ContentIndex].URL == "" {
				data, err := os.ReadFile(a.path)
				if err != nil {
					return event, err
				}
				event.Message.Content[u.ContentIndex].URL = rtmedia.DataURL(u.MIME, data)
			}
		}
		notes = append(notes, a)
		event.Attachments = append(event.Attachments, agentkit.InboundAttachment{Path: a.path, MIME: a.mime, Size: a.size, OrigName: a.origName, Image: a.image})
	}
	if len(notes) > 0 {
		found := false
		for i, p := range event.Message.Content {
			if p.Type == "text" {
				event.Message.Content[i].Text = appendInboundAttachmentBlock(ctx, p.Text, notes, &InboundOpts{Workspace: ws})
				found = true
				break
			}
		}
		if !found {
			event.Message.Content = append([]agentkit.ContentPart{{Type: "text", Text: appendInboundAttachmentBlock(ctx, "", notes, &InboundOpts{Workspace: ws})}}, event.Message.Content...)
		}
	}
	event.Uploads = nil
	return event, nil
}

// SaveInboundAttachmentsAt saves a file-first upload using an already-resolved
// ingress context. Its stable path and owner survive later cwd/session changes.
func SaveInboundAttachmentsAt(ctx context.Context, ws cw.Service, files []FileAttachment) ([]PresavedAttachment, error) {
	saved, err := saveInboundFilesAt(ctx, files, ws)
	if err != nil {
		return nil, err
	}
	env := rctx.EnvelopeFromContext(ctx)
	owner := &agentkit.AttachmentOwner{Conversation: env.Conversation, Route: env.Route, Unknown: env.Conversation == ""}
	out := make([]PresavedAttachment, 0, len(saved))
	for _, a := range saved {
		out = append(out, PresavedAttachment{Path: rtmedia.AgentLLMPath(ctx, ws, a.path), Mime: a.mime, Size: a.size, OrigName: a.origName, Image: a.image, Owner: owner})
	}
	return out, nil
}
