package filesystem_test

import (
	"testing"

	rtfs "github.com/lengzhao/agentkit/runtime/filesystem"
)

func TestTrimRedundantFSRootPrefix(t *testing.T) {
	t.Parallel()
	if got := rtfs.TrimRedundantFSRootPrefix("work", "work/upload/a.png"); got != "upload/a.png" {
		t.Fatalf("work/upload = %q", got)
	}
	if got := rtfs.TrimRedundantFSRootPrefix("work", "upload/a.png"); got != "upload/a.png" {
		t.Fatalf("upload = %q", got)
	}
	if got := rtfs.TrimRedundantFSRootPrefix(".", "work/upload/a.png"); got != "work/upload/a.png" {
		t.Fatalf("root . = %q", got)
	}
	if got := rtfs.TrimRedundantFSRootPrefix("work", "work/work/upload/a.png"); got != "upload/a.png" {
		t.Fatalf("double work/ = %q", got)
	}
}
