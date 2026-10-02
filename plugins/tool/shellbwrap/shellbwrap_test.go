package shellbwrap

import (
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
