package cli

import (
	"strings"
	"testing"

	"github.com/lengzhao/pluginkit"
)

func TestPluginDoc(t *testing.T) {
	t.Parallel()
	out, err := pluginDoc("tool/grep")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"go doc github.com/lengzhao/agentkit/plugins/tool.NewGrep",
		"tool/grep",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
}

func TestDocSymbol(t *testing.T) {
	t.Parallel()
	spec, ok := pluginkit.Lookup("shell/bash")
	if !ok {
		t.Fatal("shell/bash not registered")
	}
	symbol := docSymbol(spec)
	if want := "github.com/lengzhao/agentkit/plugins/shell.New"; symbol != want {
		t.Fatalf("got %q, want %q", symbol, want)
	}
}
