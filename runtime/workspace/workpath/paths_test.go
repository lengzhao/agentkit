package workpath_test

import (
	"testing"

	"github.com/lengzhao/agentkit/runtime/workspace/workpath"
)

func TestCanonicalWorkPath(t *testing.T) {
	t.Parallel()

	workDir := "work"
	cases := map[string]string{
		"upload/a.jpg":           "work/upload/a.jpg",
		"work/upload/a.jpg":      "work/upload/a.jpg",
		"work/work/upload/a.jpg": "work/upload/a.jpg",
		"/work/upload/a.jpg":     "work/upload/a.jpg",
		"local:skills/foo.md":    "local:skills/foo.md",
	}
	for in, want := range cases {
		if got := workpath.CanonicalWorkPath(workDir, in); got != want {
			t.Fatalf("%q => %q, want %q", in, got, want)
		}
	}
}

func TestLocalPath(t *testing.T) {
	t.Parallel()
	if got := workpath.LocalPath("work/upload/a.jpg"); got != "local:work/upload/a.jpg" {
		t.Fatalf("got %q", got)
	}
}
