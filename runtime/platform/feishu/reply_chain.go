package feishu

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	larkcore "github.com/larksuite/oapi-sdk-go/v3/core"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
)

// chainMessage holds extracted data from one message in a reply chain.
type chainMessage struct {
	senderName string
	senderType string // "user" or "app"
	text       string
	parentID   string
}

// maxReplyChainDepth is the maximum number of parent messages to traverse
// when building a reply chain. This limits API calls per inbound reply.
const maxReplyChainDepth = 5

// fetchQuotedMessage retrieves the content of a parent message that the user
// is replying to, and returns a formatted prefix string for context injection.
// For multi-level reply chains, it traces parent_id links up to maxReplyChainDepth
// levels and returns the full conversation chain.
// Returns empty string on any failure (graceful degradation — the user's own
// message is still delivered without the quote).
func (p *Platform) fetchQuotedMessage(ctx context.Context, parentID string) string {
	chain := p.fetchReplyChain(ctx, parentID, maxReplyChainDepth)
	if len(chain) == 0 {
		return ""
	}
	return formatReplyChain(chain)
}

// resolveBotSenderName returns a display name for a bot sender in a quoted
// reply chain. Feishu sets sender.id to the bot's app_id (globally stable,
// not an open_id). We consult the peer_bots config to map app_id → alias;
// if the app is unknown, we surface the app_id so operators can add it to
// the config rather than seeing an ambiguous "Bot".
func (p *Platform) resolveBotSenderName(appID string) string {
	if appID == "" {
		return "Bot"
	}
	if alias := p.peerBots[appID]; alias != "" {
		return alias
	}
	return "Bot[" + appID + "]"
}

// fetchSingleMessage retrieves one message by ID from the Feishu API and
// returns its extracted content as a chainMessage. Returns nil on any failure.
func (p *Platform) fetchSingleMessage(ctx context.Context, messageID string) *chainMessage {
	apiPath := fmt.Sprintf("/open-apis/im/v1/messages/%s?card_msg_content_type=raw_card_content", messageID)
	apiResp, err := p.client.Get(ctx, apiPath, nil, larkcore.AccessTokenTypeTenant)
	if err != nil {
		slog.Debug(p.tag()+": fetch single message failed", "message_id", messageID, "error", err)
		return nil
	}
	var resp struct {
		Code int `json:"code"`
		Data struct {
			Items []struct {
				MsgType  string `json:"msg_type"`
				ParentID string `json:"parent_id"`
				Sender   struct {
					ID         string `json:"id"`
					SenderType string `json:"sender_type"`
				} `json:"sender"`
				Body struct {
					Content string `json:"content"`
				} `json:"body"`
				Mentions []*larkim.Mention `json:"mentions"`
			} `json:"items"`
		} `json:"data"`
	}
	if err := json.Unmarshal(apiResp.RawBody, &resp); err != nil || resp.Code != 0 || len(resp.Data.Items) == 0 {
		slog.Debug(p.tag()+": fetch single message: parse failed or no data", "message_id", messageID)
		return nil
	}

	item := resp.Data.Items[0]
	content := item.Body.Content
	if content == "" {
		return nil
	}

	// Extract plain text based on message type.
	var text string
	switch item.MsgType {
	case "text":
		var textBody struct {
			Text string `json:"text"`
		}
		if err := json.Unmarshal([]byte(content), &textBody); err == nil {
			text = replaceMentions(textBody.Text, item.Mentions)
		}
	case "post":
		text = extractPostPlainText(content)
	case "interactive":
		text = extractInteractiveCardText(content)
	default:
		text = fmt.Sprintf("[%s]", item.MsgType)
	}
	if text == "" {
		return nil
	}

	// Resolve sender name.
	senderName := ""
	if item.Sender.SenderType == "app" {
		senderName = p.resolveBotSenderName(item.Sender.ID)
	} else if item.Sender.ID != "" {
		resolved := p.resolveUserName(item.Sender.ID)
		if resolved != item.Sender.ID {
			senderName = resolved
		} else {
			senderName = "User"
		}
	}
	if senderName == "" {
		senderName = "unknown"
	}

	return &chainMessage{
		senderName: senderName,
		senderType: item.Sender.SenderType,
		text:       text,
		parentID:   item.ParentID,
	}
}

