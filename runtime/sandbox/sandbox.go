// Package sandbox provides sandbox/bwrap: the single source of truth for the
// tenant sandbox view. The same view config drives two enforcement mechanisms:
//
//   - WrapArgv: subprocess isolation — translates the view into a bwrap argv
//     prefix around bash / ACP subprocesses (each call runs in its own mount
//     namespace, keeping 127.0.0.1 / the Pod network stack);
//   - CheckRead / CheckWrite: in-process enforcement — for the
//     filesystem/sandbox decorator and other in-process tools that bwrap
//     cannot confine.
//
// View: tmpfs masks the tenant-root parent (neighbor tenants invisible) →
// bind back this tenant root (rw); global root read-only with secretFiles
// masked; minimal system ro-binds; no --unshare-net.
package sandbox

import (
	"context"
	"crypto/sha1"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	capsandbox "github.com/lengzhao/agentkit/cap/sandbox"
	"github.com/lengzhao/agentkit/cap/workspace"
	"github.com/lengzhao/pluginkit"
)

// Sandbox modes.
const (
	ModeOff   = "off"   // no wrapping, no checks — equivalent to no sandbox
	ModeAuto  = "auto"  // enable when bwrap is usable, warn and degrade otherwise (local dev only)
	ModeBwrap = "bwrap" // force enable; missing deps fail closed per FailIfUnavailable (default)
)

// defaultSecretFiles is the built-in SecretFiles default.
var defaultSecretFiles = []string{"secrets.enc.json"}

// Config configures sandbox/bwrap: the tenant view fields (roBinds/rwBinds/
// hidePaths/secretFiles — shared by subprocess wrapping and in-process
// enforcement) plus the bwrap-mechanism knobs (mode/systemBinds/tmpBase/
// probe-failure policy).
type Config struct {
	Mode              string `json:"mode"`
	FailIfUnavailable *bool  `json:"failIfUnavailable"`
	// Env marks the deployment environment: "prod"/"production" upgrades
	// mode=auto to fail-closed bwrap, so a misconfig cannot silently run
	// unsandboxed in production. Explicit config, no implicit env detection.
	Env string `json:"env"`
	// RoBinds are extra read-only binds (public read-only paths shared by all
	// tenants; source and target are the same path). Host absolute paths or
	// workspace refs (e.g. "global:share_dir", resolved per call per tenant).
	RoBinds []string `json:"roBinds"`
	// RwBinds are extra read-write binds (public writable paths shared by all
	// tenants; beware concurrent write conflicts). Host absolute paths or
	// workspace refs (missing dirs are created).
	RwBinds []string `json:"rwBinds"`
	// HidePaths are masked: directories become invisible and read-only,
	// files are masked empty. Host absolute paths or workspace refs. Applied
	// after all binds, so they can mask sensitive subpaths of system binds or
	// the global root. Resolution fails closed (a failed mask means a leak).
	HidePaths []string `json:"hidePaths"`
	// SecretFiles are sensitive files under the global root masked empty
	// (relative to the global root), default ["secrets.enc.json"].
	// Applied after the global ro-bind and ro/rwBinds.
	SecretFiles []string `json:"secretFiles"`
	// HomeRef selects the in-sandbox HOME. Default "." (the writable tenant
	// root, i.e. workspace "."). Values: a workspace ref ("global:dir",
	// "local:path"), a host absolute path, or the literal "host" (pass the
	// host user's home through, ro-bound — intended for single-tenant trusted
	// setups). The resolved home must live inside the view (tenant root,
	// global root or a configured bind); otherwise rendering fails closed
	// instead of pointing HOME at an unmounted void.
	HomeRef string `json:"homeRef"`
	// Maps bind a host source onto a different in-sandbox destination, e.g.
	// share the host's ~/.ssh or ~/.gitconfig into the tenant HOME. Applied
	// after ro/rwBinds and before secretFiles/hidePaths (masks always win).
	Maps []MapConfig `json:"maps"`
	// SystemBinds overrides the default minimal system read-only bind list;
	// empty uses the built-in default (/usr /bin /lib /lib64 /opt plus
	// DNS/cert files under /etc).
	SystemBinds []string `json:"systemBinds"`
	// TmpBase is the base dir on a host tmpfs (default /dev/shm/shellbwrap):
	// each tenant gets a subdir bound rw as in-sandbox /tmp. /dev/shm has its
	// own size cap (usually half of RAM), preventing tmpfs writes from eating
	// node memory. The explicit value "tmpfs" falls back to bwrap's own
	// --tmpfs /tmp (no size limit, not recommended). Must be exclusive per
	// runner instance on a shared host: startup removes leftover contents.
	TmpBase string `json:"tmpBase"`
}

