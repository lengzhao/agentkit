package agentkit

import "testing"

func TestNormalizeModalitiesDefault(t *testing.T) {
	got := NormalizeModalities(nil)
	if len(got) != 2 || got[0] != ModalityText || got[1] != ModalityImage {
		t.Fatalf("default = %#v", got)
	}
}

func TestNormalizeModalitiesAliases(t *testing.T) {
	got := NormalizeModalities([]string{" TEXT ", "vision", "voice"})
	if len(got) != 3 || got[0] != ModalityText || got[1] != ModalityImage || got[2] != ModalityAudio {
		t.Fatalf("got = %#v", got)
	}
}

func TestSupportsModality(t *testing.T) {
	mods := []string{ModalityText}
	if SupportsModality(mods, ModalityImage) {
		t.Fatal("text-only should not support image")
	}
	if !SupportsModality(mods, ModalityText) {
		t.Fatal("expected text")
	}
}
