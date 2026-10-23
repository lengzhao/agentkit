package feishu

import "testing"

func TestCardActionAckToast(t *testing.T) {
	ack := cardActionAckToast()
	if ack == nil || ack.Toast == nil || ack.Toast.Content == "" {
		t.Fatal("missing callback acknowledgement")
	}
}
