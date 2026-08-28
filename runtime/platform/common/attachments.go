package common

import (
	"encoding/base64"
	"fmt"
	"strings"

	"github.com/lengzhao/agentkit"
)

// ImageAttachment is an inbound image from an IM platform.
type ImageAttachment struct {
	MimeType string
	Data     []byte
	FileName string
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

// InboundFromContent builds a MessageEvent from text and optional media.
// extraContent is prepended (e.g. quoted reply context). filePaths are appended to text.
func InboundFromContent(agentID agentkit.AgentID, sessionID agentkit.SessionID, platformID, userID, content, extraContent string, images []ImageAttachment, files []FileAttachment, audio *AudioAttachment, filePaths []string) agentkit.MessageEvent {
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
		ref := "\n\n(Files saved locally, please read them: " + strings.Join(filePaths, ", ") + ")"
		if text == "" {
			text = "Please analyze the attached file(s)." + ref
		} else {
			text += ref
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
		url := "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(img.Data)
		parts = append(parts, agentkit.ContentPart{Type: "image_url", URL: url, MIME: mime})
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
