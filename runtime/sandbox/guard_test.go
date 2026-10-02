package sandbox

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	capsandbox "github.com/lengzhao/agentkit/cap/sandbox"
)

// errRefWorkspace fails resolution for refs listed in failRefs.
type errRefWorkspace struct {
	fakeWorkspace
	failRefs map[string]bool
}

func (w errRefWorkspace) Resolve(ctx context.Context, rel string) (string, error) {
	if w.failRefs[rel] {
		return "", fmt.Errorf("resolve %s: unavailable", rel)
	}
	return w.fakeWorkspace.Resolve(ctx, rel)
}

// within must not confuse sibling prefixes (tenant-a vs tenant-ab).
func TestWithinBoundaries(t *testing.T) {
	cases := []struct {
		p, root string
		want    bool
	}{
		{"/a/b", "/a/b", true},   // exact match
		{"/a/b/c", "/a/b", true}, // child
		{"/a/bc", "/a/b", false}, // sibling prefix confusion
		{"/a", "/a/b", false},    // parent
		{"/a/b", "/a/bc", false}, // reverse prefix
		{"/etc/passwd", "/a", false},
	}
	for _, c := range cases {
		if got := within(c.p, c.root); got != c.want {
			t.Errorf("within(%q, %q) = %v, want %v", c.p, c.root, got, c.want)
		}
	}
}

// A symlink inside the tenant root pointing outside must not escape the view.
func TestCheckReadSymlinkEscape(t *testing.T) {
	s, local, _ := newTestSandbox(t)
	ctx := context.Background()

	outside := t.TempDir()
	if real, err := filepath.EvalSymlinks(outside); err == nil {
		outside = real
	}
	if err := os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(local, "escape")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}

	// Read through the symlink resolves to the outside path → denied.
	if err := s.CheckRead(ctx, filepath.Join(link, "secret.txt")); !errors.Is(err, capsandbox.ErrDenied) {
		t.Fatalf("symlink escape read must be denied, got %v", err)
	}
	// New file under a symlinked parent: the parent resolves outside → denied.
	if err := s.CheckWrite(ctx, filepath.Join(link, "new.txt")); !errors.Is(err, capsandbox.ErrDenied) {
		t.Fatalf("write under symlinked parent must be denied, got %v", err)
	}
}

// hidePaths resolution must fail closed: a failed mask means a leak, so the
// whole render errors (unlike ro/rwBinds which degrade to warn+skip).
func TestHidePathsFailClosed(t *testing.T) {
	s, local, global := newTestSandbox(t)
	s.workspace = errRefWorkspace{
		fakeWorkspace: fakeWorkspace{local: local, global: global},
		failRefs:      map[string]bool{"global:private": true},
	}
	s.hidePaths = []string{"global:private"}

	err := s.CheckRead(context.Background(), filepath.Join(local, "a.go"))
	if err == nil {
		t.Fatal("unresolvable hidePath must fail the render")
	}
	if errors.Is(err, capsandbox.ErrDenied) {
		t.Fatalf("render failure is not a deny decision, got %v", err)
	}
}

// roBinds degrade instead: a bad entry is skipped, the rest still works.
func TestRoBindsDegradeOnBadEntry(t *testing.T) {
	s, local, global := newTestSandbox(t)
	s.workspace = errRefWorkspace{
		fakeWorkspace: fakeWorkspace{local: local, global: global},
		failRefs:      map[string]bool{"global:broken": true},
	}
	s.roBinds = []string{"global:broken"}

	if err := s.CheckRead(context.Background(), filepath.Join(local, "a.go")); err != nil {
		t.Fatalf("bad roBind entry must not take the tool down: %v", err)
	}
}

func TestResolveBindsVariants(t *testing.T) {
	s, local, _ := newTestSandbox(t)
	ctx := context.Background()
	base := filepath.Dir(filepath.Dir(local))

	// Host path containing a colon must not be parsed as a workspace ref.
	colonDir := filepath.Join(base, "with:colon")
	if err := os.MkdirAll(colonDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// create=true makes a missing rw-bind target dir — but only for workspace
	// refs (host absolute paths are never created).
	missing := filepath.Join(local, "made-by-rwbind")

	out, err := s.resolveBinds(ctx, []string{colonDir, "  ", filepath.Join(base, "not-exist")}, false, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 || out[0] != colonDir {
		t.Fatalf("expected only the colon dir, got %v", out)
	}

	out, err = s.resolveBinds(ctx, []string{"local:made-by-rwbind"}, true, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 {
		t.Fatalf("create=true must keep the missing dir, got %v", out)
	}
	if info, err := os.Stat(missing); err != nil || !info.IsDir() {
		t.Fatalf("create=true must mkdir %s: %v", missing, err)
	}

	// strict=true propagates resolution failure.
	s2 := &Sandbox{workspace: errRefWorkspace{
		fakeWorkspace: fakeWorkspace{local: local, global: filepath.Join(base, "global")},
		failRefs:      map[string]bool{"global:nope": true},
	}}
	if _, err := s2.resolveBinds(ctx, []string{"global:nope"}, false, true); err == nil {
		t.Fatal("strict bind resolution must fail closed")
	}
}
