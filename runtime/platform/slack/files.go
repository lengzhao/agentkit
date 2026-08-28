package slack

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/lengzhao/agentkit/runtime/platform/common"
	"github.com/slack-go/slack"
	"github.com/slack-go/slack/slackevents"
)

func (p *Platform) channelAllowed(channelID, channelType string) bool {
	if channelType == "im" || isDirectMessageChannel(channelID, channelType) {
		return true
	}
	allow := strings.TrimSpace(p.cfg.AllowChannels)
	if allow == "" || allow == "*" {
		return true
	}
	if common.AllowList(allow, channelID) {
		return true
	}
	var name string
	p.channelCacheMu.RLock()
	name, nameCached := p.channelNameCache[channelID]
	p.channelCacheMu.RUnlock()
	if !nameCached && p.client != nil {
		resolved, err := p.ResolveChannelName(channelID)
		if err == nil {
			name = resolved
			nameCached = name != ""
		}
	}
	if !nameCached || name == "" {
		return false
	}
	if common.AllowList(allow, name) {
		return true
	}
	for _, entry := range strings.Split(allow, ",") {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		if strings.EqualFold(strings.TrimPrefix(entry, "#"), name) {
			return true
		}
	}
	return false
}

func inferSlackChannelType(channelID string) string {
	if strings.HasPrefix(channelID, "D") {
		return "im"
	}
	return "channel"
}

func (p *Platform) shouldProcessChannelMessage(channelType string, fromAppMention bool) bool {
	if fromAppMention {
		return true
	}
	if isDirectMessage(channelType) {
		return true
	}
	if p.cfg.GroupReplyAll {
		return true
	}
	return false
}

func parseSlackInnerEventFiles(raw *json.RawMessage) []slackevents.File {
	if raw == nil || len(*raw) == 0 {
		return nil
	}
	var wrapper struct {
		Files []slackevents.File `json:"files"`
	}
	if err := json.Unmarshal(*raw, &wrapper); err != nil {
		return nil
	}
	return wrapper.Files
}

func messageEventFiles(ev *slackevents.MessageEvent) []slackevents.File {
	if ev == nil || ev.Message == nil {
		return nil
	}
	out := make([]slackevents.File, 0, len(ev.Message.Files))
	for _, f := range ev.Message.Files {
		out = append(out, slackevents.File{
			ID:                 f.ID,
			Name:               f.Name,
			Title:              f.Title,
			Mimetype:           f.Mimetype,
			URLPrivate:         f.URLPrivate,
			URLPrivateDownload: f.URLPrivateDownload,
		})
	}
	return out
}

func (p *Platform) processSlackFileShares(files []slackevents.File) (images []common.ImageAttachment, audio *common.AudioAttachment, docFiles []common.FileAttachment) {
	for _, f := range files {
		fileURL := f.URLPrivateDownload
		if fileURL == "" {
			fileURL = f.URLPrivate
		}
		if fileURL == "" {
			continue
		}
		mt := strings.TrimSpace(strings.ToLower(f.Mimetype))
		switch {
		case strings.HasPrefix(mt, "audio/"):
			data, err := p.downloadSlackFile(fileURL)
			if err != nil {
				slog.Error("slack: download audio failed", "error", err)
				continue
			}
			format := "mp3"
			if parts := strings.SplitN(mt, "/", 2); len(parts) == 2 {
				format = parts[1]
			}
			audioMime := f.Mimetype
			if audioMime == "" {
				audioMime = mt
			}
			audio = &common.AudioAttachment{MimeType: audioMime, Data: data, Format: format}
		case strings.HasPrefix(mt, "image/"):
			imgData, err := p.downloadSlackFile(fileURL)
			if err != nil {
				slog.Error("slack: download image failed", "error", err)
				continue
			}
			images = append(images, common.ImageAttachment{
				MimeType: f.Mimetype, Data: imgData, FileName: slackFileDisplayName(f),
			})
		default:
			data, err := p.downloadSlackFile(fileURL)
			if err != nil {
				slog.Error("slack: download file failed", "error", err)
				continue
			}
			if mt == "" {
				mt = "application/octet-stream"
			}
			docFiles = append(docFiles, common.FileAttachment{
				MimeType: mt, Data: data, FileName: slackFileDisplayName(f),
			})
		}
	}
	return images, audio, docFiles
}

