package feishu

import (
	"context"
	"fmt"
	"html"
	"log/slog"
	"sort"
	"strings"
	"time"

	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"

	larkcore "github.com/larksuite/oapi-sdk-go/v3/core"
	larkcontact "github.com/larksuite/oapi-sdk-go/v3/service/contact/v3"
	"github.com/lengzhao/agentkit/runtime/platform/common"
)

// resolveUserName fetches a user's display name via the Contact API, with caching.
func (p *Platform) resolveUserName(openID string) string {
	if !isValidFeishuLookupID(openID) {
		return openID
	}
	entry := p.cachedUserProfile(openID)
	if entry.ok && entry.name != "" {
		return entry.name
	}
	return openID
}

type feishuUserProfileEntry struct {
	name  string
	email string
	ok    bool
}

func (p *Platform) cachedUserProfile(userID string) feishuUserProfileEntry {
	if cached, ok := p.userProfileCache.Load(userID); ok {
		return cached.(feishuUserProfileEntry)
	}
	entry := p.fetchUserProfile(userID)
	if entry.ok {
		p.userProfileCache.Store(userID, entry)
	}
	return entry
}

func (p *Platform) fetchUserProfile(userID string) feishuUserProfileEntry {
	if p.client == nil {
		return feishuUserProfileEntry{}
	}
	resp, err := p.client.Contact.User.Get(context.Background(),
		larkcontact.NewGetUserReqBuilder().
			UserId(userID).
			UserIdType("open_id").
			Build())
	if err != nil {
		slog.Debug(p.tag()+": user profile lookup failed", "open_id", userID, "error", err)
		return feishuUserProfileEntry{}
	}
	if !resp.Success() || resp.Data == nil || resp.Data.User == nil {
		slog.Debug(p.tag()+": user profile lookup: no data", "open_id", userID, "code", resp.Code)
		return feishuUserProfileEntry{}
	}
	user := resp.Data.User
	entry := feishuUserProfileEntry{ok: true}
	if user.Name != nil {
		entry.name = strings.TrimSpace(*user.Name)
	}
	if user.Email != nil {
		entry.email = strings.TrimSpace(*user.Email)
	}
	if entry.email == "" && user.EnterpriseEmail != nil {
		entry.email = strings.TrimSpace(*user.EnterpriseEmail)
	}
	return entry
}

func (p *Platform) userProfileMetadata(userID string) map[string]any {
	userID = strings.TrimSpace(userID)
	if userID == "" || !isValidFeishuLookupID(userID) {
		return nil
	}
	entry := p.cachedUserProfile(userID)
	if !entry.ok {
		return nil
	}
	return common.UserProfileMetadata(entry.name, entry.email)
}

func (p *Platform) mentionMetadata(mentions []*larkim.MentionEvent) map[string]any {
	if len(mentions) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(mentions))
	profiles := make([]common.MentionProfile, 0, len(mentions))
	for _, mention := range mentions {
		if mention == nil || mention.Id == nil {
			continue
		}
		if p.botOpenID != "" && mention.Id.OpenId != nil && *mention.Id.OpenId == p.botOpenID {
			continue
		}
		userID := userIDFromEvent(mention.Id)
		if userID == "" || !isValidFeishuLookupID(userID) {
			continue
		}
		if _, ok := seen[userID]; ok {
			continue
		}
		seen[userID] = struct{}{}

		name := ""
		if mention.Name != nil {
			name = strings.TrimSpace(*mention.Name)
		}
		entry := p.cachedUserProfile(userID)
		if name == "" && entry.ok {
			name = entry.name
		}
		email := ""
		if entry.ok {
			email = entry.email
		}
		profiles = append(profiles, common.MentionProfile{
			ID:    userID,
			Name:  name,
			Email: email,
		})
	}
	return common.MentionProfilesMetadata(profiles)
}

func userIDFromEvent(id *larkim.UserId) string {
	if id == nil {
		return ""
	}
	if id.OpenId != nil && *id.OpenId != "" {
		return *id.OpenId
	}
	if id.UserId != nil && *id.UserId != "" {
		return *id.UserId
	}
	if id.UnionId != nil && *id.UnionId != "" {
		return *id.UnionId
	}
	return ""
}

func isValidFeishuLookupID(id string) bool {
	if id == "" {
		return false
	}
	for _, r := range id {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9':
		case r == '_' || r == '-':
		default:
			return false
		}
	}
	return true
}

// resolveUserNames batch-resolves open_ids to display names.
func (p *Platform) resolveUserNames(openIDs []string) map[string]string {
	names := make(map[string]string, len(openIDs))
	for _, id := range openIDs {
		if _, ok := names[id]; !ok {
			names[id] = p.resolveUserName(id)
		}
	}
	return names
}