// fetchReplyChain iteratively traverses parent_id links to build a reply chain.
// Returns messages in chronological order (oldest first). Stops on any failure,
// circular reference, or when maxDepth is reached.
func (p *Platform) fetchReplyChain(ctx context.Context, parentID string, maxDepth int) []chainMessage {
	var chain []chainMessage
	visited := make(map[string]struct{})
	currentID := parentID

	for currentID != "" && len(chain) < maxDepth {
		if _, seen := visited[currentID]; seen {
			slog.Debug(p.tag()+": reply chain: circular reference detected", "message_id", currentID)
			break
		}
		visited[currentID] = struct{}{}

		msg := p.fetchSingleMessage(ctx, currentID)
		if msg == nil {
			break
		}
		chain = append(chain, *msg)
		currentID = msg.parentID
	}

	// Reverse to chronological order (oldest first).
	for i, j := 0, len(chain)-1; i < j; i, j = i+1, j-1 {
		chain[i], chain[j] = chain[j], chain[i]
	}
	return chain
}

// formatReplyChain formats a slice of chain messages into a readable string.
// Single-message chains use the legacy format for backward compatibility.
// Multi-message chains use a numbered format with role labels.
func formatReplyChain(chain []chainMessage) string {
	if len(chain) == 0 {
		return ""
	}

	// Single message: backward-compatible format.
	if len(chain) == 1 {
		return fmt.Sprintf("[Quoted message from %s]:\n%s\n\n", chain[0].senderName, chain[0].text)
	}

	// Multi-message: numbered chain format.
	var b strings.Builder
	fmt.Fprintf(&b, "--- Reply chain (%d messages) ---\n", len(chain))
	for i, msg := range chain {
		role := "user"
		if msg.senderType == "app" {
			role = "assistant"
		}
		fmt.Fprintf(&b, "[%d] %s (%s):\n%s\n\n", i+1, msg.senderName, role, msg.text)
	}
	b.WriteString("---\n\n")
	return b.String()
}

// extractPostPlainText extracts plain text from a Lark post (rich text) JSON content.
func extractPostPlainText(content string) string {
	var post struct {
		Content [][]struct {
			Tag      string `json:"tag"`
			Text     string `json:"text"`
			Language string `json:"language,omitempty"`
			UserId   string `json:"user_id,omitempty"`
			UserName string `json:"user_name,omitempty"`
		} `json:"content"`
		Title string `json:"title"`
	}
	// Post content may be wrapped in a locale key like {"zh_cn": {...}}.
	// Try direct parse first, then try extracting from locale wrapper.
	if err := json.Unmarshal([]byte(content), &post); err != nil || len(post.Content) == 0 {
		var localeWrapper map[string]json.RawMessage
		if err2 := json.Unmarshal([]byte(content), &localeWrapper); err2 == nil {
			for _, v := range localeWrapper {
				if err3 := json.Unmarshal(v, &post); err3 == nil && len(post.Content) > 0 {
					break
				}
			}
		}
	}
	if len(post.Content) == 0 {
		return ""
	}
	var parts []string
	if post.Title != "" {
		parts = append(parts, post.Title)
	}
	for _, para := range post.Content {
		var line []string
		for _, elem := range para {
			switch elem.Tag {
			case "text":
				if elem.Text != "" {
					line = append(line, elem.Text)
				}
			case "a":
				if elem.Text != "" {
					line = append(line, elem.Text)
				}
			case "markdown":
				if elem.Text != "" {
					line = append(line, elem.Text)
				}
			case "at":
				switch {
				case elem.UserId == "all":
					line = append(line, "@all")
				case elem.UserName != "":
					line = append(line, "@"+elem.UserName)
				case elem.UserId != "":
					line = append(line, "@user")
				}
			case "img":
				line = append(line, "[image]")
			case "code_block":
				if elem.Text != "" {
					lang := elem.Language
					line = append(line, "```"+lang+"\n"+elem.Text+"\n```")
				}
			}
		}
		if len(line) > 0 {
			parts = append(parts, strings.Join(line, ""))
		}
	}
	return strings.Join(parts, "\n")
}