// Map modes for MapConfig.Mode.
const (
	MapModeRO = "ro" // default: read-only mapping
	MapModeRW = "rw" // read-write mapping (tenant can modify the host source)
)

// MapConfig binds a host source onto a different in-sandbox destination.
type MapConfig struct {
	// Src is the host source: an absolute path or a workspace ref
	// ("global:...", "local:..."), resolved per call per tenant.
	Src string `json:"src"`
	// Dst is the in-sandbox destination: an absolute path, or a relative path
	// resolved against the rendered HOME (e.g. ".ssh" → $HOME/.ssh), so the
	// mapping follows homeRef. A relative dst must not escape HOME ("..").
	Dst string `json:"dst"`
	// Mode is "ro" (default) or "rw". rw hands the host source's writable
	// surface to the tenant — a deliberate choice (e.g. never rw-map .ssh).
	Mode string `json:"mode"`
}

// SetDefaults implements pluginkit.Defaulter.
func (c *Config) SetDefaults() {
	if strings.TrimSpace(c.Mode) == "" {
		c.Mode = ModeAuto
	}
	if c.TmpBase == "" {
		c.TmpBase = "/dev/shm/shellbwrap"
	}
	if c.SecretFiles == nil {
		c.SecretFiles = append([]string(nil), defaultSecretFiles...)
	}
}

// Validate implements pluginkit.Validator.
func (c *Config) Validate() error {
	switch mode := strings.TrimSpace(c.Mode); mode {
	case ModeOff, ModeAuto, ModeBwrap:
	default:
		return fmt.Errorf("sandbox/bwrap config.mode must be off, auto or bwrap, got %q", mode)
	}
	for i, m := range c.Maps {
		if strings.TrimSpace(m.Src) == "" || strings.TrimSpace(m.Dst) == "" {
			return fmt.Errorf("sandbox/bwrap config.maps[%d]: src and dst are required", i)
		}
		switch strings.TrimSpace(m.Mode) {
		case "", MapModeRO, MapModeRW:
		default:
			return fmt.Errorf("sandbox/bwrap config.maps[%d].mode must be ro or rw, got %q", i, m.Mode)
		}
	}
	return nil
}

// Deps for sandbox/bwrap.
type Deps struct {
	Workspace workspace.Service `json:"workspace"`
}

func init() {
	pluginkit.Register("sandbox/bwrap", New)
}

// Sandbox is the tenant sandbox view; tenant identity is resolved from ctx on
// every call.
type Sandbox struct {
	workspace   workspace.Service
	roBinds     []string
	rwBinds     []string
	hidePaths   []string
	systemBinds []string
	on          bool
	tmpBase     string
	secretFiles []string
	homeRef     string
	maps        []MapConfig
	// procBind=true uses --ro-bind /proc /proc instead of --proc /proc: when
	// the container's /proc has locked mounts (e.g. Docker masks /proc/kcore by
	// default) the kernel rejects a fresh procfs mount in the new pid
	// namespace (EPERM). The fallback exposes host processes via ps, but the
	// pid namespace still isolates signals (killing a host pid returns ESRCH).
	procBind bool
	// roFloorFlag=true uses bwrap's native --remount-ro to make the root
	// overlay / mask tmpfs read-only (executed by bwrap during setup, a
	// different privilege context than in-sandbox user commands — an
	// in-sandbox mount(8) remount may be denied by LSM). Falls back to the
	// pre-exec script when probing shows it unsupported; both paths fail
	// closed (bwrap setup error / script exit 126).
	roFloorFlag bool
	// roMaskDir is a process-wide shared read-only empty dir ro-bound onto
	// hidePaths directories as a mask: masked dirs are not just invisible,
	// writes fail with EROFS (a tmpfs mask would silently accept writes and
	// drop them on exit). Shared by all tenants, always empty, no isolation
	// concern. Lazily created under the data root (so host /tmp cleanup
	// policies cannot delete the mask source).
	roMaskDir  string
	roMaskOnce sync.Once
	roMaskErr  error
	// viewCache caches rendered views per tenant root for viewCacheTTL:
	// CheckRead/CheckWrite are the fs-tool hot path (hundreds of calls per
	// session) and rendering stats/realpaths every configured bind each time.
	// Tenant root resolution itself stays per call (it is the cache key and
	// carries the ctx tenant identity).
	viewMu sync.Mutex
	views  map[string]cachedView
}