// resolveChatName fetches a chat/group name via the IM API, with caching.
func (p *Platform) resolveChatName(chatID string) string {
	if chatID == "" {
		return ""
	}
	if cached, ok := p.chatNameCache.Load(chatID); ok {
		return cached.(string)
	}
	resp, err := p.client.Im.Chat.Get(context.Background(),
		larkim.NewGetChatReqBuilder().ChatId(chatID).Build())
	if err != nil {
		slog.Debug(p.tag()+": resolve chat name failed", "chat_id", chatID, "error", err)
		return chatID
	}
	if !resp.Success() || resp.Data == nil || resp.Data.Name == nil {
		slog.Debug(p.tag()+": resolve chat name: no data", "chat_id", chatID, "code", resp.Code)
		return chatID
	}
	name := *resp.Data.Name
	if name == "" {
		return chatID
	}
	p.chatNameCache.Store(chatID, name)
	return name
}

// --- Mention resolution ---

const chatMemberCacheTTL = 1 * time.Hour

type chatMemberEntry struct {
	members   map[string]string // displayName -> openID
	fetchedAt time.Time
}

// fetchChatMembers retrieves all members of a chat and returns a name->openID map.
func (p *Platform) fetchChatMembers(ctx context.Context, chatID string) (map[string]string, error) {
	if p.client == nil {
		return nil, fmt.Errorf("%s: client not initialized", p.tag())
	}
	members := make(map[string]string)
	req := larkim.NewGetChatMembersReqBuilder().
		ChatId(chatID).
		MemberIdType("open_id").
		PageSize(100).
		Build()
	timeoutCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	token, err := p.fetchFreshTenantAccessToken(timeoutCtx)
	if err != nil {
		return nil, fmt.Errorf("%s: fetch tenant token for chat members: %w", p.tag(), err)
	}
	iter, err := p.client.Im.ChatMembers.GetByIterator(timeoutCtx, req, larkcore.WithTenantAccessToken(token))
	if err != nil {
		return nil, fmt.Errorf("%s: list chat members: %w", p.tag(), err)
	}
	for {
		hasMore, member, err := iter.Next()
		if err != nil {
			slog.Debug(p.tag()+": fetch chat members page error", "chat_id", chatID, "error", err)
			break
		}
		if member != nil && member.Name != nil && member.MemberId != nil {
			name := *member.Name
			if _, exists := members[name]; !exists {
				members[name] = *member.MemberId
			} else {
				members[name] = ""
			}
		}
		if !hasMore {
			break
		}
	}
	return members, nil
}

// getChatMembers returns the cached name->openID map for a chat, fetching if needed.
func (p *Platform) getChatMembers(ctx context.Context, chatID string) map[string]string {
	if v, ok := p.chatMemberCache.Load(chatID); ok {
		entry := v.(*chatMemberEntry)
		if time.Since(entry.fetchedAt) < chatMemberCacheTTL {
			return entry.members
		}
	}
	members, err := p.fetchChatMembers(ctx, chatID)
	if err != nil {
		slog.Debug(p.tag()+": fetch chat members failed", "chat_id", chatID, "error", err)
		return nil
	}
	p.chatMemberCache.Store(chatID, &chatMemberEntry{members: members, fetchedAt: time.Now()})
	return members
}

// resolveMentionsInContent replaces @name with Feishu at tags in raw content
// (before JSON serialization). Reverse-matches against the chat member list,
// longest name first. Uses the correct at syntax based on predicted message type.
func (p *Platform) resolveMentionsInContent(ctx context.Context, chatID, content string) string {
	if !p.resolveMentions || chatID == "" || !strings.Contains(content, "@") {
		return content
	}
	members := p.getChatMembers(ctx, chatID)
	if len(members) == 0 {
		return content
	}
	// Sort names longest-first to avoid partial matches.
	names := make([]string, 0, len(members))
	for name := range members {
		names = append(names, name)
	}
	sort.Slice(names, func(i, j int) bool { return len(names[i]) > len(names[j]) })

	useCardFormat := predictMsgType(content) == larkim.MsgTypeInteractive
	result := content
	for _, name := range names {
		pattern := "@" + name
		if !strings.Contains(result, pattern) {
			continue
		}
		openID := members[name]
		if openID == "" {
			slog.Debug(p.tag()+": skipping ambiguous mention", "name", name)
			continue
		}
		var atTag string
		if useCardFormat {
			atTag = fmt.Sprintf(`<at id=%s></at>`, openID)
		} else {
			escapedName := html.EscapeString(name)
			atTag = fmt.Sprintf(`<at user_id="%s">%s</at>`, openID, escapedName)
		}
		slog.Debug(p.tag()+": mention resolved", "name", name, "card_format", useCardFormat)
		result = strings.ReplaceAll(result, pattern, atTag)
	}
	return result
}
