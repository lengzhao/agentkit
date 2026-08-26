package permission

import (
	"encoding/json"
	"fmt"
)

// Reply is the inbound answer to a permission/request.
//
// Field usage by request Kind:
//   - KindAllowDeny: Decision ("allow"/"deny", or y/n/yes/no); Text is a CLI fallback
//     when Decision is empty. UpdatedInput carries human-edited tool arguments.
//     Cancelled means the user explicitly declined.
//   - KindQuestion: Text and/or Selected only; do not set Decision.
type Reply struct {
	RequestID string `json:"requestId"`
	UserID    string `json:"userId,omitempty"`
	// Decision is for KindAllowDeny only ("allow"/"deny", or y/n/yes/no).
	Decision     string         `json:"decision,omitempty"`
	Selected     []int          `json:"selected,omitempty"`
	Text         string         `json:"text,omitempty"`
	UpdatedInput map[string]any `json:"updatedInput,omitempty"`
	Cancelled    bool           `json:"cancelled,omitempty"`
}

func MarshalReply(reply Reply) json.RawMessage {
	return json.RawMessage(mustMarshal(reply))
}

func DecodeReply(raw json.RawMessage) (Reply, error) {
	if len(raw) == 0 {
		return Reply{}, fmt.Errorf("permission reply is empty")
	}
	var reply Reply
	if err := json.Unmarshal(raw, &reply); err != nil {
		return Reply{}, err
	}
	if reply.RequestID == "" {
		return Reply{}, fmt.Errorf("permission reply missing requestId")
	}
	return reply, nil
}

func mustMarshal(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}