// viewCacheTTL bounds how long a rendered view is reused. Bind/mask config
// is static per process; the TTL only covers host filesystem changes (a bind
// target appearing/disappearing), so it can be generous.
const viewCacheTTL = 30 * time.Second

// viewCacheMax caps the cached views; beyond it, expired entries are evicted
// on insert (and the whole map resets if nothing expired — tenants are
// bounded in practice, this only guards pathological churn).
const viewCacheMax = 1024

type cachedView struct {
	v   *view
	exp time.Time
}

// New constructs sandbox/bwrap. With mode=bwrap and unavailable deps (bwrap
// not in PATH or unprivileged user namespaces disabled) it fails closed per
// failIfUnavailable (default true).
func New(cfg Config, d Deps) (capsandbox.Service, error) {
	if d.Workspace == nil {
		return nil, fmt.Errorf("sandbox/bwrap requires workspace")
	}
	cfg.SetDefaults()
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	mode := strings.TrimSpace(cfg.Mode)

	// In production, auto is treated as bwrap (fail closed) so a misconfig
	// cannot silently run unsandboxed.
	effectiveMode := mode
	if mode == ModeAuto {
		switch strings.ToLower(strings.TrimSpace(cfg.Env)) {
		case "prod", "production":
			effectiveMode = ModeBwrap
		}
	}

	on := false
	procBind := false
	roFloorFlag := false
	if effectiveMode != ModeOff {
		pb, rf, err := probeBwrap()
		switch {
		case err == nil:
			on = true
			procBind = pb
			roFloorFlag = rf
			if pb {
				slog.Warn("sandbox/bwrap: fresh procfs mount not permitted, fall back to ro-bind /proc (host processes visible via ps; signals still isolated by pid ns)")
			}
			if !rf {
				slog.Warn("sandbox/bwrap: bwrap --remount-ro unavailable, fall back to in-sandbox remount script (may be denied by LSM; the script fails closed and commands will not run)")
			}
		case effectiveMode == ModeBwrap && (cfg.FailIfUnavailable == nil || *cfg.FailIfUnavailable):
			return nil, fmt.Errorf("sandbox/bwrap: bwrap unavailable (fail closed): %w", err)
		default:
			slog.Warn("sandbox/bwrap: bwrap unavailable, sandbox disabled", "mode", mode, "err", err)
		}
	}

	tmpBase := cfg.TmpBase
	if tmpBase == "" {
		tmpBase = "/dev/shm/shellbwrap"
	}
	if tmpBase == "tmpfs" {
		// Explicit "tmpfs": fall back to bwrap's own --tmpfs /tmp (no size
		// limit, not recommended).
		tmpBase = ""
	}
	secretFiles := cfg.SecretFiles
	if secretFiles == nil {
		secretFiles = defaultSecretFiles
	}
	s := &Sandbox{
		workspace:   d.Workspace,
		roBinds:     cfg.RoBinds,
		rwBinds:     cfg.RwBinds,
		hidePaths:   cfg.HidePaths,
		systemBinds: cfg.SystemBinds,
		on:          on,
		procBind:    procBind,
		roFloorFlag: roFloorFlag,
		tmpBase:     tmpBase,
		secretFiles: secretFiles,
		homeRef:     strings.TrimSpace(cfg.HomeRef),
		maps:        cfg.Maps,
	}
	if s.homeRef == "host" {
		slog.Warn("sandbox/bwrap: homeRef=host exposes the host home read-only; intended for single-tenant trusted setups")
	}
	if on && tmpBase != "" {
		// Old sandboxes die with the runner process, so leftover tenant tmp
		// dirs are safe to remove on restart.
		if err := os.RemoveAll(tmpBase); err != nil {
			slog.Warn("sandbox/bwrap: clean tmp base failed", "dir", tmpBase, "err", err)
		}
		if err := os.MkdirAll(tmpBase, 0o700); err != nil {
			return nil, fmt.Errorf("sandbox/bwrap: mkdir tmp base %s: %w", tmpBase, err)
		}
	}
	slog.Info("sandbox/bwrap ready", "sandbox", on)
	return s, nil
}

