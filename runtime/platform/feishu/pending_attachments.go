package feishu

import (
	"log/slog"
	"time"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/runtime/platform/common"
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
	deliveryID := agentkit.SessionID(sessionKey)
	saved := common.SaveInboundAttachments(deliveryID, []common.FileAttachment{{
		MimeType: mimeType,
		Data:     data,
		FileName: fileName,
	}}, common.InboundOptsFor(p.workspace))
	if len(saved) == 0 {
		slog.Warn(p.tag()+": saveAndBuffer produced no saved attachment", "session", sessionKey, "name", fileName)
		return
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
