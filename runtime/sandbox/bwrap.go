package sandbox

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// WrapArgv renders bwrap args for the current tenant and wraps the inner
// command (e.g. ["bash","-lc",cmd] or an ACP agent command). When the sandbox
// is disabled, inner is returned unchanged.
//
// View: tmpfs masks the tenant-root parent (neighbor tenants invisible) →
// bind back this tenant root (rw); global root read-only with secretFiles
// masked; minimal system ro-binds; no --unshare-net (127.0.0.1 / Pod network
// stack preserved).
func (s *Sandbox) WrapArgv(ctx context.Context, workDir string, inner []string) ([]string, error) {
	if !s.on {
		return inner, nil
	}
	v, err := s.render(ctx)
	if err != nil {
		return nil, err
	}

	args := []string{
		"--die-with-parent",
		"--new-session",
		"--unshare-user",
		// Map to uid/gid 0 inside the sandbox: the userns creator already holds
		// all caps in the namespace; mapping to 0 just lets userland euid
		// checks of tools like mount(8) pass (the ro remount depends on it).
		// On the host everything still runs as the real uid (e.g. devops:1020);
		// the privilege surface is unchanged.
		"--uid", "0",
		"--gid", "0",
		"--unshare-ipc",
		"--unshare-uts",
		// pid namespace: host processes are invisible and unkillable from the
		// sandbox (in- and outside share the devops uid, so without this a
		// tenant command could kill the runner itself). Does not affect the
		// network stack or loopback.
		"--unshare-pid",
	}
	// Minimal system binds (only if present); config.systemBinds overrides the
	// default list.
	systemBinds := s.systemBinds
	if len(systemBinds) == 0 {
		systemBinds = []string{
			"/usr", "/bin", "/lib", "/lib64", "/opt",
			"/etc/resolv.conf", "/etc/hosts", "/etc/passwd", "/etc/group", "/etc/nsswitch.conf",
			"/etc/ssl", "/etc/ca-certificates",
		}
	}
	for _, p := range systemBinds {
		if _, err := os.Stat(p); err == nil {
			args = append(args, "--ro-bind", p, p)
		}
	}
	// /tmp: by default bind a per-tenant subdir on the host tmpfs (/dev/shm) —
	// independent of the root overlay (which is remounted ro afterwards), and
	// /dev/shm has its own size cap, preventing tmpfs writes from eating node
	// memory. Empty tmpBase (explicit "tmpfs") falls back to bwrap's tmpfs.
	if s.tmpBase != "" {
		tenantTmp := filepath.Join(s.tmpBase, tenantDirName(v.tenantRoot))
		if err := os.MkdirAll(tenantTmp, 0o700); err != nil {
			return nil, fmt.Errorf("sandbox: mkdir tenant tmp: %w", err)
		}
		args = append(args, "--bind", tenantTmp, "/tmp")
	} else {
		args = append(args, "--tmpfs", "/tmp")
	}
	if s.procBind {
		args = append(args, "--ro-bind", "/proc", "/proc")
	} else {
		args = append(args, "--proc", "/proc")
	}
	args = append(args,
		"--dev", "/dev",
	)
	// Order matters: later mounts shadow earlier ones. First ro-bind the
	// global root (if it lives outside the tenant parent), then tmpfs-mask the
	// tenant parent, then bind back this tenant root (rw); ro/rwBinds, the
	// secrets mask and hidePaths apply after that.
	if v.globalRoot != v.tenantRoot && !strings.HasPrefix(v.globalRoot, v.tenantsBase+string(filepath.Separator)) {
		args = append(args, "--ro-bind", v.globalRoot, v.globalRoot)
	}
	args = append(args,
		"--tmpfs", v.tenantsBase,
		"--bind", v.tenantRoot, v.tenantRoot,
	)
	// Public paths: ro / rw binds (shared by all tenants).
	for _, p := range v.roBinds {
		args = append(args, "--ro-bind", p, p)
	}
	for _, p := range v.rwBinds {
		args = append(args, "--bind", p, p)
	}
	// Sensitive files masked with /dev/null (must come after the global
	// ro-bind and ro/rwBinds, so rwBinds binding the global root or its parent
	// cannot re-expose secrets).
	for _, p := range v.secretPaths {
		if _, err := os.Stat(p); err == nil {
			args = append(args, "--ro-bind", "/dev/null", p)
		}
	}
	// Mask paths apply last and can cover any subpath exposed by previous
	// binds: dirs get a ro-bind of the shared read-only empty dir, files get
	// /dev/null.
	roMaskDir := ""
	if len(v.hidePaths) > 0 {
		if dir, err := s.ensureRoMaskDir(v.globalRoot); err != nil {
			slog.Warn("sandbox: ro mask dir unavailable, hidePaths dirs fall back to tmpfs", "err", err)
		} else {
			roMaskDir = dir
		}
	}
	for _, p := range v.hidePaths {
		if info, err := os.Stat(p); err == nil && info.IsDir() {
			if roMaskDir != "" {
				// ro-bind the shared read-only empty dir: invisible and writes
				// fail with EROFS.
				args = append(args, "--ro-bind", roMaskDir, p)
			} else {
				args = append(args, "--tmpfs", p)
			}
		} else {
			args = append(args, "--ro-bind", "/dev/null", p)
		}
	}
	args = append(args,
		"--chdir", workDir,
		"--setenv", "HOME", v.tenantRoot,
		"--setenv", "TMPDIR", "/tmp",
	)
	if s.roFloorFlag {
		// Preferred: bwrap's native --remount-ro makes the mask tmpfs and root
		// overlay read-only during setup (must come after all mounts; submounts
		// like the tenant root, /tmp, /proc are unaffected). A setup failure
		// makes bwrap error out — fail closed and visible.
		args = append(args, "--remount-ro", v.tenantsBase, "--remount-ro", "/", "--")
	} else {
		// Fallback: a pre-exec script remounts (may be denied by LSM; the
		// script fails closed and the command does not run).
		args = append(args,
			// Overlay layers to remount read-only before exec (root layer +
			// tenant-parent mask layer), passed via env so paths stay out of
			// the script; the script unsets it so it never leaks to the user
			// command.
			"--setenv", roMountsEnv, v.tenantsBase,
			"--",
		)
		inner = append([]string{"bash", "-c", roRemountScript, "bwrap-ro-remount"}, inner...)
	}
	// bwrap availability was probed in New (fail closed); resolve via PATH.
	// With the read-only floor in effect, writes to paths not explicitly
	// rw-bound fail with EROFS instead of "succeeding" into an overlay that
	// vanishes with the namespace.
	return append([]string{"bwrap"}, append(args, inner...)...), nil
}

