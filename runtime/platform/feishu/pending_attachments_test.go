package feishu

import (
	"testing"
	"time"

	"github.com/lengzhao/agentkit/runtime/platform/common"
)

func TestBufferAndDrainPendingAttachments(t *testing.T) {
	p := &Platform{}
	const key = "feishu:oc_chat:ou_user"

	// Buffer two attachments.
	p.bufferAttachment(key, common.PresavedAttachment{Path: "upload/a.pdf", Mime: "application/pdf", Size: 100, OrigName: "a.pdf"})
	p.bufferAttachment(key, common.PresavedAttachment{Path: "upload/b.png", Mime: "image/png", Size: 200, OrigName: "b.png", Image: true})

	atts := p.drainPendingAttachments(key)
	if len(atts) != 2 {
		t.Fatalf("drained = %d attachments, want 2", len(atts))
	}
	if atts[0].Path != "upload/a.pdf" || atts[1].Path != "upload/b.png" {
		t.Fatalf("paths = %q, %q", atts[0].Path, atts[1].Path)
	}
	if !atts[1].Image {
		t.Fatal("second attachment should be image")
	}

	// Drain again returns nil (entry removed).
	if got := p.drainPendingAttachments(key); got != nil {
		t.Fatalf("second drain = %v, want nil", got)
	}
}

func TestDrainPendingAttachmentsEmpty(t *testing.T) {
	p := &Platform{}
	if got := p.drainPendingAttachments("nope"); got != nil {
		t.Fatalf("drain on missing key = %v, want nil", got)
	}
}

func TestExpirePendingAttachments(t *testing.T) {
	p := &Platform{pendingTTL: 50 * time.Millisecond}
	const key = "feishu:oc_chat:ou_user"

	p.bufferAttachment(key, common.PresavedAttachment{Path: "upload/a.pdf"})
	// Wait for the TTL timer to fire.
	time.Sleep(150 * time.Millisecond)

	if got := p.drainPendingAttachments(key); got != nil {
		t.Fatalf("drain after expiry = %v, want nil", got)
	}
}

func TestFlushPendingAttachments(t *testing.T) {
	p := &Platform{pendingTTL: time.Hour}
	p.bufferAttachment("k1", common.PresavedAttachment{Path: "upload/a"})
	p.bufferAttachment("k2", common.PresavedAttachment{Path: "upload/b"})

	p.flushPendingAttachments()

	if got := p.drainPendingAttachments("k1"); got != nil {
		t.Fatalf("drain k1 after flush = %v, want nil", got)
	}
	if got := p.drainPendingAttachments("k2"); got != nil {
		t.Fatalf("drain k2 after flush = %v, want nil", got)
	}
}

func TestBufferAttachmentResetsTimer(t *testing.T) {
	p := &Platform{pendingTTL: 80 * time.Millisecond}
	const key = "feishu:oc_chat:ou_user"

	p.bufferAttachment(key, common.PresavedAttachment{Path: "upload/a"})
	time.Sleep(50 * time.Millisecond)
	// Second buffer should reset the timer so the first one doesn't expire.
	p.bufferAttachment(key, common.PresavedAttachment{Path: "upload/b"})
	time.Sleep(50 * time.Millisecond)

	atts := p.drainPendingAttachments(key)
	if len(atts) != 2 {
		t.Fatalf("drained = %d, want 2 (timer should have been reset)", len(atts))
	}
}

func TestPendingAttachTTLDefault(t *testing.T) {
	p := &Platform{}
	if got := p.pendingAttachTTL(); got != defaultPendingAttachTTL {
		t.Fatalf("default TTL = %v, want %v", got, defaultPendingAttachTTL)
	}
	p.pendingTTL = 42 * time.Second
	if got := p.pendingAttachTTL(); got != 42*time.Second {
		t.Fatalf("configured TTL = %v, want 42s", got)
	}
}

func TestIsAttachmentMsgType(t *testing.T) {
	for _, mt := range []string{"image", "file", "audio", "media", "sticker"} {
		if !isAttachmentMsgType(mt) {
			t.Errorf("isAttachmentMsgType(%q) = false, want true", mt)
		}
	}
	for _, mt := range []string{"text", "post", "merge_forward", ""} {
		if isAttachmentMsgType(mt) {
			t.Errorf("isAttachmentMsgType(%q) = true, want false", mt)
		}
	}
}

func TestMarkAndIsActiveThreadSession(t *testing.T) {
	const threadKey = "feishu:oc_chat:t:om_root:u:ou_user"
	const directKey = "feishu:oc_chat:u:ou_user"

	t.Run("thread isolation disabled is no-op", func(t *testing.T) {
		p := &Platform{threadIsolation: false}
		p.markThreadSessionActive(threadKey)
		if p.isActiveThreadSession(threadKey) {
			t.Fatal("expected no-op when thread_isolation is off")
		}
	})

	t.Run("non-thread sessionKey is ignored", func(t *testing.T) {
		p := &Platform{threadIsolation: true}
		p.markThreadSessionActive(directKey)
		if p.isActiveThreadSession(directKey) {
			t.Fatal("expected non-thread sessionKey to be ignored")
		}
	})

	t.Run("thread sessionKey is recorded", func(t *testing.T) {
		p := &Platform{threadIsolation: true}
		if p.isActiveThreadSession(threadKey) {
			t.Fatal("thread should not be active before mark")
		}
		p.markThreadSessionActive(threadKey)
		if !p.isActiveThreadSession(threadKey) {
			t.Fatal("thread should be active after mark")
		}
	})
}
