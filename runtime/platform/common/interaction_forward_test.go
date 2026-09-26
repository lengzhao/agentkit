package common

import (
	"strings"
	"testing"
)

func TestFormatCardActionInbound(t *testing.T) {
	got := FormatCardActionInbound("hello", true, "action=go", "U1")
	if !strings.Contains(got, "[card_action]") || !strings.Contains(got, "hello") || !strings.Contains(got, "U1") {
		t.Fatalf("%q", got)
	}
}

func TestNormalizeUnknownInteraction(t *testing.T) {
	if NormalizeUnknownInteraction("") != UnknownInteractionForward {
		t.Fatal("default forward")
	}
	if NormalizeUnknownInteraction("ignore") != UnknownInteractionIgnore {
		t.Fatal("ignore")
	}
}