// probeBwrap verifies bwrap exists and unprivileged user/pid namespaces are
// usable; it also probes whether a fresh procfs can be mounted in the new pid
// namespace — when the container's /proc has locked mounts (e.g. Docker masks
// /proc/kcore by default) the kernel rejects it (EPERM) and procBind=true is
// returned so the caller degrades to --ro-bind /proc /proc.
func probeBwrap() (procBind, roFloorFlag bool, err error) {
	bin, err := exec.LookPath("bwrap")
	if err != nil {
		return false, false, fmt.Errorf("bwrap not found in PATH")
	}
	probe := func(args ...string) error {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		args = append([]string{"--unshare-user", "--uid", "0", "--gid", "0", "--unshare-pid"}, args...)
		if out, err := exec.CommandContext(ctx, bin, args...).CombinedOutput(); err != nil {
			return fmt.Errorf("%w (%s)", err, strings.TrimSpace(string(out)))
		}
		return nil
	}
	if err := probe("--dev-bind", "/", "/", "true"); err != nil {
		return false, false, fmt.Errorf("bwrap userns probe failed: %w", err)
	}
	// With locked mounts on the container's /proc (e.g. Docker masks
	// /proc/kcore by default), the kernel rejects a fresh procfs in the new
	// pid namespace (EPERM); degrade to ro-binding the host /proc.
	if err := probe("--proc", "/proc", "--dev-bind", "/", "/", "true"); err == nil {
		procBind = false
	} else if err := probe("--ro-bind", "/proc", "/proc", "--dev-bind", "/", "/", "true"); err != nil {
		return false, false, fmt.Errorf("bwrap proc fallback probe failed: %w", err)
	} else {
		procBind = true
	}
	// bwrap's native --remount-ro runs during setup, a different privilege
	// context than in-sandbox user commands; when probing shows it unavailable
	// (old bwrap or LSM denial) fall back to the pre-exec script.
	if err := probe("--dev-bind", "/", "/", "--remount-ro", "/", "true"); err == nil {
		roFloorFlag = true
	}
	return procBind, roFloorFlag, nil
}

// roMountsEnv passes the mount points to remount read-only (colon-separated).
const roMountsEnv = "__BWRAP_RO_MOUNTS"

// roRemountScript runs inside the sandbox before the user command: it
// remounts bwrap's implicit root overlay and the mask tmpfs read-only (the
// userns root has CAP_SYS_ADMIN; submounts like the tenant root, /tmp, /proc
// are unaffected). A failed remount fails closed: the script exits non-zero
// with a clear stderr message and the user command never runs — a broken
// read-only floor must not silently degrade to a writable overlay.
const roRemountScript = `mount -o remount,ro / 2>/dev/null || { echo "sandbox: read-only remount of / failed, refusing to run unsandboxed" >&2; exit 126; }
if [ -n "$` + roMountsEnv + `" ]; then
  oldifs=$IFS; IFS=':'
  for m in $` + roMountsEnv + `; do
    mount -o remount,ro "$m" 2>/dev/null || { echo "sandbox: read-only remount of $m failed, refusing to run unsandboxed" >&2; exit 126; }
  done
  IFS=$oldifs
fi
unset ` + roMountsEnv + `
exec "$@"`
