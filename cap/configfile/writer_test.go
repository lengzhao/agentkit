package configfile

import "testing"

func TestPeelGlobalFlag(t *testing.T) {
	t.Parallel()

	global, rest := PeelGlobalFlag([]string{"-g", "add", "name"})
	if !global || len(rest) != 2 || rest[0] != "add" {
		t.Fatalf("global=%v rest=%v", global, rest)
	}
}
