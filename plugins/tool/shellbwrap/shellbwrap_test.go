package shellbwrap

import (
	"context"
	"strings"
	"testing"

	capsandbox "github.com/lengzhao/agentkit/cap/sandbox"
	"github.com/lengzhao/agentkit/testing/agenttest"
)

func TestTruncBuffer(t *testing.T) {
	buf := &truncBuffer{limit: 5}
	if n, _ := buf.Write([]byte("hello world")); n != 11 {
		t.Fatalf("Write must report full input length, got %d", n)
	}
	if buf.String() != "hello" || !buf.truncated {
		t.Fatalf("expected truncated %q, got %q (truncated=%v)", "hello", buf.String(), buf.truncated)
	}
}

func TestNewRequiresSandbox(t *testing.T) {
	_, err := New(Config{}, Deps{})
	if err == nil {
		t.Fatal("expected error when sandbox dep missing")
	}
}

func TestNewWithSandbox(t *testing.T) {
	tool, err := New(Config{}, Deps{Workspace: agenttest.ScopedWorkspace{}, Sandbox: capsandbox.Disabled()})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := tool.(*bundle); !ok {
		t.Fatalf("expected *bundle, got %T", tool)
	}
}

// structuredShell mirrors tool/workflow's StructuredShell capability (plugins
// must not import each other; Go interfaces are satisfied structurally).
type structuredShell interface {
	RunStructured(ctx context.Context, command string, timeoutSeconds *float64) (stdout, stderr string, exitCode int, err error)
}

func TestBundleImplementsRunStructured(t *testing.T) {
	tool, err := New(Config{WorkDir: "."}, Deps{Workspace: agenttest.ScopedWorkspace{}, Sandbox: capsandbox.Disabled()})
	if err != nil {
		t.Fatal(err)
	}
	ss, ok := tool.(structuredShell)
	if !ok {
		t.Fatal("tool/shell-bwrap must implement RunStructured so workflow script steps stay sandboxed")
	}
	stdout, stderr, code, err := ss.RunStructured(context.Background(), "echo out && echo err >&2 && exit 3", nil)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(stdout) != "out" {
		t.Fatalf("stdout = %q", stdout)
	}
	if strings.TrimSpace(stderr) != "err" {
		t.Fatalf("stderr = %q", stderr)
	}
	if code != 3 {
		t.Fatalf("exitCode = %d", code)
	}
}
