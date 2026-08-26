package permission

import (
	"encoding/json"
	"testing"
)

func TestDecodeReply(t *testing.T) {
	t.Parallel()

	raw := MarshalReply(Reply{RequestID: "perm1", Text: "yes"})
	got, err := DecodeReply(raw)
	if err != nil {
		t.Fatal(err)
	}
	if got.RequestID != "perm1" || got.Text != "yes" {
		t.Fatalf("reply = %+v", got)
	}

	if _, err := DecodeReply(nil); err == nil {
		t.Fatal("expected empty reply to fail")
	}
	if _, err := DecodeReply(json.RawMessage(`{"text":"yes"}`)); err == nil {
		t.Fatal("expected missing requestId to fail")
	}
}
