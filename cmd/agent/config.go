package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/lengzhao/agentkit/config"
)

func runConfig(args []string) {
	if len(args) == 0 {
		printConfigUsage()
		os.Exit(2)
	}

	switch args[0] {
	case "dump":
		runConfigDump(args[1:])
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

func printConfigUsage() {
	fmt.Fprintf(os.Stderr, `usage:
  agent config dump [-base path] [-config path] [-redact] [-no-redact] [-o file]

Print the fully resolved config graph (L0 + overlays, extends, prune, interpolation).

Examples:
  agent config dump
  agent config dump -config presets/coding.yaml
  agent config dump -config presets/feishu.yaml,config.yaml -o resolved.yaml
  agent config dump -no-redact   # include interpolated secrets (use with care)
`)
}
