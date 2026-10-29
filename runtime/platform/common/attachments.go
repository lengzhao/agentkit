package common

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/lengzhao/agentkit"
	cw "github.com/lengzhao/agentkit/cap/workspace"
	rtmedia "github.com/lengzhao/agentkit/runtime/media"
	"github.com/lengzhao/agentkit/runtime/rctx"
	rtws "github.com/lengzhao/agentkit/runtime/workspace"
)

// ImageAttachment is an inbound image from an IM platform.
type ImageAttachment struct {
	MimeType string
	Data     []byte
	FileName string
	// WorkPath is relative to the agent work dir (e.g. upload/foo.png).
	WorkPath string
}

// FileAttachment is an inbound file from an IM platform.
type FileAttachment struct {
	MimeType string
	Data     []byte
	FileName string
}

// AudioAttachment is an inbound voice message.
type AudioAttachment struct {
	MimeType string
	Data     []byte
	Format   string
	Duration int
}

// PresavedAttachment is an attachment already saved to disk by the platform
// (e.g. buffered pending attachments). Only metadata is carried; the bytes
// are already under upload/. Paths returned by SaveInboundAttachments are absolute.
type PresavedAttachment struct {
	Owner    *agentkit.AttachmentOwner
	Path     string
	Mime     string
	Size     int
	OrigName string
	Image    bool
}

// InboundOpts configures optional inbound media handling.
type InboundOpts struct {
	// Workspace resolves upload paths; when set, inbound files land under the
	// same tenant root as session/store and tool/fs-workspace.
	Workspace cw.Service
	// DeferUploads leaves fresh downloaded files for the runner turn snapshot.
	DeferUploads bool
	// PresavedAttachments are attachments already on disk; their paths are
	// appended to the user prompt as an attachment block.
	PresavedAttachments []PresavedAttachment
}

// InboundOptsFor builds inbound media options from an optional workspace.
func InboundOptsFor(ws cw.Service) *InboundOpts {
	if ws == nil {
		return nil
	}
	return &InboundOpts{Workspace: ws}
}

// SaveInboundAttachments writes the given files to the upload directory and
// returns stable absolute paths plus metadata. Useful for platforms
// that buffer attachments before a text message arrives.
func SaveInboundAttachments(route agentkit.RouteRef, files []FileAttachment, opts *InboundOpts) []PresavedAttachment {
	saved := saveInboundFiles(route, files, opts)
	out := make([]PresavedAttachment, 0, len(saved))
	for _, s := range saved {
		out = append(out, PresavedAttachment{
			Path:     rtmedia.AgentLLMPath(inboundRouteContext(route), opts.Workspace, s.path),
			Mime:     s.mime,
			Size:     s.size,
			OrigName: s.origName,
			Image:    s.image,
		})
	}
	return out
}

// DeferredInboundOptsFor opts runner-based adapters into turn-time saving.
// InboundOptsFor retains eager behavior for standalone legacy callers.
func DeferredInboundOptsFor(ws cw.Service) *InboundOpts {
	if ws == nil {
		return nil
	}
	return &InboundOpts{Workspace: ws, DeferUploads: true}
}

