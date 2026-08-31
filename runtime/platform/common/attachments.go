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
	"github.com/lengzhao/agentkit/cap/media"
	"github.com/lengzhao/agentkit/cap/workspace"
)

// ImageAttachment is an inbound image from an IM platform.
type ImageAttachment struct {
	MimeType string
	Data     []byte
	FileName string
	// WorkPath is workspace-relative under work/ (e.g. upload/foo.png).
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

const (
	// inboundAttachWorkRoot is the fs-workspace root relative to tenant local root.
	inboundAttachWorkRoot = "work"
	// inboundUploadDir is where user-uploaded files land under the work root.
	inboundUploadDir = "upload"
)

// UploadWorkRel is the workspace-relative upload directory (under tool/fs-workspace root).
func UploadWorkRel() string {
	return filepath.Join(inboundAttachWorkRoot, inboundUploadDir)
}

// InboundOpts configures optional inbound media handling.
type InboundOpts struct {
	// Workspace resolves upload paths; when set, inbound files land under the
	// same tenant root as session/store and tool/fs-workspace.
	Workspace workspace.Service
}

// InboundOptsFor builds inbound media options from an optional workspace.
func InboundOptsFor(ws workspace.Service) *InboundOpts {
	if ws == nil {
		return nil
	}
	return &InboundOpts{Workspace: ws}
}

// InboundFromContent builds a MessageEvent from text and optional media.
// extraContent is prepended (e.g. quoted reply context). Non-image files are
// saved under the tenant work dir and referenced in the prompt for the read tool.
func InboundFromContent(agentID agentkit.AgentID, sessionID agentkit.SessionID, platformID, userID, content, extraContent string, images []ImageAttachment, files []FileAttachment, audio *AudioAttachment, filePaths []string, opts *InboundOpts) agentkit.MessageEvent {
	if len(files) > 0 {
		saved := saveInboundFiles(sessionID, files, opts)
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
		saved := saveInboundFiles(sessionID, []FileAttachment{{
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
		url := media.DataURL(mime, img.Data)
		part := agentkit.ContentPart{Type: "image_url", URL: url, MIME: mime}
		if path := strings.TrimSpace(img.WorkPath); path != "" {
			part.Source = path
		}
		parts = append(parts, part)
	}
	if len(parts) == 0 {
		parts = append(parts, agentkit.ContentPart{Type: "text", Text: ""})
	}
	return agentkit.MessageEvent{
		SessionID:  sessionID,
		AgentID:    agentID,
		PlatformID: platformID,
		UserID:     userID,
		Message: agentkit.ModelMessage{
			Role:    "user",
			Content: parts,
		},
	}
}

// IsImageAttachment reports whether an inbound attachment should be sent to the
// model as vision input instead of a read-tool file path.
func IsImageAttachment(mimeType, filename string) bool {
	mime := strings.ToLower(strings.TrimSpace(mimeType))
	if strings.HasPrefix(mime, "image/") {
		return true
	}
	switch strings.ToLower(filepath.Ext(filename)) {
	case ".png", ".jpg", ".jpeg", ".gif", ".webp", ".bmp", ".heic", ".heif":
		return true
	default:
		return false
	}
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

func saveInboundFiles(sessionID agentkit.SessionID, files []FileAttachment, opts *InboundOpts) []string {
	if opts == nil || opts.Workspace == nil {
		slog.Warn("common: inbound attachments require workspace")
		return nil
	}
	ctx := context.WithValue(context.Background(), agentkit.KeySessionID, sessionID)
	attachDir, err := opts.Workspace.Resolve(ctx, UploadWorkRel())
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
		fpath := filepath.Join(attachDir, fname)
		if err := os.WriteFile(fpath, f.Data, 0o644); err != nil {
			slog.Error("common: write inbound attachment failed", "path", fpath, "error", err)
			continue
		}
		// Paths are relative to the fs-workspace root (work/) so read tool can open them.
		paths = append(paths, filepath.Join(inboundUploadDir, fname))
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
