package main

import (
	"flag"
	"fmt"
	"os"
	"reflect"
	"sort"
	"strings"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/config"
	"github.com/lengzhao/pluginkit"
	"github.com/lengzhao/pluginkit/build"
	"gopkg.in/yaml.v3"
)

func runConfig(args []string) {
	if len(args) == 0 {
		printConfigUsage()
		os.Exit(2)
	}

	switch args[0] {
	case "dump":
		runConfigDump(args[1:])
	case "describe":
		runConfigDescribe(args[1:])
	case "scaffold":
		runConfigScaffold(args[1:])
	case "validate":
		runConfigValidate(args[1:])
	default:
		fmt.Fprintf(os.Stderr, "unknown config command %q\n\n", args[0])
		printConfigUsage()
		os.Exit(2)
	}
}

func runConfigDump(args []string) {
	fs := flag.NewFlagSet("config dump", flag.ExitOnError)
	basePath := fs.String("base", config.DefaultBasePath, "L0 base config YAML path")
	overlayPath := fs.String("config", config.DefaultOverlayPath, "L1 override YAML path(s), comma-separated; later files win")
	redact := fs.Bool("redact", true, "redact interpolated secrets (credential refs like env:VAR are kept)")
	noRedact := fs.Bool("no-redact", false, "print resolved values without redaction")
	out := fs.String("o", "", "write YAML to file (default stdout)")
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "usage: agent config dump [-base path] [-config path] [-redact] [-no-redact] [-o file]\n")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		os.Exit(2)
	}

	opts := config.DumpOptions{Redact: *redact && !*noRedact}
	raw, err := config.DumpResolvedYAML(*basePath, config.SplitOverlayPaths(*overlayPath), opts)
	if err != nil {
		fatal("dump config", err)
	}

	if *out == "" {
		os.Stdout.Write(raw)
		return
	}
	if err := os.WriteFile(*out, raw, 0o644); err != nil {
		fatal("write dump output", err)
	}
	fmt.Printf("wrote %s\n", *out)
}

// runConfigDescribe 打印指定 kind 的配置字段、默认值与 deps 扩展点。
func runConfigDescribe(args []string) {
	fs := flag.NewFlagSet("config describe", flag.ExitOnError)
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "usage: agent config describe <kind>\n")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		os.Exit(2)
	}
	if fs.NArg() != 1 {
		fs.Usage()
		os.Exit(2)
	}

	kind := fs.Arg(0)
	desc, ok := pluginkit.Describe(kind)
	if !ok {
		fmt.Fprintf(os.Stderr, "error: kind %q not registered\n\nregistered kinds:\n", kind)
		for _, k := range registeredKinds() {
			fmt.Fprintf(os.Stderr, "  %s\n", k)
		}
		os.Exit(1)
	}

	defaults := desc.ConfigDefaults()
	fmt.Printf("kind: %s\n", desc.Kind)
	fmt.Printf("returns: %s\n", desc.ReturnType)
	if len(desc.Config) > 0 {
		fmt.Println("config:")
		for _, f := range desc.Config {
			fmt.Printf("  %s\n", formatField(f, defaults[f.Name]))
		}
	}
	if len(desc.Extensions) > 0 {
		fmt.Println("deps:")
		for _, f := range desc.Extensions {
			fmt.Printf("  %s\n", formatField(f, nil))
		}
	}
}

func formatField(f pluginkit.FieldDescription, def any) string {
	var b strings.Builder
	b.WriteString(f.Name)
	if f.List {
		b.WriteString(" []")
	}
	fmt.Fprintf(&b, " (%s)", f.Type)
	if f.Optional {
		b.WriteString(" optional")
	}
	if def != nil && !reflect.ValueOf(def).IsZero() {
		fmt.Fprintf(&b, " default: %v", def)
	}
	return b.String()
}

func registeredKinds() []string {
	kinds := pluginkit.ListKinds()
	sort.Strings(kinds)
	return kinds
}

// runConfigScaffold 输出指定 kind 的可用配置骨架（含 SetDefaults 真实默认值）。
func runConfigScaffold(args []string) {
	fs := flag.NewFlagSet("config scaffold", flag.ExitOnError)
	out := fs.String("o", "", "write YAML to file (default stdout)")
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "usage: agent config scaffold <kind> [-o file]\n")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		os.Exit(2)
	}
	if fs.NArg() != 1 {
		fs.Usage()
		os.Exit(2)
	}

	kind := fs.Arg(0)
	desc, ok := pluginkit.Describe(kind)
	if !ok {
		fmt.Fprintf(os.Stderr, "error: kind %q not registered\n", kind)
		os.Exit(1)
	}
	raw, err := yaml.Marshal(map[string]any{kind + ".default": desc.Template()})
	if err != nil {
		fatal("scaffold config", err)
	}
	if *out == "" {
		os.Stdout.Write(raw)
		return
	}
	if err := os.WriteFile(*out, raw, 0o644); err != nil {
		fatal("write scaffold output", err)
	}
	fmt.Printf("wrote %s\n", *out)
}

// runConfigValidate 加载 L0+L1 后执行 resolve/typecheck/deps 规划与
// decode → SetDefaults → Validate 管线，不构造插件实例；错误聚合输出。
func runConfigValidate(args []string) {
	fs := flag.NewFlagSet("config validate", flag.ExitOnError)
	basePath := fs.String("base", config.DefaultBasePath, "L0 base config YAML path")
	overlayPath := fs.String("config", config.DefaultOverlayPath, "L1 override YAML path(s), comma-separated; later files win")
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "usage: agent config validate [-base path] [-config path]\n")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		os.Exit(2)
	}

	doc, err := config.LoadDocument(*basePath, config.SplitOverlayPaths(*overlayPath)...)
	if err != nil {
		fatal("load config", err)
	}
	want := reflect.TypeOf((*agentkit.Runner)(nil)).Elem()
	if err := build.ValidatePlan(doc.ToGraph(), doc.RootID, want); err != nil {
		fmt.Fprintf(os.Stderr, "config invalid:\n%v\n", err)
		os.Exit(1)
	}
	fmt.Println("config valid")
}

func printConfigUsage() {
	fmt.Fprintf(os.Stderr, `usage:
  agent config dump [-base path] [-config path] [-redact] [-no-redact] [-o file]
  agent config describe <kind>
  agent config scaffold <kind> [-o file]
  agent config validate [-base path] [-config path]

Commands:
  dump      Print the fully resolved config graph (L0 + overlays, extends, prune, interpolation).
  describe  Show a plugin kind's config fields, defaults, and deps extension points.
  scaffold  Print a fill-in YAML skeleton for a plugin kind (defaults from SetDefaults).
  validate  Check the config graph without constructing plugin instances.

Examples:
  agent config dump
  agent config dump -config presets/coding.yaml
  agent config dump -config presets/feishu.yaml,config.yaml -o resolved.yaml
  agent config dump -no-redact   # include interpolated secrets (use with care)
  agent config describe llm/openai
  agent config scaffold platform/feishu
  agent config validate -config config.yaml
`)
}
