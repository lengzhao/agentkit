package sandbox

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/lengzhao/agentkit/testing/agenttest"
)

func newTestSandbox(t *testing.T) (*Sandbox, string, string) {
	t.Helper()
	ws, local, global := agenttest.SetupScopedTenantDirs(t)
	s := &Sandbox{
		workspace: ws,
		on:        true,
		roMaskDir: t.TempDir(),
		tmpBase:   "",
	}
	return s, local, global
}

func TestWrapBwrapMasksNeighborTenants(t *testing.T) {
	s, local, global := newTestSandbox(t)
	workDir := filepath.Join(local, "work")
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		t.Fatal(err)
	}
	argv, err := s.WrapArgv(context.Background(), workDir, []string{"bash", "-lc", "ls"})
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(argv, "\x00")

	// tmpfs masks the tenant parent, then the tenant root is bound back
	tenantsBase := filepath.Dir(local)
	i := mountIndex(argv, "--tmpfs", tenantsBase)
	if i < 0 {
		t.Fatalf("expected --tmpfs %s, argv=%v", tenantsBase, argv)
	}
	i = slices.Index(argv, "--bind")
	if i < 0 || argv[i+1] != local || argv[i+2] != local {
		t.Fatalf("expected --bind %s %s, argv=%v", local, local, argv)
	}
	// global root read-only + secrets mask
	if !strings.Contains(joined, "--ro-bind\x00"+global) {
		t.Fatalf("expected ro-bind of global root, argv=%v", argv)
	}
	if !strings.Contains(joined, "/dev/null\x00"+filepath.Join(global, "secrets.enc.json")) {
		t.Fatalf("expected secrets.enc.json masked, argv=%v", argv)
	}
	// Network stack preserved: no unshare-net; pid namespace must be isolated
	// (prevent killing host processes)
	for _, a := range argv {
		if a == "--unshare-net" {
			t.Fatalf("must keep pod network stack, found %s", a)
		}
	}
	if !slices.Contains(argv, "--unshare-pid") {
		t.Fatalf("expected --unshare-pid, argv=%v", argv)
	}
	// HOME points at the tenant root; the inner command follows --
	i = slices.Index(argv, "--setenv")
	if i < 0 || argv[i+1] != "HOME" || argv[i+2] != local {
		t.Fatalf("expected --setenv HOME <tenantRoot>, argv=%v", argv)
	}
	sep := slices.Index(argv, "--")
	if sep < 0 || argv[sep+1] != "bash" {
		t.Fatalf("expected -- bash, argv=%v", argv)
	}
	// Read-only remount: the root overlay and mask layer are remounted ro
	// before the command runs, so writes outside explicit rw binds fail with
	// EROFS instead of vanishing into the overlay.
	if !strings.Contains(joined, roMountsEnv+"\x00"+tenantsBase) {
		t.Fatalf("expected --setenv %s <tenantsBase>, argv=%v", roMountsEnv, argv)
	}
	if sep+3 >= len(argv) || argv[sep+2] != "-c" || !strings.Contains(argv[sep+3], "remount,ro") {
		t.Fatalf("expected ro remount script after --, argv=%v", argv)
	}
	// /tmp must be its own tmpfs mount (still writable after the ro remount)
	if mountIndex(argv, "--tmpfs", "/tmp") < 0 {
		t.Fatalf("expected --tmpfs /tmp, argv=%v", argv)
	}
}