func slackFileDisplayName(f slackevents.File) string {
	if f.Name != "" {
		return f.Name
	}
	return f.Title
}

func (p *Platform) downloadSlackFile(url string) ([]byte, error) {
	if url == "" {
		return nil, fmt.Errorf("empty URL")
	}
	req, _ := http.NewRequest(http.MethodGet, url, nil)
	req.Header.Set("Authorization", "Bearer "+p.cfg.BotToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%s", common.RedactToken(err.Error(), p.cfg.BotToken))
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, fmt.Errorf("download failed with status %d: %s", resp.StatusCode, string(body))
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if len(data) > 0 && (bytes.HasPrefix(data, []byte("<!DOCTYPE")) || bytes.HasPrefix(data, []byte("<html"))) {
		return nil, fmt.Errorf("received HTML response (likely missing auth)")
	}
	return data, nil
}

func (p *Platform) ResolveChannelName(channelID string) (string, error) {
	p.channelCacheMu.RLock()
	if name, ok := p.channelNameCache[channelID]; ok {
		p.channelCacheMu.RUnlock()
		return name, nil
	}
	p.channelCacheMu.RUnlock()
	info, err := p.client.GetConversationInfo(&slack.GetConversationInfoInput{ChannelID: channelID})
	if err != nil {
		return "", err
	}
	p.channelCacheMu.Lock()
	p.channelNameCache[channelID] = info.Name
	p.channelCacheMu.Unlock()
	return info.Name, nil
}

func stripAppMentionText(text string) string {
	if idx := strings.Index(text, "> "); idx != -1 && strings.HasPrefix(text, "<@") {
		return strings.TrimSpace(text[idx+2:])
	}
	return stripBotMention(text)
}

func isSlackMessageOld(ts string) bool {
	if ts == "" {
		return false
	}
	dotIdx := strings.IndexByte(ts, '.')
	if dotIdx <= 0 {
		return false
	}
	sec, err := strconvParseInt64(ts[:dotIdx])
	if err != nil {
		return false
	}
	return common.IsOldMessage(time.Unix(sec, 0))
}

func strconvParseInt64(s string) (int64, error) {
	var n int64
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0, fmt.Errorf("invalid")
		}
		n = n*10 + int64(c-'0')
	}
	return n, nil
}

// startTypingProgress adds progressive emoji reactions while processing.
func (p *Platform) startTypingProgress(_ context.Context, d delivery) (stop func()) {
	if p.client == nil || d.channel == "" || d.msgTS == "" {
		return func() {}
	}
	ref := slack.ItemRef{Channel: d.channel, Timestamp: d.msgTS}
	var mu sync.Mutex
	var added []string
	addReaction := func(emoji string) {
		if err := p.client.AddReaction(emoji, ref); err != nil {
			return
		}
		mu.Lock()
		added = append(added, emoji)
		mu.Unlock()
	}
	addReaction("eyes")
	done := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		timer := time.NewTimer(2 * time.Minute)
		defer timer.Stop()
		select {
		case <-timer.C:
			addReaction("clock1")
		case <-done:
			return
		}
		extras := []string{"hourglass_flowing_sand", "gear", "bulb", "rocket", "zap"}
		ticker := time.NewTicker(5 * time.Minute)
		defer ticker.Stop()
		idx := 0
		for {
			select {
			case <-ticker.C:
				if idx < len(extras) {
					addReaction(extras[idx])
					idx++
				}
			case <-done:
				return
			}
		}
	}()
	return func() {
		close(done)
		wg.Wait()
		for _, emoji := range added {
			_ = p.client.RemoveReaction(emoji, ref)
		}
	}
}
