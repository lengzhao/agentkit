// gen-config-catalog 从 pluginkit 注册表生成 docs/config-catalog.zh.md：
// 每个 kind 的 config 字段（类型/可选/默认值/字段注释）、deps 扩展点与源码链接。
// 默认值来自 Config 的 SetDefaults（pluginkit.Defaulter），字段注释来自源码 AST。
//
// 用法：
//
//	go run ./scripts/gen-config-catalog           # 重新生成文档
//	go run ./scripts/gen-config-catalog -check    # CI：文档过期则退出非零
package main

import (
	"bytes"
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"strings"

	_ "github.com/lengzhao/agentkit/plugins"
	"github.com/lengzhao/pluginkit"
)

const (
	modulePath = "github.com/lengzhao/agentkit"
	outRel     = "docs/config-catalog.zh.md"
)

func main() {
	check := flag.Bool("check", false, "verify the committed catalog is up to date")
	flag.Parse()

	root, err := repoRoot()
	if err != nil {
		exit(err)
	}
	doc, err := generate(root)
	if err != nil {
		exit(err)
	}
	outPath := filepath.Join(root, outRel)

	if *check {
		existing, err := os.ReadFile(outPath)
		if err != nil {
			exit(fmt.Errorf("read %s: %w", outRel, err))
		}
		if !bytes.Equal(existing, doc) {
			exit(fmt.Errorf("%s is stale; run `go run ./scripts/gen-config-catalog`", outRel))
		}
		fmt.Printf("%s is up to date\n", outRel)
		return
	}
	if err := os.WriteFile(outPath, doc, 0o644); err != nil {
		exit(err)
	}
	fmt.Printf("wrote %s\n", outRel)
}

func generate(root string) ([]byte, error) {
	pkgs := newPackageIndex(root)
	kinds := pluginkit.ListKinds()
	sort.Strings(kinds)

	var b bytes.Buffer
	b.WriteString(`<!-- 由 scripts/gen-config-catalog 生成——请勿手工编辑。
     运行 ` + "`go run ./scripts/gen-config-catalog`" + ` 重新生成；CI 以 -check 校验新鲜度。 -->

# 插件配置目录

每个 kind 对应 ` + "`use: <kind>`" + ` 可引用的插件类型。config 表列出字段名、类型、默认值
（来自插件 Config 的 SetDefaults）与字段注释；deps 表列出依赖注入点。字段级校验规则
（Validate）在装配期生效，可用 ` + "`agent config validate`" + ` 在不构造实例的情况下检查配置。

`)

	for _, kind := range kinds {
		spec, ok := pluginkit.Lookup(kind)
		if !ok {
			continue
		}
		desc, _ := pluginkit.Describe(kind)
		writeKind(&b, pkgs, spec, desc)
	}
	return b.Bytes(), nil
}

func writeKind(b *bytes.Buffer, pkgs *packageIndex, spec pluginkit.Spec, desc pluginkit.PluginDescription) {
	fmt.Fprintf(b, "## `%s`\n\n", spec.Kind)
	fmt.Fprintf(b, "- 返回类型：`%s`\n", formatType(spec.ReturnType))
	if src := pkgs.sourceRef(deref(spec.ConfigType)); src != "" {
		// 文档位于 docs/，链接需回到仓库根。
		fmt.Fprintf(b, "- 源码：[`%s`](../%s)\n", src, src)
	}
	b.WriteString("\n")

	defaults := desc.ConfigDefaults()
	if len(desc.Config) == 0 {
		b.WriteString("无 config 字段。\n\n")
	} else {
		b.WriteString("| config 字段 | 类型 | 默认值 | 说明 |\n|---|---|---|---|\n")
		comments := pkgs.fieldComments(deref(spec.ConfigType))
		for _, f := range desc.Config {
			fmt.Fprintf(b, "| `%s` | `%s` | %s | %s |\n",
				f.Name, fieldType(f), formatDefault(defaults[f.Name]), escapeCell(comments[f.GoName]))
		}
		b.WriteString("\n")
	}

	if len(desc.Extensions) > 0 {
		b.WriteString("| deps 字段 | 类型 | 说明 |\n|---|---|---|\n")
		comments := pkgs.fieldComments(deref(spec.DepsType))
		for _, f := range desc.Extensions {
			name := "`" + f.Name + "`"
			if !f.Optional {
				name += "（必填）"
			}
			fmt.Fprintf(b, "| %s | `%s` | %s |\n", name, fieldType(f), escapeCell(comments[f.GoName]))
		}
		b.WriteString("\n")
	}
}