// Enabled reports whether the sandbox is active (false in off mode or after
// an auto-mode probe fallback). When false, WrapArgv passes through and
// CheckRead/CheckWrite allow everything (confinement falls back to the tool
// layer).
func (s *Sandbox) Enabled() bool { return s.on }

// view is the rendered per-tenant view (all paths realpath-normalized).
type view struct {
	tenantRoot  string
	globalRoot  string
	tenantsBase string
	// home is the in-sandbox HOME (default tenantRoot; see Config.HomeRef).
	home        string
	roBinds     []string
	rwBinds     []string
	roMaps      []pathMap
	rwMaps      []pathMap
	hidePaths   []string
	secretPaths []string
}

// pathMap is a resolved src→dst bind (Config.Maps entry).
type pathMap struct {
	src string
	dst string
}

// render resolves the current tenant from ctx and renders the view. Tenant
// identity comes from ctx and must be resolved on every call; the rendered
// view is cached per tenant root for viewCacheTTL (see viewCacheTTL).
func (s *Sandbox) render(ctx context.Context) (*view, error) {
	tenantRoot, err := resolveClean(ctx, s.workspace, ".")
	if err != nil {
		return nil, fmt.Errorf("sandbox: resolve tenant root: %w", err)
	}
	s.viewMu.Lock()
	if s.views != nil {
		if c, ok := s.views[tenantRoot]; ok && time.Now().Before(c.exp) {
			s.viewMu.Unlock()
			return c.v, nil
		}
	}
	s.viewMu.Unlock()

	v, err := s.renderView(ctx, tenantRoot)
	if err != nil {
		return nil, err
	}
	s.viewMu.Lock()
	if s.views == nil {
		s.views = map[string]cachedView{}
	}
	if len(s.views) >= viewCacheMax {
		now := time.Now()
		for k, c := range s.views {
			if now.After(c.exp) {
				delete(s.views, k)
			}
		}
		if len(s.views) >= viewCacheMax {
			s.views = map[string]cachedView{}
		}
	}
	s.views[tenantRoot] = cachedView{v: v, exp: time.Now().Add(viewCacheTTL)}
	s.viewMu.Unlock()
	return v, nil
}

// renderView renders the view for an already-resolved tenant root.
func (s *Sandbox) renderView(ctx context.Context, tenantRoot string) (*view, error) {
	globalRoot, err := resolveClean(ctx, s.workspace, "global:.")
	if err != nil {
		return nil, fmt.Errorf("sandbox: resolve global root: %w", err)
	}
	tenantsBase := filepath.Dir(tenantRoot)
	if tenantsBase == tenantRoot || tenantsBase == "." {
		return nil, fmt.Errorf("sandbox: tenant root %q has no maskable parent", tenantRoot)
	}
	// Unresolvable public binds degrade to warn+skip (a single bad config
	// entry must not take the tool down entirely).
	roBinds, err := s.resolveBinds(ctx, s.roBinds, false, false)
	if err != nil {
		return nil, err
	}
	rwBinds, err := s.resolveBinds(ctx, s.rwBinds, true, false)
	if err != nil {
		return nil, err
	}
	// Mask path resolution must fail closed (a failed mask means a leak).
	hidePaths, err := s.resolveBinds(ctx, s.hidePaths, false, true)
	if err != nil {
		return nil, err
	}
	// HOME: default the writable tenant root; "host" passes the host user's
	// home through as a read-only bind (single-tenant trusted setups). Any
	// other homeRef must resolve inside the view — HOME pointing at an
	// unmounted path is a config error, not a runtime state.
	home := tenantRoot
	if ref := s.homeRef; ref != "" && ref != "." {
		if ref == "host" {
			hostHome, err := os.UserHomeDir()
			if err != nil {
				return nil, fmt.Errorf("sandbox: resolve host home: %w", err)
			}
			home, err = cleanExisting(hostHome)
			if err != nil {
				return nil, fmt.Errorf("sandbox: resolve host home: %w", err)
			}
			roBinds = append(roBinds, home)
		} else {
			home, err = s.resolveHomeRef(ctx, ref)
			if err != nil {
				return nil, err
			}
			if !withinAny(home, []string{tenantRoot, globalRoot}, roBinds, rwBinds) {
				return nil, fmt.Errorf("sandbox: homeRef %q resolves to %s outside the sandbox view (tenant root, global root, ro/rwBinds)", ref, home)
			}
		}
	}
	// Maps: src resolution degrades to warn+skip like ro/rwBinds, but a src
	// inside a hidePath fails closed — maps must not re-expose masked paths
	// under a different location.
	roMaps, rwMaps, err := s.resolveMaps(ctx, home, hidePaths)
	if err != nil {
		return nil, err
	}
	secretFiles := s.secretFiles
	if secretFiles == nil {
		secretFiles = defaultSecretFiles
	}
	var secretPaths []string
	for _, f := range secretFiles {
		secretPaths = append(secretPaths, filepath.Join(globalRoot, f))
	}
	return &view{
		tenantRoot:  tenantRoot,
		globalRoot:  globalRoot,
		tenantsBase: tenantsBase,
		home:        home,
		roBinds:     roBinds,
		rwBinds:     rwBinds,
		roMaps:      roMaps,
		rwMaps:      rwMaps,
		hidePaths:   hidePaths,
		secretPaths: secretPaths,
	}, nil
}

