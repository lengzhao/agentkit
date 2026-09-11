package agentkit

import "testing"

func TestRedactSlashArgsForLog(t *testing.T) {
	if RedactSlashArgsForLog("") != "" {
		t.Fatal("empty should stay empty")
	}
	if RedactSlashArgsForLog("  echo hi  ") != SlashLogRedacted {
		t.Fatalf("got %q", RedactSlashArgsForLog("  echo hi  "))
	}
}

func TestRedactSlashAddNamePayload(t *testing.T) {
	got := RedactSlashAddNamePayload(`add demo {"secret":"x"}`)
	want := `add demo ` + SlashLogRedacted
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	got = RedactSlashAddNamePayload(`add -g demo {"secret":"x"}`)
	want = `add -g demo ` + SlashLogRedacted
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	if RedactSlashAddNamePayload("-u") != "-u" {
		t.Fatal("expected -u unchanged")
	}
}
