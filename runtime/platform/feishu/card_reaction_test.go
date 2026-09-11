package feishu

import (
	"testing"

	"github.com/lengzhao/agentkit"
)

func TestCardProcessingReactionEmojiAlternates(t *testing.T) {
	if cardProcessingReactionEmoji(0) != "Typing" {
		t.Fatalf("pulse 0 = %q", cardProcessingReactionEmoji(0))
	}
	if cardProcessingReactionEmoji(1) != "OneSecond" {
		t.Fatalf("pulse 1 = %q", cardProcessingReactionEmoji(1))
	}
	if cardProcessingReactionEmoji(2) != "Typing" {
		t.Fatalf("pulse 2 should wrap to Typing, got %q", cardProcessingReactionEmoji(2))
	}
}

func TestAttachCardProcessingReactionSkipsNonUnifiedCard(t *testing.T) {
	p := &Platform{progressStyle: "card", useInteractiveCard: false}
	sessionID := agentkit.SessionID("session-non-unified")
	st := p.streamState(sessionID)
	st.cardHandle = &feishuPreviewHandle{messageID: "om_card"}
	p.attachCardProcessingReaction(sessionID)
	st.mu.Lock()
	id := st.cardReactionID
	st.mu.Unlock()
	if id != "" {
		t.Fatalf("expected no card reaction in non-unified mode, got %q", id)
	}
}
