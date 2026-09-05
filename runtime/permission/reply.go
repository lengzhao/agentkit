package permission

import (
	"encoding/json"
	"fmt"

	capspermission "github.com/lengzhao/agentkit/cap/permission"
)

func MarshalReply(reply capspermission.Reply) json.RawMessage {
	return json.RawMessage(mustMarshal(reply))
}

func DecodeReply(raw json.RawMessage) (capspermission.Reply, error) {
	if len(raw) == 0 {
		return capspermission.Reply{}, fmt.Errorf("permission reply is empty")
	}
	var reply capspermission.Reply
	if err := json.Unmarshal(raw, &reply); err != nil {
		return capspermission.Reply{}, err
	}
	if reply.RequestID == "" {
		return capspermission.Reply{}, fmt.Errorf("permission reply missing requestId")
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
