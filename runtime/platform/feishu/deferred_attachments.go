package feishu

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/runtime/platform/common"
)

type deferredKey struct{ Session, User string }
type deferredAttachments struct {
	route      replyContext
	owner      agentkit.SessionID
	uploads    []agentkit.InboundUpload
	timer      *time.Timer
	generation uint64
}

func (p *Platform) deferAttachment(ctx context.Context, rc replyContext, user string, owner agentkit.SessionID, data []byte, mime, name string) error {
	staged, err := common.StageInboundUploads(agentkit.MessageEvent{Uploads: []agentkit.InboundUpload{{Data: data, MIME: mime, OrigName: name, ContentIndex: -1}}})
	if err != nil {
		return err
	}
	key := deferredKey{rc.sessionKey, user}
	p.deferredMu.Lock()
	if p.deferred == nil {
		p.deferred = make(map[deferredKey]*deferredAttachments)
	}
	entry := p.deferred[key]
	if entry != nil && entry.owner != owner {
		p.deferredMu.Unlock()
		common.ReleaseInboundUploads(staged)
		return fmt.Errorf("pending files belong to another logical session")
	}
	first := entry == nil
	if first {
		entry = &deferredAttachments{route: rc, owner: owner}
		p.deferred[key] = entry
	}
	entry.uploads = append(entry.uploads, staged.Uploads...)
	entry.generation++
	generation := entry.generation
	if entry.timer != nil {
		entry.timer.Stop()
	}
	entry.timer = time.AfterFunc(p.pendingAttachTTL(), func() {
		p.deferredMu.Lock()
		defer p.deferredMu.Unlock()
		if p.deferred[key] != entry || entry.generation != generation {
			return
		}
		delete(p.deferred, key)
		common.ReleaseInboundUploads(agentkit.MessageEvent{Uploads: entry.uploads})
	})
	p.deferredMu.Unlock()
	if first && p.client != nil {
		notice := "文件已收到，请继续说明需要我做什么。"
		if p.cfg.DMThreadMode && rc.chatType == "p2p" {
			notice = "文件已收到，请在此话题说明需要我做什么。"
		}
		if err := p.sendIMContent(ctx, rc, notice); err != nil {
			return fmt.Errorf("file buffered but receipt failed: %w", err)
		}
	}
	return nil
}

// deferredRoute never changes ownership. A unique top-level DM continuation is
// routed back to the original topic, rather than moving files into a new topic.
func (p *Platform) deferredRoute(rc replyContext, user string, topLevel bool) (replyContext, bool, bool) {
	p.deferredMu.Lock()
	defer p.deferredMu.Unlock()
	if entry := p.deferred[deferredKey{rc.sessionKey, user}]; entry != nil {
		return rc, true, false
	}
	if !topLevel || rc.chatType != "p2p" {
		return rc, false, false
	}
	var candidate *deferredAttachments
	for key, entry := range p.deferred {
		if key.User != user || entry.route.chatID != rc.chatID || entry.route.chatType != "p2p" {
			continue
		}
		if candidate != nil {
			return rc, false, true
		}
		candidate = entry
	}
	if candidate == nil {
		return rc, false, false
	}
	target := candidate.route
	return target, true, false
}

func (p *Platform) consumeDeferred(msg *inboundMessage, owner agentkit.SessionID) error {
	key := deferredKey{msg.rctx.sessionKey, msg.userID}
	p.deferredMu.Lock()
	defer p.deferredMu.Unlock()
	entry := p.deferred[key]
	if entry == nil {
		return nil
	}
	if owner == "" || entry.owner == "" || entry.owner != owner {
		return fmt.Errorf("pending files belong to a previous session; resend them in the intended session")
	}
	var images []common.ImageAttachment
	var files []common.FileAttachment
	for _, u := range entry.uploads {
		reader, err := u.Source.Open()
		if err != nil {
			return err
		}
		data, err := io.ReadAll(reader)
		closeErr := reader.Close()
		if err != nil {
			return err
		}
		if closeErr != nil {
			return closeErr
		}
		if strings.HasPrefix(u.MIME, "image/") {
			images = append(images, common.ImageAttachment{Data: data, MimeType: u.MIME, FileName: u.OrigName})
		} else {
			files = append(files, common.FileAttachment{Data: data, MimeType: u.MIME, FileName: u.OrigName})
		}
	}
	msg.images = append(msg.images, images...)
	msg.files = append(msg.files, files...)
	return nil
}

// commitDeferred releases pending input only after the runner queue accepted it.
func (p *Platform) commitDeferred(msg inboundMessage, owner agentkit.SessionID) {
	p.deferredMu.Lock()
	defer p.deferredMu.Unlock()
	key := deferredKey{msg.rctx.sessionKey, msg.userID}
	entry := p.deferred[key]
	if entry == nil || entry.owner != owner {
		return
	}
	delete(p.deferred, key)
	entry.timer.Stop()
	common.ReleaseInboundUploads(agentkit.MessageEvent{Uploads: entry.uploads})
}
func (p *Platform) flushDeferredAttachments() {
	p.deferredMu.Lock()
	defer p.deferredMu.Unlock()
	for key, entry := range p.deferred {
		entry.timer.Stop()
		common.ReleaseInboundUploads(agentkit.MessageEvent{Uploads: entry.uploads})
		delete(p.deferred, key)
	}
}

// A verified logical-session change retires old pending input instead of blocking
// new messages until its TTL. Unknown ownership is never guessed.
func (p *Platform) pruneDeferredAttachments(ctx context.Context, rc replyContext, user string) {
	if p.sessionStore == nil {
		return
	}
	p.deferredMu.Lock()
	candidates := make(map[deferredKey]*deferredAttachments)
	for key, entry := range p.deferred {
		if key.User == user && entry.route.chatID == rc.chatID {
			candidates[key] = entry
		}
	}
	p.deferredMu.Unlock()
	for key, entry := range candidates {
		env, _, err := p.ingressEnvelope(ctx, entry.route, user)
		if err != nil || env.Conversation == "" || entry.owner == "" || env.Conversation == string(entry.owner) {
			continue
		}
		p.deferredMu.Lock()
		if p.deferred[key] == entry {
			delete(p.deferred, key)
			entry.timer.Stop()
			common.ReleaseInboundUploads(agentkit.MessageEvent{Uploads: entry.uploads})
		}
		p.deferredMu.Unlock()
	}
}
