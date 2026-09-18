package feishu

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"

	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
)

func detectFeishuFileType(mimeType, fileName string) string {
	name := strings.ToLower(fileName)
	switch {
	case mimeType == "application/pdf" || strings.HasSuffix(name, ".pdf"):
		return larkim.CreateFileFileTypePdf
	case strings.HasSuffix(name, ".doc") || strings.HasSuffix(name, ".docx"):
		return larkim.CreateFileFileTypeDoc
	case strings.HasSuffix(name, ".xls") || strings.HasSuffix(name, ".xlsx") || strings.HasSuffix(name, ".csv"):
		return larkim.CreateFileFileTypeXls
	case strings.HasSuffix(name, ".ppt") || strings.HasSuffix(name, ".pptx"):
		return larkim.CreateFileFileTypePpt
	case mimeType == "video/mp4" || strings.HasSuffix(name, ".mp4"):
		return larkim.CreateFileFileTypeMp4
	case mimeType == "audio/ogg" || mimeType == "audio/opus" || strings.HasSuffix(name, ".opus"):
		return larkim.CreateFileFileTypeOpus
	default:
		return larkim.CreateFileFileTypeStream
	}
}

func (p *Platform) downloadImage(messageID, imageKey string) ([]byte, string, error) {
	resp, err := p.client.Im.MessageResource.Get(context.Background(),
		larkim.NewGetMessageResourceReqBuilder().
			MessageId(messageID).
			FileKey(imageKey).
			Type("image").
			Build())
	if err != nil {
		return nil, "", fmt.Errorf("%s: image API: %w", p.tag(), err)
	}
	if !resp.Success() {
		return nil, "", fmt.Errorf("%s: image API code=%d msg=%s", p.tag(), resp.Code, resp.Msg)
	}
	if resp.File == nil {
		return nil, "", fmt.Errorf("%s: image API returned nil file body", p.tag())
	}
	data, err := io.ReadAll(resp.File)
	if err != nil {
		return nil, "", fmt.Errorf("%s: read image: %w", p.tag(), err)
	}

	mimeType := detectMimeType(data)
	slog.Debug(p.tag()+": downloaded image", "key", imageKey, "size", len(data), "mime", mimeType)
	return data, mimeType, nil
}

func (p *Platform) downloadResource(messageID, fileKey, resType string) ([]byte, error) {
	resp, err := p.client.Im.MessageResource.Get(context.Background(),
		larkim.NewGetMessageResourceReqBuilder().
			MessageId(messageID).
			FileKey(fileKey).
			Type(resType).
			Build())
	if err != nil {
		return nil, fmt.Errorf("%s: resource API: %w", p.tag(), err)
	}
	if !resp.Success() {
		return nil, fmt.Errorf("%s: resource API code=%d msg=%s", p.tag(), resp.Code, resp.Msg)
	}
	if resp.File == nil {
		return nil, fmt.Errorf("%s: resource API returned nil file body", p.tag())
	}
	data, err := io.ReadAll(resp.File)
	if err != nil {
		return nil, fmt.Errorf("%s: read resource: %w", p.tag(), err)
	}
	slog.Debug(p.tag()+": downloaded resource", "key", fileKey, "type", resType, "size", len(data))
	return data, nil
}

func detectMimeType(data []byte) string {
	if len(data) >= 8 {
		if data[0] == 0x89 && data[1] == 'P' && data[2] == 'N' && data[3] == 'G' {
			return "image/png"
		}
		if data[0] == 0xFF && data[1] == 0xD8 {
			return "image/jpeg"
		}
		if string(data[:4]) == "GIF8" {
			return "image/gif"
		}
		if string(data[:4]) == "RIFF" && len(data) >= 12 && string(data[8:12]) == "WEBP" {
			return "image/webp"
		}
	}
	return http.DetectContentType(data)
}

// predictMsgType returns the message type that buildReplyContent will choose,
// without actually building the content. Used to select the correct at syntax
// before building.
func predictMsgType(content string) string {
	if !containsMarkdown(content) {
		return larkim.MsgTypeText
	}
	if countMarkdownTables(content) <= maxCardTables {
		return larkim.MsgTypeInteractive
	}
	return larkim.MsgTypePost
}
