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

func TestPeelGlobalFlag(t *testing.T) {
	t.Parallel()

	global, rest := PeelGlobalFlag([]string{"-g", "add", "name"})
	if !global || len(rest) != 2 || rest[0] != "add" {
		t.Fatalf("global=%v rest=%v", global, rest)
	}
	global, rest = PeelGlobalFlag([]string{"--global", "use", "coding"})
	if !global || len(rest) != 2 || rest[0] != "use" {
		t.Fatalf("--global: global=%v rest=%v", global, rest)
	}
	global, rest = PeelGlobalFlag([]string{"use", "coding"})
	if global || len(rest) != 2 {
		t.Fatalf("no flag: global=%v rest=%v", global, rest)
	}
}
