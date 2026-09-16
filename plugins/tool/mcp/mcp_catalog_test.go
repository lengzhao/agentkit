package mcp

import (
	"strings"
	"testing"
)

func TestFormatMCPToolCatalog(t *testing.T) {
	out := formatMCPToolCatalog([]toolDefinition{
		{Server: "openviking", ExposedName: "openviking__search"},
		{Server: "openviking", ExposedName: "openviking__read"},
	})
	if out == "" {
		t.Fatal("expected catalog")
	}
	for _, want := range []string{"openviking (2):", "openviking__search", "openviking__read"} {
		if !strings.Contains(out, want) {
			t.Fatalf("catalog missing %q:\n%s", want, out)
		}
	}
}
