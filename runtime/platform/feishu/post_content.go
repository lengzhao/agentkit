package feishu

import (
	"encoding/json"
	"log/slog"

	"github.com/lengzhao/agentkit/runtime/platform/common"
)

type postElement struct {
	Tag      string `json:"tag"`
	Text     string `json:"text,omitempty"`
	Language string `json:"language,omitempty"`
	ImageKey string `json:"image_key,omitempty"`
	Href     string `json:"href,omitempty"`
	UserId   string `json:"user_id,omitempty"`
	UserName string `json:"user_name,omitempty"`
}

type postLang struct {
	Title   string          `json:"title"`
	Content [][]postElement `json:"content"`
}

// parsePostContent handles both formats of feishu post content:
// 1. {"title":"...", "content":[[...]]}  (receive event)
// 2. {"zh_cn":{"title":"...", "content":[[...]]}}  (some SDK versions)
func (p *Platform) parsePostContent(messageID, raw string) ([]string, []common.ImageAttachment) {
	// try flat format first
	var flat postLang
	if err := json.Unmarshal([]byte(raw), &flat); err == nil && flat.Content != nil {
		return p.extractPostParts(messageID, &flat)
	}
	// try language-keyed format
	var langMap map[string]postLang
	if err := json.Unmarshal([]byte(raw), &langMap); err == nil {
		for _, lang := range langMap {
			return p.extractPostParts(messageID, &lang)
		}
	}
	slog.Error(p.tag()+": failed to parse post content", "raw", raw)
	return nil, nil
}

func (p *Platform) extractPostParts(messageID string, post *postLang) ([]string, []common.ImageAttachment) {
	var textParts []string
	var images []common.ImageAttachment
	if post.Title != "" {
		textParts = append(textParts, post.Title)
	}
	for _, line := range post.Content {
		for _, elem := range line {
			switch elem.Tag {
			case "text":
				if elem.Text != "" {
					textParts = append(textParts, elem.Text)
				}
			case "a":
				if elem.Text != "" {
					textParts = append(textParts, elem.Text)
				}
			case "code_block":
				if elem.Text != "" {
					lang := elem.Language
					textParts = append(textParts, "```"+lang+"\n"+elem.Text+"\n```")
				}
			case "markdown":
				if elem.Text != "" {
					textParts = append(textParts, elem.Text)
				}
			case "at":
				if p.botOpenID != "" && elem.UserId == p.botOpenID {
					continue
				}
				switch {
				case elem.UserId == "all":
					textParts = append(textParts, "@all")
				case elem.UserName != "":
					textParts = append(textParts, "@"+elem.UserName)
				case elem.UserId != "":
					textParts = append(textParts, "@"+p.resolveUserName(elem.UserId))
				}
			case "img":
				if elem.ImageKey != "" {
					imgData, mimeType, err := p.downloadImage(messageID, elem.ImageKey)
					if err != nil {
						slog.Error(p.tag()+": download post image failed", "error", err, "key", elem.ImageKey)
						continue
					}
					images = append(images, common.ImageAttachment{MimeType: mimeType, Data: imgData})
				}
			}
		}
	}
	return textParts, images
}
