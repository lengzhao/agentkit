package workflow

import (
	"strings"
	"testing"
)

func TestRenderTemplateFuncs(t *testing.T) {
	t.Parallel()
	data := map[string]any{
		"inputs": map[string]any{"date": "2026-10-13"},
		"steps": map[string]any{
			"submit": map[string]any{
				"json": JSON{V: map[string]any{"status": "ok", "items": []any{"a", "b"}}},
			},
		},
	}
	cases := []struct {
		tmpl string
		want string
	}{
		{`{{ .inputs.date }}`, "2026-10-13"},
		{`{{ dig .steps.submit.json "status" }}`, "ok"},
		{`{{ dig .steps.submit.json "items.1" }}`, "b"},
		{`{{ join (dig .steps.submit.json "items") "," }}`, "a,b"},
		{`{{ .inputs.date | quote }}`, "'2026-10-13'"},
		{`{{ .steps.submit.json }}`, `{"items":["a","b"],"status":"ok"}`},
	}
	for _, tc := range cases {
		got, err := renderTemplate("test", tc.tmpl, data)
		if err != nil {
			t.Fatalf("%s: %v", tc.tmpl, err)
		}
		if got != tc.want {
			t.Fatalf("%s = %q, want %q", tc.tmpl, got, tc.want)
		}
	}
}

func TestRenderTemplateDigMissingKey(t *testing.T) {
	t.Parallel()
	_, err := renderTemplate("test", `{{ dig .steps.x.json "nope" }}`, map[string]any{
		"steps": map[string]any{"x": map[string]any{"json": map[string]any{}}},
	})
	if err == nil || !strings.Contains(err.Error(), "no key") {
		t.Fatalf("expected missing key error, got %v", err)
	}
}

func TestRenderArgsNested(t *testing.T) {
	t.Parallel()
	data := map[string]any{
		"steps": map[string]any{"gen": map[string]any{"path": "/tmp/ledger.json"}},
	}
	args, err := renderArgs(map[string]any{
		"path": "{{ .steps.gen.path }}",
		"meta": map[string]any{"src": "{{ .steps.gen.path }}"},
		"list": []any{"{{ .steps.gen.path }}", 1},
		"n":    42,
	}, data)
	if err != nil {
		t.Fatal(err)
	}
	if args["path"] != "/tmp/ledger.json" {
		t.Fatalf("path = %v", args["path"])
	}
	if args["meta"].(map[string]any)["src"] != "/tmp/ledger.json" {
		t.Fatalf("meta.src = %v", args["meta"])
	}
	if args["list"].([]any)[0] != "/tmp/ledger.json" || args["n"] != 42 {
		t.Fatalf("list/n = %v %v", args["list"], args["n"])
	}
}
