package feishu

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"

	"github.com/lengzhao/agentkit/runtime/platform/common"
)

// parseMergeForward fetches sub-messages of a merge_forward message via the
// GET /open-apis/im/v1/messages/{message_id} API, then formats them into
// readable text. Returns combined text, images, and files from the sub-messages.
func (p *Platform) parseMergeForward(rootMessageID string) (string, []common.ImageAttachment, []common.FileAttachment) {
	resp, err := p.client.Im.Message.Get(context.Background(),
		larkim.NewGetMessageReqBuilder().
			MessageId(rootMessageID).
			Build())
	if err != nil {
		slog.Error(p.tag()+": fetch merge_forward sub-messages failed", "error", err)
		return "", nil, nil
	}
	if !resp.Success() {
		slog.Error(p.tag()+": fetch merge_forward sub-messages failed", "code", resp.Code, "msg", resp.Msg)
		return "", nil, nil
	}
	if resp.Data == nil || len(resp.Data.Items) == 0 {
		slog.Warn(p.tag()+": merge_forward has no sub-messages", "message_id", rootMessageID)
		return "", nil, nil
	}

	items := resp.Data.Items
	slog.Info(p.tag()+": merge_forward sub-messages fetched", "message_id", rootMessageID, "count", len(items))

	// Build tree: group children by upper_message_id, collect sender IDs
	childrenMap := make(map[string][]*larkim.Message)
	senderIDs := make(map[string]struct{})
	for _, item := range items {
		if item.MessageId != nil && *item.MessageId == rootMessageID {
			continue // skip root container
		}
		parentID := ""
		if item.UpperMessageId != nil {
			parentID = *item.UpperMessageId
		}
		if parentID == "" || parentID == rootMessageID {
			parentID = rootMessageID
		}
		childrenMap[parentID] = append(childrenMap[parentID], item)
		if item.Sender != nil && item.Sender.Id != nil {
			senderIDs[*item.Sender.Id] = struct{}{}
		}
	}

	// Resolve sender IDs to display names
	uniqueIDs := make([]string, 0, len(senderIDs))
	for id := range senderIDs {
		uniqueIDs = append(uniqueIDs, id)
	}
	nameMap := p.resolveUserNames(uniqueIDs)

	var allImages []common.ImageAttachment
	var allFiles []common.FileAttachment
	var sb strings.Builder
	sb.WriteString("<forwarded_messages>\n")
	p.formatMergeForwardTree(rootMessageID, childrenMap, nameMap, &sb, &allImages, &allFiles, 0)
	sb.WriteString("</forwarded_messages>")

	return sb.String(), allImages, allFiles
}

// replaceMentions replaces @_user_N placeholders with real names from the Mentions list.
func replaceMentions(text string, mentions []*larkim.Mention) string {
	for _, m := range mentions {
		if m.Key != nil && m.Name != nil {
			text = strings.ReplaceAll(text, *m.Key, "@"+*m.Name)
		}
	}
	return text
}

// formatMergeForwardTree recursively formats the sub-message tree.
func (p *Platform) formatMergeForwardTree(parentID string, childrenMap map[string][]*larkim.Message, nameMap map[string]string, sb *strings.Builder, images *[]common.ImageAttachment, files *[]common.FileAttachment, depth int) {
	if depth > 10 {
		sb.WriteString(strings.Repeat("    ", depth))
		sb.WriteString("[nested forwarding truncated]\n")
		return
	}
	children := childrenMap[parentID]
	indent := strings.Repeat("    ", depth)

	for _, item := range children {
		msgID := ""
		if item.MessageId != nil {
			msgID = *item.MessageId
		}
		msgType := ""
		if item.MsgType != nil {
			msgType = *item.MsgType
		}
		senderID := ""
		if item.Sender != nil && item.Sender.Id != nil {
			senderID = *item.Sender.Id
		}
		senderName := senderID
		if name, ok := nameMap[senderID]; ok {
			senderName = name
		}

		// Format timestamp
		ts := ""
		if item.CreateTime != nil {
			if ms, err := strconv.ParseInt(*item.CreateTime, 10, 64); err == nil {
				ts = time.Unix(ms/1000, 0).Format("2006-01-02 15:04:05")
			}
		}

		content := ""
		if item.Body != nil && item.Body.Content != nil {
			content = *item.Body.Content
		}

		switch msgType {
		case "text":
			var textBody struct {
				Text string `json:"text"`
			}
			if err := json.Unmarshal([]byte(content), &textBody); err == nil && textBody.Text != "" {
				msgText := replaceMentions(textBody.Text, item.Mentions)
				sb.WriteString(fmt.Sprintf("%s[%s] %s:\n", indent, ts, senderName))
				for _, line := range strings.Split(msgText, "\n") {
					sb.WriteString(fmt.Sprintf("%s    %s\n", indent, line))
				}
			}

		case "post":
			textParts, postImages := p.parsePostContent(msgID, content)
			*images = append(*images, postImages...)
			text := replaceMentions(strings.Join(textParts, "\n"), item.Mentions)
			if text != "" {
				sb.WriteString(fmt.Sprintf("%s[%s] %s:\n", indent, ts, senderName))
				for _, line := range strings.Split(text, "\n") {
					sb.WriteString(fmt.Sprintf("%s    %s\n", indent, line))
				}
			}

		case "image":
			var imgBody struct {
				ImageKey string `json:"image_key"`
			}
			if err := json.Unmarshal([]byte(content), &imgBody); err == nil && imgBody.ImageKey != "" {
				imgData, mimeType, err := p.downloadImage(msgID, imgBody.ImageKey)
				if err != nil {
					slog.Error(p.tag()+": download merge_forward image failed", "error", err)
					sb.WriteString(fmt.Sprintf("%s[%s] %s: [image - download failed]\n", indent, ts, senderName))
				} else {
					*images = append(*images, common.ImageAttachment{MimeType: mimeType, Data: imgData})
					sb.WriteString(fmt.Sprintf("%s[%s] %s: [image]\n", indent, ts, senderName))
				}
			}

		case "file":
			var fileBody struct {
				FileKey  string `json:"file_key"`
				FileName string `json:"file_name"`
			}
			if err := json.Unmarshal([]byte(content), &fileBody); err == nil && fileBody.FileKey != "" {
				fileData, err := p.downloadResource(msgID, fileBody.FileKey, "file")
				if err != nil {
					slog.Error(p.tag()+": download merge_forward file failed", "error", err)
					sb.WriteString(fmt.Sprintf("%s[%s] %s: [file: %s - download failed]\n", indent, ts, senderName, fileBody.FileName))
				} else {
					mt := detectMimeType(fileData)
					*files = append(*files, common.FileAttachment{MimeType: mt, Data: fileData, FileName: fileBody.FileName})
					sb.WriteString(fmt.Sprintf("%s[%s] %s: [file: %s]\n", indent, ts, senderName, fileBody.FileName))
				}
			}

		case "merge_forward":
			sb.WriteString(fmt.Sprintf("%s[%s] %s: [forwarded messages]\n", indent, ts, senderName))
			p.formatMergeForwardTree(msgID, childrenMap, nameMap, sb, images, files, depth+1)

		default:
			sb.WriteString(fmt.Sprintf("%s[%s] %s: [%s message]\n", indent, ts, senderName, msgType))
		}
	}
}
