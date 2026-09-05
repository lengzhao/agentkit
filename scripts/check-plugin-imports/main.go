package main

import (
	"fmt"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
)

const modulePath = "github.com/lengzhao/agentkit"

func main() {
	root, err := repoRoot()
	if err != nil {
		exit(err)
	}
	violations, err := checkPluginImports(root)
	if err != nil {
		exit(err)
	}
	if len(violations) == 0 {
		fmt.Fprintf(os.Stderr, "check-plugin-imports: ok\n")
		return
	}
	fmt.Fprintf(os.Stderr, "check-plugin-imports: found %d cross-plugin import(s):\n", len(violations))
	for _, v := range violations {
		fmt.Fprintf(os.Stderr, "  %s:%d imports %s\n", v.File, v.Line, v.Import)
	}
	fmt.Fprintf(os.Stderr, "\nplugins/* must not import other plugin packages; use cap/*, runtime/*, or agentkit root types instead.\n")
	os.Exit(1)
}

type violation struct {
	File   string
	Line   int
	Import string
}

func repoRoot() (string, error) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		return "", fmt.Errorf("resolve caller path")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..")), nil
}

func checkPluginImports(root string) ([]violation, error) {
	pluginsRoot := filepath.Join(root, "plugins")
	var out []violation
	err := filepath.WalkDir(pluginsRoot, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if rel == "plugins/all.go" {
			return nil
		}
		pkg, err := pluginPackage(rel)
		if err != nil {
			return err
		}
		imports, err := parsePluginImports(path)
		if err != nil {
			return err
		}
		for _, imp := range imports {
			if samePluginTree(pkg, imp.Path) {
				continue
			}
			out = append(out, violation{
				File:   rel,
				Line:   imp.Line,
				Import: modulePath + "/plugins/" + imp.Path,
			})
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].File != out[j].File {
			return out[i].File < out[j].File
		}
		if out[i].Line != out[j].Line {
			return out[i].Line < out[j].Line
		}
		return out[i].Import < out[j].Import
	})
	return out, nil
}

type pluginImport struct {
	Path string
	Line int
}

func pluginPackage(fileRel string) (string, error) {
	if !strings.HasPrefix(fileRel, "plugins/") {
		return "", fmt.Errorf("not under plugins/: %s", fileRel)
	}
	rest := strings.TrimPrefix(fileRel, "plugins/")
	if rest == "" {
		return "", fmt.Errorf("invalid plugins path: %s", fileRel)
	}
	dir := filepath.ToSlash(filepath.Dir(rest))
	if dir == "." {
		return "", fmt.Errorf("plugins root file not expected: %s", fileRel)
	}
	return dir, nil
}

func samePluginTree(pkg, imp string) bool {
	if imp == pkg {
		return true
	}
	if strings.HasPrefix(imp, pkg+"/") {
		return true
	}
	if strings.HasPrefix(pkg, imp+"/") {
		return true
	}
	return false
}

func parsePluginImports(path string) ([]pluginImport, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	prefix := modulePath + "/plugins/"
	var out []pluginImport
	for _, spec := range f.Imports {
		raw := strings.Trim(spec.Path.Value, `"`)
		if !strings.HasPrefix(raw, prefix) {
			continue
		}
		impPath := strings.TrimPrefix(raw, prefix)
		pos := fset.Position(spec.Path.ValuePos)
		out = append(out, pluginImport{Path: impPath, Line: pos.Line})
	}
	return out, nil
}

func exit(err error) {
	fmt.Fprintf(os.Stderr, "check-plugin-imports: %v\n", err)
	os.Exit(1)
}
