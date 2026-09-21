package skill_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/lengzhao/agentkit/cap/filesystem"
	"github.com/lengzhao/agentkit/cap/skill"
	skillplugin "github.com/lengzhao/agentkit/plugins/skill"
	rtfilesystem "github.com/lengzhao/agentkit/runtime/filesystem"
	rtworkspace "github.com/lengzhao/agentkit/runtime/workspace"
)

func testFS(t *testing.T, dir string) filesystem.Service {
	t.Helper()
	fs, err := rtfilesystem.New(rtfilesystem.Config{Root: "."}, rtfilesystem.Deps{Workspace: rtworkspace.Static(dir)})
	if err != nil {
		t.Fatal(err)
	}
	return fs
}

func TestFilesystemRegistryDiscoversBundleSkills(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	bundleDir := filepath.Join(root, "bundle-skill")
	if err := os.MkdirAll(bundleDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeSkill(t, filepath.Join(bundleDir, "SKILL.md"), "---\nname: bundle-skill\ndescription: bundle description\nlicense: MIT\n---\n\nBundle body.\n")
	writeSkill(t, filepath.Join(root, "flat-skill.md"), "---\nname: flat-skill\ndescription: flat description\n---\n\nFlat body.\n")
	writeSkill(t, filepath.Join(root, "bad.md"), "---\nname: Bad_Name\ndescription: bad\n---\n\nbad\n")
	mismatchDir := filepath.Join(root, "wrong-dir")
	if err := os.MkdirAll(mismatchDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeSkill(t, filepath.Join(mismatchDir, "SKILL.md"), "---\nname: other-name\ndescription: mismatch\n---\n\nBody.\n")

	reg, err := skillplugin.New(skillplugin.Config{
		Dirs: []string{"."},
	}, skillplugin.Deps{
		FS: testFS(t, root),
	})
	if err != nil {
		t.Fatal(err)
	}
	list, err := reg.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 {
		t.Fatalf("got %d descriptors: %#v", len(list), list)
	}
	if list[0].Name != "bundle-skill" {
		t.Fatalf("name = %q", list[0].Name)
	}
	if list[0].License != "MIT" {
		t.Fatalf("license = %q", list[0].License)
	}

	content, err := reg.Load(context.Background(), "bundle-skill")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if content.Body != "Bundle body." {
		t.Fatalf("body = %q", content.Body)
	}
	if content.Path != "./bundle-skill" {
		t.Fatalf("path = %q", content.Path)
	}
}

func writeSkill(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

var _ skill.Registry = (*skillplugin.Registry)(nil)
