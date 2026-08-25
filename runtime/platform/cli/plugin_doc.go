package cli

import (
	"bytes"
	"fmt"
	"os/exec"
	"reflect"
	"runtime"
	"strings"

	"github.com/lengzhao/pluginkit"
)

func formatPluginList() string {
	kinds := pluginkit.ListKinds()
	width := 0
	for _, kind := range kinds {
		width = max(width, len(kind))
	}
	var b strings.Builder
	b.WriteString("Registered plugin kinds:\n")
	for _, kind := range kinds {
		fmt.Fprintf(&b, "  %-*s\n", width, kind)
	}
	b.WriteString("\nUse /help plugin <kind> to view godoc.")
	return b.String()
}

func pluginDoc(kind string) (string, error) {
	kind = strings.TrimSpace(kind)
	if kind == "" {
		return "", fmt.Errorf("plugin kind is required")
	}
	spec, ok := pluginkit.Lookup(kind)
	if !ok {
		return "", fmt.Errorf("unknown plugin kind %q (try /help plugin -l)", kind)
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
