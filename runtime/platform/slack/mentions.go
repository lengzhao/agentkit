package slack

import (
	"regexp"
	"strings"

	"github.com/lengzhao/agentkit/runtime/platform/common"
)

var slackUserMentionRE = regexp.MustCompile(`<@([UW][A-Z0-9]+)(?:\|([^>]+))?>`)

type slackUserMention struct {
	id   string
	name string
}

func parseSlackUserMentions(text string) []slackUserMention {
	if text == "" {
		return nil
	}
	matches := slackUserMentionRE.FindAllStringSubmatch(text, -1)
	if len(matches) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(matches))
	mentions := make([]slackUserMention, 0, len(matches))
	for _, match := range matches {
		if len(match) < 2 {
			continue
		}
		id := strings.TrimSpace(match[1])
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		name := ""
		if len(match) > 2 {
			name = strings.TrimSpace(match[2])
		}
		mentions = append(mentions, slackUserMention{id: id, name: name})
	}
	return mentions
}

func (p *Platform) mentionMetadata(text string) map[string]any {
	mentions := parseSlackUserMentions(text)
	if len(mentions) == 0 {
		return nil
	}
	profiles := make([]common.MentionProfile, 0, len(mentions))
	for _, mention := range mentions {
		if p.botUserID != "" && mention.id == p.botUserID {
			continue
		}
		entry := p.cachedUserProfile(mention.id)
		name := mention.name
		if entry.ok && entry.name != "" {
			name = entry.name
		}
		email := ""
		if entry.ok {
			email = entry.email
		}
		profiles = append(profiles, common.MentionProfile{
			ID:    mention.id,
			Name:  name,
			Email: email,
		})
	}
	return common.MentionProfilesMetadata(profiles)
}

func (p *Platform) inboundMetadata(userID, text string) map[string]any {
	return common.MergeMetadata(p.userProfileMetadata(userID), p.mentionMetadata(text))
}
