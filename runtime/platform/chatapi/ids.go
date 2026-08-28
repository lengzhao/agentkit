package chatapi

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"regexp"
)

const conversationIDPrefix = "conv_"

var conversationIDPattern = regexp.MustCompile(`^conv_[A-Za-z0-9_-]{22}$`)

func newConversationID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("chat-api: generate conversation id: %w", err)
	}
	return conversationIDPrefix + base64.RawURLEncoding.EncodeToString(b[:]), nil
}

func isOpaqueConversationID(id string) bool {
	return conversationIDPattern.MatchString(id)
}

func newRunID() string {
	var b [12]byte
	_, _ = rand.Read(b[:])
	return "run_" + base64.RawURLEncoding.EncodeToString(b[:])
}

func newInteractionID() string {
	var b [12]byte
	_, _ = rand.Read(b[:])
	return "ix_" + base64.RawURLEncoding.EncodeToString(b[:])
}

func messageID(conversationID string, turnIndex int) string {
	return fmt.Sprintf("%s:%d", conversationID, turnIndex)
}