// homeRef selects the in-sandbox HOME; default stays the tenant root.
func TestWrapBwrapHomeRef(t *testing.T) {
	s, local, global := newTestSandbox(t)
	workDir := filepath.Join(local, "work")
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		t.Fatal(err)
	}
	homeOf := func(argv []string) string {
		i := slices.Index(argv, "HOME")
		if i < 0 || i+1 >= len(argv) {
			t.Fatalf("no --setenv HOME, argv=%v", argv)
		}
		return argv[i+1]
	}

	// Default: HOME = tenant root.
	argv, err := s.WrapArgv(context.Background(), workDir, []string{"bash", "-lc", "ls"})
	if err != nil {
		t.Fatal(err)
	}
	if got := homeOf(argv); got != local {
		t.Fatalf("default HOME must be the tenant root %s, got %s", local, got)
	}

	// homeRef = global:. → HOME = global root (read-only in the view).
	s2, _, global2 := newTestSandbox(t)
	s2.homeRef = "global:."
	argv, err = s2.WrapArgv(context.Background(), workDir, []string{"bash", "-lc", "ls"})
	if err != nil {
		t.Fatal(err)
	}
	if got := homeOf(argv); got != global2 {
		t.Fatalf("homeRef=global:. must set HOME to %s, got %s", global2, got)
	}
	_ = global
}

// A homeRef resolving outside the view fails closed instead of pointing HOME
// at an unmounted path.
func TestWrapBwrapHomeRefOutsideViewFails(t *testing.T) {
	s, local, _ := newTestSandbox(t)
	s.homeRef = t.TempDir() // host absolute path outside tenant/global/binds
	workDir := filepath.Join(local, "work")
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := s.WrapArgv(context.Background(), workDir, []string{"bash", "-lc", "ls"}); err == nil {
		t.Fatal("homeRef outside the view must fail the render")
	}
}