// resolveHomeRef resolves a non-default homeRef (workspace ref or host
// absolute path) to a canonical existing path.
func (s *Sandbox) resolveHomeRef(ctx context.Context, ref string) (string, error) {
	if filepath.IsAbs(ref) {
		home, err := cleanExisting(ref)
		if err != nil {
			return "", fmt.Errorf("sandbox: resolve homeRef %q: %w", ref, err)
		}
		return home, nil
	}
	home, err := resolveClean(ctx, s.workspace, ref)
	if err != nil {
		return "", fmt.Errorf("sandbox: resolve homeRef %q: %w", ref, err)
	}
	return home, nil
}

// resolveMaps renders Config.Maps entries against the rendered HOME.
func (s *Sandbox) resolveMaps(ctx context.Context, home string, hidePaths []string) (roMaps, rwMaps []pathMap, err error) {
	for _, m := range s.maps {
		src := strings.TrimSpace(m.Src)
		if isWorkspaceRef(src) {
			resolved, rerr := s.workspace.Resolve(ctx, src)
			if rerr != nil {
				slog.Warn("sandbox: skip map with unresolvable src", "src", src, "err", rerr)
				continue
			}
			src = resolved
		}
		src, rerr := cleanExisting(src)
		if rerr != nil {
			slog.Warn("sandbox: skip map with unresolvable src", "src", m.Src, "err", rerr)
			continue
		}
		for _, h := range hidePaths {
			if within(src, h) {
				return nil, nil, fmt.Errorf("sandbox: map src %q is inside masked path %s (maps must not re-expose masked paths)", m.Src, h)
			}
		}
		dst := strings.TrimSpace(m.Dst)
		if filepath.IsAbs(dst) {
			dst = filepath.Clean(dst)
		} else {
			dst = filepath.Join(home, dst)
			if !within(dst, home) {
				return nil, nil, fmt.Errorf("sandbox: map dst %q escapes HOME (%s)", m.Dst, home)
			}
		}
		pm := pathMap{src: src, dst: dst}
		if strings.TrimSpace(m.Mode) == MapModeRW {
			rwMaps = append(rwMaps, pm)
		} else {
			roMaps = append(roMaps, pm)
		}
	}
	return roMaps, rwMaps, nil
}

// TranslatePath implements capsandbox.PathTranslator: a path under a map dst
// is rewritten to its host src (longest dst prefix wins), so in-process
// consumers read/write the same backing content the subprocess sees at dst.
// The dst itself may not exist on the host — without this translation the
// in-process view would be an empty shell compared to the bwrap view.
func (s *Sandbox) TranslatePath(ctx context.Context, path string) (string, bool) {
	if !s.on {
		return "", false
	}
	v, err := s.render(ctx)
	if err != nil {
		return "", false
	}
	p := cleanHostPath(path)
	var best *pathMap
	for _, maps := range [][]pathMap{v.roMaps, v.rwMaps} {
		for i := range maps {
			m := &maps[i]
			if within(p, m.dst) && (best == nil || len(m.dst) > len(best.dst)) {
				best = m
			}
		}
	}
	if best == nil {
		return "", false
	}
	if p == best.dst {
		return best.src, true
	}
	return filepath.Join(best.src, strings.TrimPrefix(p, best.dst+string(filepath.Separator))), true
}

