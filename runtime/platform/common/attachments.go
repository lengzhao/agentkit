package common

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/lengzhao/agentkit"
	cw "github.com/lengzhao/agentkit/cap/workspace"
	rtmedia "github.com/lengzhao/agentkit/runtime/media"
	"github.com/lengzhao/agentkit/runtime/session"
	"github.com/lengzhao/agentkit/runtime/workspace/workpath"
)

// ImageAttachment is an inbound image from an IM platform.
type ImageAttachment struct {
	MimeType string
	Data     []byte
	FileName string
	// WorkPath is a scoped workspace path (e.g. local:work/upload/foo.png).
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

// UploadWorkRel is the scoped path to the tenant upload directory (e.g. local:work/upload).
func UploadWorkRel(ws cw.Service) string {
	_, upload := workpath.WorkLayout(ws)
	return workpath.LocalPath(upload)
}

// AttachFSRel is the scoped path for an inbound file under the upload directory.
func AttachFSRel(ws cw.Service, name string) string {
	return workpath.LocalPath(workpath.AttachRel(ws, name))
}

// InboundOpts configures optional inbound media handling.
type InboundOpts struct {
	// Workspace resolves upload paths; when set, inbound files land under the
	// same tenant root as session/store and tool/fs-workspace.
	Workspace cw.Service
}

// InboundOptsFor builds inbound media options from an optional workspace.
func InboundOptsFor(ws cw.Service) *InboundOpts {
	if ws == nil {
		return nil
	}
	return &InboundOpts{Workspace: ws}
}

// InboundFromContent builds a MessageEvent from text and optional rtmedia.
// extraContent is prepended (e.g. quoted reply context). Non-image files are
// saved under the tenant work dir and referenced in the prompt for the read tool.
func InboundFromContent(agentID agentkit.AgentID, route session.SessionRouteInput, userID, content, extraContent string, images []ImageAttachment, files []FileAttachment, audio *AudioAttachment, filePaths []string, opts *InboundOpts) agentkit.MessageEvent {
	if route.ScopeUserID == "" {
		route.ScopeUserID = strings.TrimSpace(userID)
	}
	deliveryID := route.DeliveryID
	if deliveryID == "" && route.Platform != "" && strings.TrimSpace(route.ChannelID) != "" {
		deliveryID = session.BuildDeliverySessionID(route.Platform, route.ChannelID, route.ThreadID, route.ScopeUserID)
		route.DeliveryID = deliveryID
	}
	if len(files) > 0 {
		saved := saveInboundFiles(deliveryID, files, opts)
		filePaths = append(filePaths, saved...)
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
	if len(filePaths) > 0 {
		text = appendFileRefs(text, filePaths)
	}
	for i := range images {
		if strings.TrimSpace(images[i].WorkPath) != "" || len(images[i].Data) == 0 {
			continue
		}
		if opts == nil || opts.Workspace == nil {
			continue
		}
		saved := saveInboundFiles(deliveryID, []FileAttachment{{
			MimeType: images[i].MimeType,
			Data:     images[i].Data,
			FileName: images[i].FileName,
		}}, opts)
		if len(saved) > 0 {
			images[i].WorkPath = saved[0]
		}
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
			part.Source = path
		}
		parts = append(parts, part)
	}
	if len(parts) == 0 {
		parts = append(parts, agentkit.ContentPart{Type: "text", Text: ""})
	}
	return WithInboundRoute(agentkit.MessageEvent{
		AgentID:    agentID,
		PlatformID: strings.TrimSpace(route.Platform),
		UserID:     userID,
		Message: agentkit.ModelMessage{
			Role:    "user",
			Content: parts,
		},
	}, route)
}

// IsImageAttachment reports whether an inbound attachment should be sent to the
// model as vision input instead of a read-tool file path.
func IsImageAttachment(mimeType, filename string) bool {
	return rtmedia.IsImage(mimeType, filename)
}

func appendFileRefs(prompt string, filePaths []string) string {
	if len(filePaths) == 0 {
		return prompt
	}
	if prompt == "" {
		prompt = "Please analyze the attached file(s)."
	}
	return prompt + "\n\n(Files saved locally, please read them: " + strings.Join(filePaths, ", ") + ")"
}

func saveInboundFiles(deliveryID agentkit.SessionID, files []FileAttachment, opts *InboundOpts) []string {
	if opts == nil || opts.Workspace == nil {
		slog.Warn("common: inbound attachments require workspace")
		return nil
	}
	ctx := session.ApplyEnvelopeToContext(context.Background(), agentkit.TurnEnvelope{
		Conversation: string(deliveryID),
		Workspace:    session.WorkspaceKey(string(deliveryID)),
	})
	attachDir, err := workpath.ResolveFile(ctx, opts.Workspace, UploadWorkRel(opts.Workspace))
	if err != nil {
		slog.Warn("common: resolve inbound upload dir failed", "error", err)
		return nil
	}
	if err := os.MkdirAll(attachDir, 0o755); err != nil {
		slog.Warn("common: mkdir inbound upload dir failed", "dir", attachDir, "error", err)
		return nil
	}

	var paths []string
	for i, f := range files {
		if len(f.Data) == 0 {
			continue
		}
		fname := sanitizeAttachmentFileName(f.FileName)
		if fname == "" {
			fname = fmt.Sprintf("file_%d_%d", time.Now().UnixMilli(), i)
		}
		fname = rtmedia.EnsureInboundFileName(fname, f.MimeType, f.Data)
		fpath := filepath.Join(attachDir, fname)
		if err := os.WriteFile(fpath, f.Data, 0o644); err != nil {
			slog.Error("common: write inbound attachment failed", "path", fpath, "error", err)
			continue
		}
		// Paths are relative to the tenant local root so read can open them with root: .
		paths = append(paths, AttachFSRel(opts.Workspace, fname))
		slog.Debug("common: inbound upload saved", "path", fpath, "name", f.FileName, "size", len(f.Data))
	}
	return paths
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
