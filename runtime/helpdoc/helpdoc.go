// Package helpdoc resolves pluginkit kind documentation for slash commands.
package helpdoc

import (
	"bytes"
	"fmt"
	"os/exec"
	"reflect"
	"runtime"
	"strings"

	"github.com/lengzhao/pluginkit"
)

const (
	AgentKindPrefix    = "agent/"
	SubagentKindPrefix = "subagent/"
)

func kindsWithPrefix(prefix string) []string {
	var out []string
	for _, kind := range pluginkit.ListKinds() {
		if prefix == "" || strings.HasPrefix(kind, prefix) {
			out = append(out, kind)
		}
	}
	return out
}

func FormatKindList(title, prefix, command, nameHint string) string {
	kinds := kindsWithPrefix(prefix)
	width := 0
	for _, kind := range kinds {
		width = max(width, len(kind))
	}
	var b strings.Builder
	b.WriteString(title)
	b.WriteString(":\n")
	for _, kind := range kinds {
		fmt.Fprintf(&b, "  %-*s\n", width, kind)
	}
	b.WriteString(fmt.Sprintf("\nUse /%s %s for details.", command, nameHint))
	return b.String()
}

func resolveKind(prefix, name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	if strings.Contains(name, "/") {
		return name
	}
	if prefix != "" {
		return prefix + name
	}
	return name
}

func KindDoc(prefix, name string) (string, error) {
	kind := resolveKind(prefix, name)
	if kind == "" {
		return "", fmt.Errorf("kind is required")
	}
	if prefix != "" && !strings.HasPrefix(kind, prefix) {
		label := strings.TrimSuffix(prefix, "/")
		return "", fmt.Errorf("unknown %s kind %q (try /%s)", label, kind, label)
	}
	spec, ok := pluginkit.Lookup(kind)
	if !ok {
		label := strings.TrimSuffix(prefix, "/")
		if label == "" {
			label = "plugin"
		}
		return "", fmt.Errorf("unknown %s kind %q (try /%s)", label, kind, label)
	}
	symbol := docSymbol(spec)
	if symbol == "" {
		return "", fmt.Errorf("no doc target for kind %q", kind)
	}
	out, err := runGoDoc(symbol)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("go doc %s\n\n%s", symbol, out), nil
}

func docSymbol(spec pluginkit.Spec) string {
	if symbol := constructorSymbol(spec.Constructor()); symbol != "" {
		return symbol
	}
	if t := derefType(spec.ConfigType); t != nil {
		return t.PkgPath() + "." + t.Name()
	}
	if t := derefType(spec.DepsType); t != nil {
		return t.PkgPath() + "." + t.Name()
	}
	return ""
}

func constructorSymbol(constructor any) string {
	v := reflect.ValueOf(constructor)
	if !v.IsValid() || v.Kind() != reflect.Func {
		return ""
	}
	fn := runtime.FuncForPC(v.Pointer())
	if fn == nil {
		return ""
	}
	return fn.Name()
}

func runGoDoc(symbol string) (string, error) {
	cmd := exec.Command("go", "doc", symbol)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return "", fmt.Errorf("go doc %s: %s", symbol, msg)
	}
	return strings.TrimRight(stdout.String(), "\n"), nil
}

func derefType(t reflect.Type) reflect.Type {
	for t != nil && t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	return t
}