// InboundFromContent builds a MessageEvent from text and media. By default it
// saves attachments eagerly. DeferUploads retains fresh bytes for turn-time
// materialization; existing file references are pinned to their ingress paths.
func InboundFromContent(agentID agentkit.AgentID, route agentkit.SessionRouteInput, userID, content, extraContent string, images []ImageAttachment, files []FileAttachment, audio *AudioAttachment, filePaths []string, opts *InboundOpts) agentkit.MessageEvent {
	if route.ScopeUserID == "" {
		route.ScopeUserID = strings.TrimSpace(userID)
	}
	deliveryID := route.DeliveryID
	if deliveryID == "" && route.Platform != "" && strings.TrimSpace(route.ChannelID) != "" {
		deliveryID = rctx.BuildDeliverySessionID(route.Platform, route.ChannelID, route.ThreadID, route.ScopeUserID)
		route.DeliveryID = deliveryID
	}

	routeRef := rctx.BuildSessionRoute(route)
	inboundCtx := inboundRouteContext(routeRef)
	deferred := opts != nil && opts.DeferUploads
	images = append([]ImageAttachment(nil), images...)
	var uploads []agentkit.InboundUpload
	if deferred {
		for _, f := range files {
			if len(f.Data) > 0 {
				uploads = append(uploads, agentkit.InboundUpload{Data: append([]byte(nil), f.Data...), MIME: f.MimeType, OrigName: f.FileName, ContentIndex: -1})
			}
		}
	}

	var attachmentNotes []inboundSavedAttachment
	for _, img := range images {
		if len(img.Data) == 0 {
			continue
		}
		if p := strings.TrimSpace(img.WorkPath); p != "" {
			attachmentNotes = append(attachmentNotes, inboundSavedAttachment{
				path:     p,
				mime:     strings.TrimSpace(img.MimeType),
				size:     len(img.Data),
				origName: strings.TrimSpace(img.FileName),
				image:    true,
			})
		}
	}
	if len(files) > 0 && !deferred {
		attachmentNotes = append(attachmentNotes, saveInboundFiles(routeRef, files, opts)...)
	}
	for i := range images {
		if deferred || strings.TrimSpace(images[i].WorkPath) != "" || len(images[i].Data) == 0 {
			continue
		}
		if opts == nil || opts.Workspace == nil {
			continue
		}
		saved := saveInboundFiles(routeRef, []FileAttachment{{
			MimeType: images[i].MimeType,
			Data:     images[i].Data,
			FileName: images[i].FileName,
		}}, opts)
		if len(saved) == 0 {
			continue
		}
		saved[0].image = true
		images[i].WorkPath = saved[0].path
		attachmentNotes = append(attachmentNotes, saved[0])
	}
	if len(filePaths) > 0 {
		for _, p := range filePaths {
			attachmentNotes = append(attachmentNotes, inboundSavedAttachment{path: p})
		}
	}
	if opts != nil && len(opts.PresavedAttachments) > 0 {
		for _, a := range opts.PresavedAttachments {
			attachmentNotes = append(attachmentNotes, inboundSavedAttachment{
				owner:    a.Owner,
				path:     a.Path,
				mime:     a.Mime,
				size:     a.Size,
				origName: a.OrigName,
				image:    a.Image,
			})
		}
	}

	text := strings.TrimSpace(content)
	if extraContent != "" {
		if text != "" {
			text = extraContent + "\n\n" + text
		} else {
			text = extraContent
		}
	}
	if audio != nil {
		hint := "[voice message"
		if audio.Duration > 0 {
			hint += fmt.Sprintf(", %ds", audio.Duration)
		}
		hint += "]"
		if text != "" {
			text += "\n\n" + hint
		} else {
			text = hint
		}
	}
	if len(attachmentNotes) > 0 {
		text = appendInboundAttachmentBlock(inboundCtx, text, attachmentNotes, opts)
	}

	var parts []agentkit.ContentPart
	if text != "" {
		parts = append(parts, agentkit.ContentPart{Type: "text", Text: text})
	}
	for _, img := range images {
		if len(img.Data) == 0 {
			continue
		}
		mime := img.MimeType
		if mime == "" {
			mime = "image/png"
		}
		url := rtmedia.DataURL(mime, img.Data)
		part := agentkit.ContentPart{Type: "image_url", URL: url, MIME: mime}
		if path := strings.TrimSpace(img.WorkPath); path != "" {
			if opts != nil && opts.Workspace != nil {
				path = rtmedia.AgentLLMPath(inboundCtx, opts.Workspace, path)
			}
			part.Source = path
		}
		if deferred && strings.TrimSpace(img.WorkPath) == "" {
			uploads = append(uploads, agentkit.InboundUpload{Data: append([]byte(nil), img.Data...), MIME: mime, OrigName: img.FileName, ContentIndex: len(parts)})
		}
		parts = append(parts, part)
	}
	if len(parts) == 0 {
		parts = append(parts, agentkit.ContentPart{Type: "text", Text: ""})
	}
	event := rctx.WithInboundRoute(agentkit.MessageEvent{

		Message: agentkit.ModelMessage{
			Role:    "user",
			Content: parts,
		},
		Envelope: agentkit.TurnEnvelope{AgentID: agentID, Route: agentkit.RouteRef{Instance: strings.TrimSpace(route.Platform)},
			Actor: agentkit.ActorRef{UserID: userID}},
	}, route)
	if len(attachmentNotes) > 0 {
		event.Attachments = make([]agentkit.InboundAttachment, 0, len(attachmentNotes))
		for _, a := range attachmentNotes {
			event.Attachments = append(event.Attachments, agentkit.InboundAttachment{
				Owner:    a.owner,
				Path:     a.path,
				MIME:     a.mime,
				Size:     a.size,
				OrigName: a.origName,
				Image:    a.image,
			})
		}
	}
	if deferred {
		event.IngressWorkspace = opts.Workspace
	}
	event.Uploads = uploads
	return event
}

type inboundSavedAttachment struct {
	owner    *agentkit.AttachmentOwner
	path     string
	mime     string
	size     int
	origName string
	image    bool
}

func appendInboundAttachmentBlock(ctx context.Context, prompt string, saved []inboundSavedAttachment, opts *InboundOpts) string {
	if len(saved) == 0 {
		return prompt
	}
	if prompt == "" {
		prompt = "Please analyze the attached file(s)."
	}
	var ws cw.Service
	if opts != nil {
		ws = opts.Workspace
	}
	var lines []string
	for _, a := range saved {
		lines = append(lines, formatInboundAttachmentLine(ctx, ws, a))
	}
	return prompt + "\n\n(Files the user uploaded with this message. workspace paths:\n" +
		strings.Join(lines, "\n") + ")"
}

