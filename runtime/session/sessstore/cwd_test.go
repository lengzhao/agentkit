package sessstore_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/cap/workspace"
	"github.com/lengzhao/agentkit/runtime/session/sessstore"
	rtworkspace "github.com/lengzhao/agentkit/runtime/workspace"
)

func TestCwdPersistenceAndOtherBindings(t *testing.T) {
	for _, backend := range []string{"file", "sqlite", "memory"} {
		t.Run(backend, func(t *testing.T) {
			root := t.TempDir()
			open := func() agentkit.SessionStore {
				switch backend {
				case "file":
					s, e := sessstore.NewStore(sessstore.StoreConfig{Dir: "sessions"}, sessstore.StoreDeps{Workspace: rtworkspace.Static(root)})
					if e != nil {
						t.Fatal(e)
					}
					return s
				case "sqlite":
					s, e := sessstore.NewSQLStore(sessstore.SQLStoreConfig{Driver: "sqlite", DSN: filepath.Join(root, "sessions.db")}, sessstore.SQLStoreDeps{})
					if e != nil {
						t.Fatal(e)
					}
					return s
				default:
					m, _ := sessstore.NewMemory(sessstore.MemoryConfig{ID: "test"})
					return sessstore.NewStaticStore(m)
				}
			}
			s := open()
			cwd := s.(agentkit.SessionCwdStore)
			rt := s.(agentkit.SessionRuntimeStore)
			ctx := context.Background()
			if err := cwd.SetCwdBind(ctx, "one", "/project/a"); err != nil {
				t.Fatal(err)
			}
			if err := rt.SetAgentBind(ctx, "one", "agent"); err != nil {
				t.Fatal(err)
			}
			if err := rt.SetModelBind(ctx, "one", "model"); err != nil {
				t.Fatal(err)
			}
			if err := rt.SetAgentBind(ctx, "one", ""); err != nil {
				t.Fatal(err)
			}
			if err := rt.SetModelBind(ctx, "one", ""); err != nil {
				t.Fatal(err)
			}
			if backend != "memory" {
				s = open()
				cwd = s.(agentkit.SessionCwdStore)
				rt = s.(agentkit.SessionRuntimeStore)
			}
			if got, err := cwd.CwdBind(ctx, "one"); err != nil || got != "/project/a" {
				t.Fatalf("persisted cwd %q %v", got, err)
			}
			if got, err := cwd.CwdBind(ctx, "other"); err != nil || got != "" {
				t.Fatalf("leaked cwd %q %v", got, err)
			}
			if err := rt.SetAgentBind(ctx, "one", "kept"); err != nil {
				t.Fatal(err)
			}
			if err := cwd.SetCwdBind(ctx, "one", ""); err != nil {
				t.Fatal(err)
			}
			if got, err := cwd.CwdBind(ctx, "one"); err != nil || got != "" {
				t.Fatalf("reset cwd %q %v", got, err)
			}
			if got, err := rt.AgentBind(ctx, "one"); err != nil || got != "kept" {
				t.Fatalf("reset lost agent %q %v", got, err)
			}
		})
	}
}

func TestLoadedWindowRetainsContextBoundary(t *testing.T) {
	events := []agentkit.SessionEvent{
		{Seq: 1, Type: agentkit.EventCompaction, Data: []byte(`{"summary":{"role":"assistant","content":[{"type":"text","text":"old project summary"}]}}`)},
		{Seq: 2, Type: agentkit.EventSessionContext, Data: []byte(`{"cwd":"/new"}`)},
		{Seq: 3, Type: agentkit.EventUserMessage},
		{Seq: 4, Type: agentkit.EventAssistantMessage},
	}
	loaded, _, _, err := sessstore.FoldLoadedSessionEvents(events, 1)
	if err != nil {
		t.Fatal(err)
	}
	for _, event := range loaded {
		if event.Type == agentkit.EventSessionContext {
			return
		}
	}
	t.Fatal("loaded window dropped session context boundary; old compaction may be replayed")
}

func TestFileRuntimeConcurrentFieldsAcrossStoreInstances(t *testing.T) {
	root := t.TempDir()
	ctx := context.Background()
	open := func() agentkit.SessionStore {
		s, e := sessstore.NewStore(sessstore.StoreConfig{Dir: "sessions"}, sessstore.StoreDeps{Workspace: rtworkspace.Static(root)})
		if e != nil {
			t.Fatal(e)
		}
		return s
	}
	a, b := open(), open()
	for i := 0; i < 25; i++ {
		id := agentkit.SessionID(fmt.Sprintf("parallel-%d", i))
		start := make(chan struct{})
		errCh := make(chan error, 3)
		go func() { <-start; errCh <- a.(agentkit.SessionCwdStore).SetCwdBind(ctx, id, "/project") }()
		go func() { <-start; errCh <- b.(agentkit.SessionRuntimeStore).SetModelBind(ctx, id, "model") }()
		go func() { <-start; errCh <- a.(agentkit.SessionRuntimeStore).SetAgentBind(ctx, id, "agent") }()
		close(start)
		for range 3 {
			if err := <-errCh; err != nil {
				t.Fatal(err)
			}
		}
		raw, err := os.ReadFile(filepath.Join(root, "sessions", string(id), "runtime.json"))
		if err != nil {
			t.Fatal(err)
		}
		var data struct {
			Cwd   string `json:"cwd"`
			Model string `json:"model"`
			Agent string `json:"agentId"`
		}
		if err := json.Unmarshal(raw, &data); err != nil {
			t.Fatal(err)
		}
		if data.Cwd != "/project" || data.Model != "model" || data.Agent != "agent" {
			t.Fatalf("lost update: %s", raw)
		}
	}
}

