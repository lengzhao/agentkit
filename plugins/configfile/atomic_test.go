package configfile

import "testing"

func TestWriteTargetForAdd(t *testing.T) {
	t.Parallel()

	files := []string{"local:mcp.json", "global:mcp.json", ".cursor/mcp.json"}

	got, err := WriteTargetForAdd(files, false)
	if err != nil || got != "local:mcp.json" {
		t.Fatalf("local = %q err=%v", got, err)
	}

	got, err = WriteTargetForAdd(files, true)
	if err != nil || got != "global:mcp.json" {
		t.Fatalf("global = %q err=%v", got, err)
	}

	_, err = WriteTargetForAdd([]string{"local:mcp.json"}, true)
	if err == nil {
		t.Fatal("expected error when no global file configured")
	}
}

func TestPeelGlobalFlag(t *testing.T) {
	t.Parallel()

	global, rest := PeelGlobalFlag([]string{"-g", "add", "name"})
	if !global || len(rest) != 2 || rest[0] != "add" {
		t.Fatalf("global=%v rest=%v", global, rest)
	}
}