// packageIndex 按包缓存 AST 解析结果，提供字段注释与源码位置。
type packageIndex struct {
	root      string
	fset      *token.FileSet
	parsed    map[string]map[string]*ast.StructType // pkgPath -> typeName -> struct
	typeFiles map[string]map[string]string          // pkgPath -> typeName -> 源文件相对路径
	files     map[string]string                     // pkgPath -> 首个源文件相对路径（type 未命中时回退）
}

func newPackageIndex(root string) *packageIndex {
	return &packageIndex{
		root:      root,
		fset:      token.NewFileSet(),
		parsed:    map[string]map[string]*ast.StructType{},
		typeFiles: map[string]map[string]string{},
		files:     map[string]string{},
	}
}

func (p *packageIndex) parsePkg(pkgPath string) map[string]*ast.StructType {
	if got, ok := p.parsed[pkgPath]; ok {
		return got
	}
	types := map[string]*ast.StructType{}
	p.parsed[pkgPath] = types
	if !strings.HasPrefix(pkgPath, modulePath) {
		return types
	}
	dir := filepath.Join(p.root, strings.TrimPrefix(pkgPath, modulePath+"/"))
	entries, err := os.ReadDir(dir)
	if err != nil {
		return types
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		full := filepath.Join(dir, name)
		f, err := parser.ParseFile(p.fset, full, nil, parser.ParseComments)
		if err != nil {
			continue
		}
		rel, _ := filepath.Rel(p.root, full)
		if _, recorded := p.files[pkgPath]; !recorded {
			p.files[pkgPath] = filepath.ToSlash(rel)
		}
		for _, decl := range f.Decls {
			gd, ok := decl.(*ast.GenDecl)
			if !ok || gd.Tok != token.TYPE {
				continue
			}
			for _, sp := range gd.Specs {
				ts, ok := sp.(*ast.TypeSpec)
				if !ok {
					continue
				}
				if st, ok := ts.Type.(*ast.StructType); ok {
					name := ts.Name.Name
					types[name] = st
					if p.typeFiles[pkgPath] == nil {
						p.typeFiles[pkgPath] = map[string]string{}
					}
					p.typeFiles[pkgPath][name] = filepath.ToSlash(rel)
				}
			}
		}
	}
	return types
}

// fieldComments 返回 struct 字段 Go 名 → 字段注释（前导 + 行尾）。
func (p *packageIndex) fieldComments(typ reflect.Type) map[string]string {
	out := map[string]string{}
	if typ == nil || typ.Kind() != reflect.Struct {
		return out
	}
	st := p.parsePkg(typ.PkgPath())[typ.Name()]
	if st == nil {
		return out
	}
	for _, field := range st.Fields.List {
		if len(field.Names) == 0 {
			continue
		}
		var parts []string
		if field.Doc != nil {
			parts = append(parts, strings.TrimSpace(field.Doc.Text()))
		}
		if field.Comment != nil {
			parts = append(parts, strings.TrimSpace(field.Comment.Text()))
		}
		out[field.Names[0].Name] = strings.Join(parts, " ")
	}
	return out
}

// sourceRef 返回类型声明所在文件的仓库相对路径（不含行号，行号随生成漂移）。
func (p *packageIndex) sourceRef(typ reflect.Type) string {
	if typ == nil {
		return ""
	}
	pkgPath := typ.PkgPath()
	p.parsePkg(pkgPath)
	if byType := p.typeFiles[pkgPath]; byType != nil {
		if src := byType[typ.Name()]; src != "" {
			return src
		}
	}
	return p.files[pkgPath]
}

func fieldType(f pluginkit.FieldDescription) string {
	if f.List {
		return "[]" + formatType(f.Type)
	}
	return formatType(f.Type)
}

func formatType(t reflect.Type) string {
	if t == nil {
		return "-"
	}
	return t.String()
}

func formatDefault(v any) string {
	if v == nil {
		return "—"
	}
	rv := reflect.ValueOf(v)
	if rv.IsZero() {
		return "—"
	}
	if s, ok := v.(string); ok {
		return "`" + s + "`"
	}
	return fmt.Sprintf("`%v`", v)
}

func escapeCell(s string) string {
	s = strings.ReplaceAll(s, "|", "\\|")
	s = strings.ReplaceAll(s, "\n", " ")
	return s
}

func deref(t reflect.Type) reflect.Type {
	for t != nil && t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	return t
}

func repoRoot() (string, error) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		return "", fmt.Errorf("resolve caller path")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..")), nil
}

func exit(err error) {
	fmt.Fprintln(os.Stderr, "error:", err)
	os.Exit(1)
}
