package helpdoc_test

import (
	"strings"
	"testing"

	_ "github.com/lengzhao/agentkit/plugins"
	"github.com/lengzhao/pluginkit"
	"github.com/lengzhao/agentkit/runtime/helpdoc"
)

func TestKindDoc(t *testing.T) {
	t.Parallel()
	out, err := helpdoc.KindDoc("", "tool/fs-workspace")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"go doc github.com/lengzhao/agentkit/plugins/tool/fs.NewFSWorkspace",
		"tool/fs-workspace",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
}

func TestDocSymbol(t *testing.T) {
	t.Parallel()
	spec, ok := pluginkit.Lookup("tool/shell-bash")
	if !ok {
		t.Fatal("tool/shell-bash not registered")
	}
	// docSymbol is unexported; exercise it through KindDoc output prefix instead.
	out, err := helpdoc.KindDoc("", "tool/shell-bash")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "github.com/lengzhao/agentkit/plugins/tool/shell.NewShellBash") {
		t.Fatalf("unexpected doc output:\n%s", out)
	}
	_ = spec
}
