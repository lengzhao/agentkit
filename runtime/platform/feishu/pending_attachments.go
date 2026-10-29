package feishu

import (
	"context"
	"log/slog"
	"os"
	"time"

	"github.com/lengzhao/agentkit"
	capsession "github.com/lengzhao/agentkit/cap/session"
	"github.com/lengzhao/agentkit/runtime/platform/common"
	"github.com/lengzhao/agentkit/runtime/rctx"
	"github.com/lengzhao/agentkit/runtime/session/sessctx"
)

// defaultPendingAttachTTL is how long a buffered attachment waits for a
// follow-up text message before being silently dropped. Feishu sends files
// and text as separate messages; the buffer bridges "file first, text later"
// into a single user turn. Three minutes covers the typical typing gap while
// keeping memory bounded.
const defaultPendingAttachTTL = 3 * time.Minute

// pendingAttachEntry holds attachments received for a session that have not
// yet been paired with a text message. The timer fires to evict the entry
// after pendingAttachTTL of inactivity so stale buffers don't linger.
type pendingAttachEntry struct {
	attachments []common.PresavedAttachment
	timer       *time.Timer
}

// bufferAttachment saves the attachment bytes to upload/ and records the
// resulting workspace path for later merging with a text message. If an entry
// already exists for the session, the attachment is appended and the TTL
// timer is reset.
func (p *Platform) bufferAttachment(sessionKey string, att common.PresavedAttachment) {
	p.pendingMu.Lock()
	defer p.pendingMu.Unlock()
	if p.pending == nil {
		p.pending = make(map[string]*pendingAttachEntry)
	}
	entry, ok := p.pending[sessionKey]
	if !ok {
		entry = &pendingAttachEntry{}
		p.pending[sessionKey] = entry
	}
	entry.attachments = append(entry.attachments, att)
	if entry.timer != nil {
		entry.timer.Stop()
	}
	entry.timer = time.AfterFunc(p.pendingAttachTTL(), func() {
		p.expirePendingAttachments(sessionKey)
	})
	slog.Debug(p.tag()+": buffered pending attachment",
		"session", sessionKey, "path", att.Path, "pending_count", len(entry.attachments))
}

// drainPendingAttachments returns and clears buffered attachments for the
// session. Called when a text message arrives so the attachments can be
// merged into the same user turn.
func (p *Platform) drainPendingAttachments(sessionKey string) []common.PresavedAttachment {
	p.pendingMu.Lock()
	defer p.pendingMu.Unlock()
	entry, ok := p.pending[sessionKey]
	if !ok {
		return nil
	}
	delete(p.pending, sessionKey)
	if entry.timer != nil {
		entry.timer.Stop()
	}
	return entry.attachments
}

// expirePendingAttachments drops a stale buffer after the TTL fires.
func (p *Platform) expirePendingAttachments(sessionKey string) {
	p.pendingMu.Lock()
	defer p.pendingMu.Unlock()
	if entry, ok := p.pending[sessionKey]; ok {
		if entry.timer != nil {
			entry.timer.Stop()
		}
		delete(p.pending, sessionKey)
		slog.Debug(p.tag()+": pending attachments expired", "session", sessionKey, "count", len(entry.attachments))
	}
}

// flushPendingAttachments stops all pending timers. Called on shutdown so
// timers don't fire after the platform is gone.
func (p *Platform) flushPendingAttachments() {
	p.pendingMu.Lock()
	defer p.pendingMu.Unlock()
	for _, entry := range p.pending {
		if entry.timer != nil {
			entry.timer.Stop()
		}
	}
	p.pending = make(map[string]*pendingAttachEntry)
}

// pendingAttachTTL returns the configured TTL, falling back to the default.
func (p *Platform) pendingAttachTTL() time.Duration {
	if p.pendingTTL > 0 {
		return p.pendingTTL
	}
	return defaultPendingAttachTTL
}