// maps bind host sources onto in-sandbox destinations relative to HOME.
func TestWrapBwrapMaps(t *testing.T) {
	s, local, _ := newTestSandbox(t)
	base := t.TempDir()
	if real, err := filepath.EvalSymlinks(base); err == nil {
		base = real
	}
	sshDir := filepath.Join(base, ".ssh")
	gitconfig := filepath.Join(base, ".gitconfig")
	shared := filepath.Join(base, "shared")
	if err := os.MkdirAll(sshDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(shared, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(gitconfig, []byte("[user]"), 0o600); err != nil {
		t.Fatal(err)
	}
	s.maps = []MapConfig{
		{Src: sshDir, Dst: ".ssh"},                         // ro, relative to HOME
		{Src: gitconfig, Dst: ".gitconfig"},                // ro file map
		{Src: shared, Dst: "shared", Mode: "rw"},           // rw map
		{Src: filepath.Join(base, "missing"), Dst: "nope"}, // skipped
	}
	workDir := filepath.Join(local, "work")
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		t.Fatal(err)
	}
	argv, err := s.WrapArgv(context.Background(), workDir, []string{"bash", "-lc", "ls"})
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(argv, "\x00")
	if !strings.Contains(joined, "--ro-bind\x00"+sshDir+"\x00"+filepath.Join(local, ".ssh")) {
		t.Fatalf("expected ro map .ssh under HOME, argv=%v", argv)
	}
	if !strings.Contains(joined, "--ro-bind\x00"+gitconfig+"\x00"+filepath.Join(local, ".gitconfig")) {
		t.Fatalf("expected ro map .gitconfig under HOME, argv=%v", argv)
	}
	if !strings.Contains(joined, "--bind\x00"+shared+"\x00"+filepath.Join(local, "shared")) {
		t.Fatalf("expected rw map shared under HOME, argv=%v", argv)
	}
	if strings.Contains(joined, "missing") {
		t.Fatalf("unresolvable map src must be skipped, argv=%v", argv)
	}
}

// A map src inside a hidePath would re-expose a masked path under a new
// location: fail closed.
func TestMapSrcInsideHidePathFails(t *testing.T) {
	s, local, global := newTestSandbox(t)
	private := filepath.Join(global, "private")
	if err := os.MkdirAll(private, 0o755); err != nil {
		t.Fatal(err)
	}
	s.hidePaths = []string{"global:private"}
	s.maps = []MapConfig{{Src: private, Dst: ".private"}}
	workDir := filepath.Join(local, "work")
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := s.WrapArgv(context.Background(), workDir, []string{"bash", "-lc", "ls"}); err == nil {
		t.Fatal("map src inside a hidePath must fail the render")
	}
}

// A relative map dst must not escape HOME.
func TestMapDstEscapeFails(t *testing.T) {
	s, local, _ := newTestSandbox(t)
	src := filepath.Join(t.TempDir(), "src")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	s.maps = []MapConfig{{Src: src, Dst: "../escape"}}
	workDir := filepath.Join(local, "work")
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := s.WrapArgv(context.Background(), workDir, []string{"bash", "-lc", "ls"}); err == nil {
		t.Fatal("map dst escaping HOME must fail the render")
	}
}

func TestWrapBwrapGlobalAncestorOfTenants(t *testing.T) {
	// When the global root is an ancestor of the tenants parent (e.g.
	// global=~/.agentkit, tenants=~/.agentkit/tenants), the global ro-bind
	// must come before the tenants tmpfs mask, otherwise the global bind
	// would re-expose neighbor tenants.
	base := t.TempDir()
	if real, err := filepath.EvalSymlinks(base); err == nil {
		base = real
	}
	global := filepath.Join(base, ".agentkit")
	local := filepath.Join(global, "tenants", "tenant-a")
	if err := os.MkdirAll(local, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(global, 0o755); err != nil {
		t.Fatal(err)
	}
	s := &Sandbox{workspace: agenttest.ScopedWorkspace{Local: local, Global: global}, on: true}
	workDir := filepath.Join(local, "work")
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		t.Fatal(err)
	}
	argv, err := s.WrapArgv(context.Background(), workDir, []string{"bash", "-lc", "ls"})
	if err != nil {
		t.Fatal(err)
	}
	tenantsBase := filepath.Join(global, "tenants")
	tmpIdx := mountIndex(argv, "--tmpfs", tenantsBase)
	bindIdx := mountIndex(argv, "--bind", local)
	if tmpIdx < 0 || bindIdx < 0 {
		t.Fatalf("missing mounts, argv=%v", argv)
	}
	// Find the ro-bind of the global root (the first one pointing at global)
	var globalRoIdx = -1
	for i, a := range argv {
		if a == "--ro-bind" && i+2 < len(argv) && argv[i+1] == global {
			globalRoIdx = i
		}
	}
	if globalRoIdx < 0 {
		t.Fatalf("global root not ro-bound, argv=%v", argv)
	}
	if !(globalRoIdx < tmpIdx && tmpIdx < bindIdx) {
		t.Fatalf("mount order must be ro-bind global → tmpfs tenants → bind tenant, argv=%v", argv)
	}
}

// mountIndex returns the arg index of flag immediately followed by target
// (the same flag may appear multiple times, e.g. --tmpfs).
func mountIndex(argv []string, flag, target string) int {
	for i := 0; i+1 < len(argv); i++ {
		if argv[i] == flag && argv[i+1] == target {
			return i
		}
	}
	return -1
}

func TestWrapBwrapConfigurablePaths(t *testing.T) {
	s, local, _ := newTestSandbox(t)
	base := filepath.Dir(filepath.Dir(local)) // parent of .../tenants
	roDir := filepath.Join(base, "shared-ro")
	rwDir := filepath.Join(base, "shared-rw")
	hideDir := filepath.Join(base, "secret-dir")
	hideFile := filepath.Join(base, "secret-file")
	for _, d := range []string{roDir, rwDir, hideDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(hideFile, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	s.roBinds = []string{roDir, filepath.Join(base, "not-exist")}
	s.rwBinds = []string{rwDir}
	s.hidePaths = []string{hideDir, hideFile}
	s.systemBinds = []string{"/usr"}

	workDir := filepath.Join(local, "work")
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		t.Fatal(err)
	}
	argv, err := s.WrapArgv(context.Background(), workDir, []string{"bash", "-lc", "ls"})
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(argv, "\x00")

	if !strings.Contains(joined, "--ro-bind\x00"+roDir+"\x00"+roDir) {
		t.Fatalf("expected ro-bind of shared-ro, argv=%v", argv)
	}
	if !strings.Contains(joined, "--bind\x00"+rwDir+"\x00"+rwDir) {
		t.Fatalf("expected rw bind of shared-rw, argv=%v", argv)
	}
	if strings.Contains(joined, "not-exist") {
		t.Fatalf("nonexistent path must be skipped, argv=%v", argv)
	}
	// Dirs are masked by ro-binding the shared read-only empty dir (invisible,
	// writes EROFS); files are masked with /dev/null
	if !strings.Contains(joined, "--ro-bind\x00"+s.roMaskDir+"\x00"+hideDir) {
		t.Fatalf("expected ro-bind mask of hideDir, argv=%v", argv)
	}
	if !strings.Contains(joined, "--ro-bind\x00/dev/null\x00"+hideFile) {
		t.Fatalf("expected /dev/null mask of hideFile, argv=%v", argv)
	}
	// systemBinds overrides the default list: /usr in, /lib out
	if !strings.Contains(joined, "--ro-bind\x00/usr\x00/usr") {
		t.Fatalf("expected /usr ro-bind, argv=%v", argv)
	}
	if strings.Contains(joined, "--ro-bind\x00/lib\x00/lib") {
		t.Fatalf("default system binds must be overridden, argv=%v", argv)
	}
	// Masks apply after the public binds
	roIdx := strings.Index(joined, "--ro-bind\x00"+roDir)
	hideIdx := strings.Index(joined, "--ro-bind\x00"+s.roMaskDir+"\x00"+hideDir)
	if roIdx < 0 || hideIdx < 0 || hideIdx < roIdx {
		t.Fatalf("hidePaths must apply after binds, argv=%v", argv)
	}
}

func TestWrapBwrapRoFloorFlag(t *testing.T) {
	s, local, _ := newTestSandbox(t)
	s.roFloorFlag = true
	workDir := filepath.Join(local, "work")
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		t.Fatal(err)
	}
	argv, err := s.WrapArgv(context.Background(), workDir, []string{"bash", "-lc", "ls"})
	if err != nil {
		t.Fatal(err)
	}
	tenantsBase := filepath.Dir(local)
	roBase := mountIndex(argv, "--remount-ro", tenantsBase)
	roRoot := mountIndex(argv, "--remount-ro", "/")
	bindIdx := mountIndex(argv, "--bind", local)
	if roBase < 0 || roRoot < 0 {
		t.Fatalf("expected --remount-ro for tenantsBase and /, argv=%v", argv)
	}
	// remount must apply after all mounts (last, before --)
	sep := slices.Index(argv, "--")
	if roBase < bindIdx || roBase > sep || roRoot > sep {
		t.Fatalf("remount-ro must apply after all binds and before --, argv=%v", argv)
	}
	// flag mode must not inject the pre-exec script
	for _, a := range argv {
		if strings.Contains(a, "remount,ro") {
			t.Fatalf("script fallback must not appear in flag mode, argv=%v", argv)
		}
	}
}

func TestWrapBwrapDisabledPassthrough(t *testing.T) {
	s := &Sandbox{on: false}
	inner := []string{"bash", "-lc", "ls"}
	argv, err := s.WrapArgv(context.Background(), "/tmp", inner)
	if err != nil {
		t.Fatal(err)
	}
	if slices.Compare(argv, inner) != 0 {
		t.Fatalf("disabled sandbox must pass through, argv=%v", argv)
	}
}

func TestGuardReadWrite(t *testing.T) {
	s, local, global := newTestSandbox(t)
	ctx := context.Background()
	base := filepath.Dir(filepath.Dir(local))
	rwDir := filepath.Join(base, "shared-rw")
	hideDir := filepath.Join(base, "secret-dir")
	for _, d := range []string{rwDir, hideDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	s.rwBinds = []string{rwDir}
	s.hidePaths = []string{hideDir}

	// Reads/writes inside the tenant root pass
	if err := s.CheckRead(ctx, filepath.Join(local, "work", "a.go")); err != nil {
		t.Fatalf("tenant read must pass: %v", err)
	}
	if err := s.CheckWrite(ctx, filepath.Join(local, "work", "a.go")); err != nil {
		t.Fatalf("tenant write must pass: %v", err)
	}
	// Neighbor tenants are unreadable and unwritable
	neighbor := filepath.Join(filepath.Dir(local), "tenant-b", "work", "x")
	if err := s.CheckRead(ctx, neighbor); err == nil {
		t.Fatal("neighbor read must be denied")
	}
	if err := s.CheckWrite(ctx, neighbor); err == nil {
		t.Fatal("neighbor write must be denied")
	}
	// The global root is readable but not writable; secrets are unreadable
	if err := s.CheckRead(ctx, filepath.Join(global, "skills", "s.md")); err != nil {
		t.Fatalf("global read must pass: %v", err)
	}
	if err := s.CheckWrite(ctx, filepath.Join(global, "skills", "s.md")); err == nil {
		t.Fatal("global write must be denied")
	}
	if err := s.CheckRead(ctx, filepath.Join(global, "secrets.enc.json")); err == nil {
		t.Fatal("secret file read must be denied")
	}
	// rwBinds are readable and writable; hidePaths are unreadable
	if err := s.CheckRead(ctx, filepath.Join(rwDir, "in.txt")); err != nil {
		t.Fatalf("rwBind read must pass: %v", err)
	}
	if err := s.CheckWrite(ctx, filepath.Join(rwDir, "out.txt")); err != nil {
		t.Fatalf("rwBind write must pass: %v", err)
	}
	if err := s.CheckRead(ctx, filepath.Join(hideDir, "f")); err == nil {
		t.Fatal("hidePath read must be denied")
	}
	// Reads outside the view allowlist are denied (aligned with the bwrap
	// view, where only the bound roots exist)
	if err := s.CheckRead(ctx, "/etc/passwd"); err == nil {
		t.Fatal("read of unrelated host path must be denied")
	}
	if err := s.CheckRead(ctx, filepath.Join(base, "unrelated")); err == nil {
		t.Fatal("read outside tenant/global/binds must be denied")
	}
	// Writes to unbound paths (e.g. /etc) are denied
	if err := s.CheckWrite(ctx, "/etc/passwd"); err == nil {
		t.Fatal("write outside tenant/rwBinds must be denied")
	}
}

func TestGuardDisabledAllowsAll(t *testing.T) {
	s := &Sandbox{on: false}
	ctx := context.Background()
	if err := s.CheckRead(ctx, "/etc/passwd"); err != nil {
		t.Fatal(err)
	}
	if err := s.CheckWrite(ctx, "/etc/passwd"); err != nil {
		t.Fatal(err)
	}
}

func TestTenantDirNameStableAndDistinct(t *testing.T) {
	a := tenantDirName("/data/tenants/a")
	if a != tenantDirName("/data/tenants/a") {
		t.Fatal("tenantDirName must be stable")
	}
	if a == tenantDirName("/other/tenants/a") {
		t.Fatal("same basename with different paths must not collide")
	}
}

func TestNewRejectsBadMode(t *testing.T) {
	_, err := New(Config{Mode: "nope"}, Deps{Workspace: agenttest.ScopedWorkspace{}})
	if err == nil {
		t.Fatal("expected error for invalid mode")
	}
}

func TestNewOffModeSkipsProbe(t *testing.T) {
	s, err := New(Config{Mode: ModeOff}, Deps{Workspace: agenttest.ScopedWorkspace{}})
	if err != nil {
		t.Fatal(err)
	}
	if s.Enabled() {
		t.Fatal("sandbox must be disabled in off mode")
	}
}
