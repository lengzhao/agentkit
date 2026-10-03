package sandbox

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	capsandbox "github.com/lengzhao/agentkit/cap/sandbox"
	"github.com/lengzhao/agentkit/testing/agenttest"
)

// errRefWorkspace fails resolution for refs listed in failRefs.
type errRefWorkspace struct {
	agenttest.ScopedWorkspace
	failRefs map[string]bool
}

func (w errRefWorkspace) Resolve(ctx context.Context, rel string) (string, error) {
	if w.failRefs[rel] {
		return "", fmt.Errorf("resolve %s: unavailable", rel)
	}
	return w.ScopedWorkspace.Resolve(ctx, rel)
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
		ScopedWorkspace: agenttest.ScopedWorkspace{Local: local, Global: global},
		failRefs:        map[string]bool{"global:private": true},
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
		ScopedWorkspace: agenttest.ScopedWorkspace{Local: local, Global: global},
		failRefs:        map[string]bool{"global:broken": true},
	}
	s.roBinds = []string{"global:broken"}

	if err := s.CheckRead(context.Background(), filepath.Join(local, "a.go")); err != nil {
		t.Fatalf("bad roBind entry must not take the tool down: %v", err)
	}
}

// In-process checks must agree with the bwrap maps: a ro map dst is readable
// but not writable even inside the writable tenant root; a rw map dst outside
// the tenant root is writable.
func TestCheckReadWriteMaps(t *testing.T) {
	s, local, global := newTestSandbox(t)
	ctx := context.Background()
	base := t.TempDir()
	if real, err := filepath.EvalSymlinks(base); err == nil {
		base = real
	}
	sshDir := filepath.Join(base, ".ssh")
	shared := filepath.Join(base, "shared")
	for _, d := range []string{sshDir, shared} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	s.maps = []MapConfig{
		{Src: sshDir, Dst: ".ssh"},                                      // ro, under HOME (tenant root)
		{Src: shared, Dst: filepath.Join(global, "shared"), Mode: "rw"}, // rw, under ro global root
	}

	roDst := filepath.Join(local, ".ssh", "id_rsa")
	if err := s.CheckRead(ctx, roDst); err != nil {
		t.Fatalf("ro map dst must be readable: %v", err)
	}
	if err := s.CheckWrite(ctx, roDst); !errors.Is(err, capsandbox.ErrDenied) {
		t.Fatalf("ro map dst must stay read-only even under the tenant root, got %v", err)
	}

	rwDst := filepath.Join(global, "shared", "f.txt")
	if err := s.CheckRead(ctx, rwDst); err != nil {
		t.Fatalf("rw map dst must be readable: %v", err)
	}
	if err := s.CheckWrite(ctx, rwDst); err != nil {
		t.Fatalf("rw map dst must be writable even under the ro global root: %v", err)
	}

	// The map src itself stays outside the view.
	if err := s.CheckRead(ctx, filepath.Join(sshDir, "id_rsa")); !errors.Is(err, capsandbox.ErrDenied) {
		t.Fatalf("map src must not become visible, got %v", err)
	}
}

// TranslatePath rewrites in-view map dsts to their host srcs (longest prefix
// wins) so in-process consumers see the same content as the subprocess view.
func TestTranslatePath(t *testing.T) {
	s, local, _ := newTestSandbox(t)
	ctx := context.Background()
	base := t.TempDir()
	if real, err := filepath.EvalSymlinks(base); err == nil {
		base = real
	}
	sshDir := filepath.Join(base, ".ssh")
	nested := filepath.Join(base, "nested")
	for _, d := range []string{sshDir, nested} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	s.maps = []MapConfig{
		{Src: sshDir, Dst: ".ssh"},
		{Src: nested, Dst: ".ssh/nested"}, // longer dst prefix must win
	}

	cases := []struct {
		in     string
		want   string
		wantOK bool
	}{
		{filepath.Join(local, ".ssh"), sshDir, true},
		{filepath.Join(local, ".ssh", "id_rsa"), filepath.Join(sshDir, "id_rsa"), true},
		{filepath.Join(local, ".ssh", "nested", "f"), filepath.Join(nested, "f"), true},
		{filepath.Join(local, "work", "a.go"), "", false},
	}
	for _, c := range cases {
		got, ok := s.TranslatePath(ctx, c.in)
		if ok != c.wantOK || got != c.want {
			t.Errorf("TranslatePath(%q) = %q,%v; want %q,%v", c.in, got, ok, c.want, c.wantOK)
		}
	}

	// Disabled sandbox never translates.
	off := &Sandbox{on: false}
	if _, ok := off.TranslatePath(ctx, filepath.Join(local, ".ssh")); ok {
		t.Fatal("disabled sandbox must not translate")
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
		ScopedWorkspace: agenttest.ScopedWorkspace{
			Local:  local,
			Global: filepath.Join(base, "global"),
		},
		failRefs: map[string]bool{"global:nope": true},
	}}
	if _, err := s2.resolveBinds(ctx, []string{"global:nope"}, false, true); err == nil {
		t.Fatal("strict bind resolution must fail closed")
	}
}