func formatInboundAttachmentLine(ctx context.Context, ws cw.Service, a inboundSavedAttachment) string {
	display := strings.TrimSpace(a.path)
	if ws != nil {
		display = rtmedia.AgentLLMPath(ctx, ws, display)
	}
	var b strings.Builder
	b.WriteString("- ")
	b.WriteString(display)
	if m := strings.TrimSpace(a.mime); m != "" {
		b.WriteString(" mime=")
		b.WriteString(m)
	}
	if a.size > 0 {
		b.WriteString(fmt.Sprintf(" size=%d", a.size))
	}
	if n := strings.TrimSpace(a.origName); n != "" {
		b.WriteString(" name=")
		b.WriteString(n)
	}
	if a.image {
		b.WriteString(" type=image")
	}
	return b.String()
}

// IsImageAttachment reports whether an inbound attachment should be sent to the
// model as vision input instead of a read-tool file path.
func IsImageAttachment(mimeType, filename string) bool {
	return rtmedia.IsImage(mimeType, filename)
}

func saveInboundFiles(route agentkit.RouteRef, files []FileAttachment, opts *InboundOpts) []inboundSavedAttachment {
	if opts == nil || opts.Workspace == nil {
		slog.Warn("common: inbound attachments require workspace")
		return nil
	}
	saved, err := saveInboundFilesAt(inboundRouteContext(route), files, opts.Workspace)
	if err != nil {
		slog.Warn("common: save inbound attachments failed", "error", err)
	}
	return saved
}

func saveInboundFilesAt(ctx context.Context, files []FileAttachment, ws cw.Service) ([]inboundSavedAttachment, error) {
	if ws == nil {
		return nil, fmt.Errorf("inbound attachments require workspace")
	}
	attachDir, err := rtws.ResolveFile(ctx, ws, cw.UploadWorkRel(ws))
	if err != nil {
		return nil, fmt.Errorf("resolve inbound upload directory: %w", err)
	}
	if err = os.MkdirAll(attachDir, 0755); err != nil {
		return nil, fmt.Errorf("create inbound upload directory: %w", err)
	}
	var out []inboundSavedAttachment
	for i, f := range files {
		if len(f.Data) == 0 {
			continue
		}
		fname := sanitizeAttachmentFileName(f.FileName)
		if fname == "" {
			fname = fmt.Sprintf("file_%d_%d", time.Now().UnixNano(), i)
		}
		fname = rtmedia.EnsureInboundFileName(fname, f.MimeType, f.Data)
		name, err := writeInboundFile(attachDir, fname, f.Data)
		if err != nil {
			return out, err
		}
		path := cw.AttachFSRel(ws, name)
		if cw.WorkingDirectory(ctx) != "" {
			path = filepath.Join(attachDir, name)
		}
		out = append(out, inboundSavedAttachment{path: path, mime: strings.TrimSpace(f.MimeType), size: len(f.Data), origName: strings.TrimSpace(f.FileName), image: rtmedia.IsImage(f.MimeType, f.FileName)})
	}
	return out, nil
}

// Exclusive creation protects historical references, including concurrent uploads.
func writeInboundFile(dir, name string, data []byte) (string, error) {
	ext := filepath.Ext(name)
	stem := strings.TrimSuffix(name, ext)
	for n := 0; ; n++ {
		candidate := name
		if n > 0 {
			candidate = fmt.Sprintf("%s_%d%s", stem, n, ext)
		}
		path := filepath.Join(dir, candidate)
		f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
		if errors.Is(err, os.ErrExist) {
			continue
		}
		if err != nil {
			return "", fmt.Errorf("create inbound attachment: %w", err)
		}
		_, writeErr := f.Write(data)
		closeErr := f.Close()
		if err = errors.Join(writeErr, closeErr); err != nil {
			return "", fmt.Errorf("write inbound attachment: %w", err)
		}
		return candidate, nil
	}
}

func sanitizeAttachmentFileName(name string) string {
	name = filepath.ToSlash(name)
	name = strings.ReplaceAll(name, "\\", "/")
	name = filepath.Base(name)
	if name == "" || name == "." || name == ".." {
		return ""
	}
	return name
}

func inboundRouteContext(route agentkit.RouteRef) context.Context {
	env := rctx.ResolveEnvelope(agentkit.MessageEvent{Envelope: agentkit.TurnEnvelope{Route: route}}, rctx.DefaultRoutePolicy(agentkit.SessionScopeChannel))
	return rctx.ApplyEnvelopeToContext(context.Background(), env)
}
