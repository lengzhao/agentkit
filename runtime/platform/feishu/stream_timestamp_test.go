package feishu

import (
	"fmt"
	"testing"
	"time"
)

func TestFormatStreamHeartbeatTimestamp(t *testing.T) {
	tm := time.Date(2026, 9, 11, 15, 4, 5, 0, time.UTC)
	got := formatStreamHeartbeatTimestamp(tm)
	if got != "2026-09-11 15:04:05" {
		t.Fatalf("timestamp = %q", got)
	}
}

func TestIsCardStreamingClosedError(t *testing.T) {
	err := fmt.Errorf("feishu: stream card content code=300309 msg=ErrMsg: streaming mode is closed")
	if !isCardStreamingClosedError(err) {
		t.Fatal("expected streaming closed detection")
	}
}