func TestFileRuntimeReadsMemoryAndWritesCompleteSnapshot(t *testing.T) {
	root := t.TempDir()
	ctx := context.Background()
	s, err := sessstore.NewStore(sessstore.StoreConfig{Dir: "."}, sessstore.StoreDeps{Workspace: rtworkspace.Static(root)})
	if err != nil {
		t.Fatal(err)
	}
	cwd := s.(agentkit.SessionCwdStore)
	rt := s.(agentkit.SessionRuntimeStore)
	if err := cwd.SetCwdBind(ctx, "memory", "/project"); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "memory", "runtime.json")
	if err := os.WriteFile(path, []byte("invalid external contents"), 0600); err != nil {
		t.Fatal(err)
	}
	if got, err := cwd.CwdBind(ctx, "memory"); err != nil || got != "/project" {
		t.Fatalf("read did not use memory: %q %v", got, err)
	}
	if err := rt.SetModelBind(ctx, "memory", "model"); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "/project") || !strings.Contains(string(raw), "model") {
		t.Fatalf("incomplete snapshot: %s", raw)
	}
	// A failed persistence must not publish uncommitted state to readers.
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0755); err != nil {
		t.Fatal(err)
	}
	if err := cwd.SetCwdBind(ctx, "memory", "/uncommitted"); err == nil {
		t.Fatal("expected persistence failure")
	}
	if got, err := cwd.CwdBind(ctx, "memory"); err != nil || got != "/project" {
		t.Fatalf("failed update became visible: %q %v", got, err)
	}
}

// A snapshot primed before another writer persisted a binding must not replay
// its stale (empty) copy on the next unrelated write: that used to delete
// runtime.json and silently drop the cwd binding.
func TestFileRuntimeWriteMergesOnDiskState(t *testing.T) {
	for _, twoPaths := range []bool{false, true} {
		name := "single-snapshot"
		if twoPaths {
			// Two workspace front ends resolving one directory with
			// different spellings own two snapshots of the same file.
			name = "two-spellings"
		}
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			var a, b agentkit.SessionStore
			var path string
			if twoPaths {
				base, err := os.MkdirTemp("/tmp", "merge")
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { os.RemoveAll(base) })
				plain := rtworkspace.Static(base)
				aliased := symlinkResolved{plain}
				open := func(ws workspace.Service) agentkit.SessionStore {
					s, e := sessstore.NewStore(sessstore.StoreConfig{Dir: "sessions"}, sessstore.StoreDeps{Workspace: ws})
					if e != nil {
						t.Fatal(e)
					}
					return s
				}
				a, b = open(plain), open(aliased)
				dir, _ := plain.Resolve(ctx, "sessions")
				if real, err := filepath.EvalSymlinks(dir); err == nil {
					dir = real
				}
				path = filepath.Join(dir, "session", "runtime.json")
			} else {
				root := t.TempDir()
				open := func() agentkit.SessionStore {
					s, e := sessstore.NewStore(sessstore.StoreConfig{Dir: "sessions"}, sessstore.StoreDeps{Workspace: rtworkspace.Static(root)})
					if e != nil {
						t.Fatal(e)
					}
					return s
				}
				a, b = open(), open()
				path = filepath.Join(root, "sessions", "session", "runtime.json")
			}

			// b observes the session first: an empty snapshot.
			if _, err := b.(agentkit.SessionRuntimeStore).AgentBind(ctx, "session"); err != nil {
				t.Fatal(err)
			}
			if err := a.(agentkit.SessionCwdStore).SetCwdBind(ctx, "session", "/project"); err != nil {
				t.Fatal(err)
			}
			// An unrelated write from the stale snapshot (session/agent-guard
			// clears a denied agent bind exactly like this).
			if err := b.(agentkit.SessionRuntimeStore).SetAgentBind(ctx, "session", ""); err != nil {
				t.Fatal(err)
			}
			if got, err := a.(agentkit.SessionCwdStore).CwdBind(ctx, "session"); err != nil || got != "/project" {
				t.Fatalf("cwd binding lost after unrelated write: %q %v", got, err)
			}
			raw, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("runtime.json removed: %v", err)
			}
			if !strings.Contains(string(raw), "/project") {
				t.Fatalf("runtime.json lost cwd: %s", raw)
			}
		})
	}
}

// symlinkResolved resolves every absolute path, modelling a host where the same
// directory has two spellings (/tmp vs /private/tmp, a symlinked tenant root).
type symlinkResolved struct{ inner workspace.Service }

func (s symlinkResolved) Resolve(ctx context.Context, rel string) (string, error) {
	path, err := s.inner.Resolve(ctx, rel)
	if err != nil {
		return "", err
	}
	if real, err := filepath.EvalSymlinks(path); err == nil {
		return real, nil
	}
	// The leaf may not exist yet; resolve the parent so the two front ends
	// still disagree on the spelling of the same directory.
	if dir, err := filepath.EvalSymlinks(filepath.Dir(path)); err == nil {
		return filepath.Join(dir, filepath.Base(path)), nil
	}
	return path, nil
}