// saveAndBuffer downloads-then-saves are done by the caller; this helper takes
// already-downloaded bytes, persists them to upload/, and buffers the result.
func (p *Platform) saveAndBuffer(sessionKey string, data []byte, mimeType, fileName string) {
	route := rctx.SessionRoute(p.platformTag, sessionKey).WithInstance(p.PlatformInstance())
	policy := rctx.RoutePolicyForPlatform(p.platformTag, rctx.DefaultRoutePolicy(p.sessionScope))
	env := rctx.ResolveEnvelope(agentkit.MessageEvent{Envelope: agentkit.TurnEnvelope{Route: route}}, policy)
	ctx := rctx.ApplyEnvelopeToContext(context.Background(), env)
	entry := rctx.ActiveEntryKey(route, policy, env.Actor.UserID)
	owner, err := capsession.ResolveActiveSessionID(ctx, p.sessionStore, entry)
	if err != nil {
		slog.Warn(p.tag()+": resolve buffered attachment owner failed", "error", err)
		return
	}
	env.Conversation = string(owner)
	ctx = rctx.ApplyEnvelopeToContext(ctx, env)
	if p.sessionStore != nil {
		snapshot, resolveErr := sessctx.Resolve(ctx, p.sessionStore, p.workspace)
		if resolveErr != nil {
			slog.Warn(p.tag()+": resolve buffered attachment directory failed", "error", resolveErr)
			return
		}
		ctx = rctx.WithSessionContext(ctx, snapshot)
		if snapshot.Cwd != "" {
			if err = os.MkdirAll(snapshot.Cwd, 0755); err != nil {
				slog.Warn(p.tag()+": create buffered attachment directory failed", "error", err)
				return
			}
		}
	}
	saved, err := common.SaveInboundAttachmentsAt(ctx, p.workspace, []common.FileAttachment{{MimeType: mimeType, Data: data, FileName: fileName}})
	if err != nil || len(saved) == 0 {
		slog.Warn(p.tag()+": save buffered attachment failed", "error", err)
		return
	}
	if p.sessionStore == nil {
		saved[0].Owner.Unknown = true
	}
	p.bufferAttachment(sessionKey, saved[0])
}

// isAttachmentMsgType reports whether a Feishu message type carries only an
// attachment payload (no free-form text). These are admitted into an
// already-engaged thread without an explicit @bot mention.
func isAttachmentMsgType(msgType string) bool {
	switch msgType {
	case "image", "file", "audio", "media", "sticker":
		return true
	}
	return false
}

// markThreadSessionActive records that a thread sessionKey has been engaged
// by an @bot message, enabling attachment-only follow-ups inside the thread.
// No-op when thread isolation is disabled or sessionKey is not a thread key.
func (p *Platform) markThreadSessionActive(sessionKey string) {
	if !p.threadIsolation || !isThreadSessionKey(sessionKey) {
		return
	}
	p.activeThreadSessions.Store(sessionKey, time.Now())
}

// isActiveThreadSession reports whether the given sessionKey corresponds to a
// thread that has previously been engaged by an @bot message.
func (p *Platform) isActiveThreadSession(sessionKey string) bool {
	if !p.threadIsolation || !isThreadSessionKey(sessionKey) {
		return false
	}
	_, ok := p.activeThreadSessions.Load(sessionKey)
	return ok
}

// allowsUnmentionedGroupMessage reports whether a group message that does not
// @ the bot should still be dispatched. Only bot-engaged topic threads
// (threadIsolation) qualify: with threadNoMention every follow-up message in
// the thread passes; by default only attachment-only messages are admitted.
func (p *Platform) allowsUnmentionedGroupMessage(msgType, sessionKey string) bool {
	if !p.threadIsolation || !p.isActiveThreadSession(sessionKey) {
		return false
	}
	if p.threadNoMention {
		return true
	}
	return isAttachmentMsgType(msgType)
}