// cleanExisting normalizes a host path that must exist (realpath).
func cleanExisting(p string) (string, error) {
	p = filepath.Clean(strings.TrimSpace(p))
	real, err := filepath.EvalSymlinks(p)
	if err != nil {
		return "", err
	}
	return real, nil
}

// withinAny reports whether p equals or lives under any of roots or the
// paths in lists.
func withinAny(p string, roots []string, lists ...[]string) bool {
	for _, r := range roots {
		if within(p, r) {
			return true
		}
	}
	for _, list := range lists {
		for _, r := range list {
			if within(p, r) {
				return true
			}
		}
	}
	return false
}

// resolveBinds normalizes bind configs and drops nonexistent entries. Two
// entry forms:
//   - host absolute path: used directly (trimmed, Cleaned, realpath'd);
//   - workspace ref (known scope prefix "global:"/"local:", e.g.
//     "global:share_dir"): resolved per call per tenant via workspace.Resolve;
//     with create=true a missing target dir is created (for rw-bind shared
//     dirs).
//
// A single entry failing to resolve: with strict=false (ro/rwBinds) warn and
// skip — a bad config entry must not take the tool down; with strict=true
// (hidePaths masks) fail closed — a failed mask means a leak.
func (s *Sandbox) resolveBinds(ctx context.Context, paths []string, create, strict bool) ([]string, error) {
	var out []string
	for _, p := range paths {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if isWorkspaceRef(p) {
			resolved, err := s.workspace.Resolve(ctx, p)
			if err == nil && create {
				err = ensureRWBindTarget(resolved)
			}
			if err != nil {
				if strict {
					return nil, fmt.Errorf("sandbox: resolve bind %q: %w", p, err)
				}
				slog.Warn("sandbox: skip unresolvable bind", "path", p, "err", err)
				continue
			}
			p = resolved
		}
		p = filepath.Clean(p)
		if p == "." {
			continue
		}
		if real, err := filepath.EvalSymlinks(p); err == nil {
			p = real
		}
		if _, err := os.Stat(p); err == nil {
			out = append(out, p)
		}
	}
	return out, nil
}

// isWorkspaceRef detects workspace refs: known scope prefixes only, so host
// paths containing a colon are not misparsed.
func isWorkspaceRef(p string) bool {
	return strings.HasPrefix(p, "global:") || strings.HasPrefix(p, "local:")
}

// ensureRWBindTarget creates a missing rw-bind target. Existing files are left
// alone (MkdirAll on a file path fails with "not a directory").
func ensureRWBindTarget(path string) error {
	_, err := os.Stat(path)
	if err == nil {
		return nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return os.MkdirAll(path, 0o755)
}

// tenantDirName derives a stable tmp subdir name from the tenant root path
// (basename + short hash to avoid collisions).
func tenantDirName(tenantRoot string) string {
	sum := sha1.Sum([]byte(tenantRoot))
	return fmt.Sprintf("%s-%x", filepath.Base(tenantRoot), sum[:4])
}

// ensureRoMaskDir lazily creates the process-wide shared read-only mask dir
// under the data root (parent of the global root), so host /tmp cleanup
// policies cannot delete the mask source. Always empty, shared by all tenants.
func (s *Sandbox) ensureRoMaskDir(globalRoot string) (string, error) {
	s.roMaskOnce.Do(func() {
		dir := filepath.Join(filepath.Dir(globalRoot), ".shellbwrap-ro-mask")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			s.roMaskErr = err
			return
		}
		s.roMaskDir = dir
	})
	return s.roMaskDir, s.roMaskErr
}

// resolveClean resolves a workspace-relative path and normalizes it
// (realpath); binds always use canonical paths so the in-sandbox and host
// views cannot diverge through symlinks.
func resolveClean(ctx context.Context, ws workspace.Service, rel string) (string, error) {
	p, err := ws.Resolve(ctx, rel)
	if err != nil {
		return "", err
	}
	p = filepath.Clean(p)
	real, err := filepath.EvalSymlinks(p)
	if err != nil {
		return "", fmt.Errorf("eval symlinks %s: %w", p, err)
	}
	return real, nil
}

var _ capsandbox.Service = (*Sandbox)(nil)
var _ capsandbox.PathTranslator = (*Sandbox)(nil)
