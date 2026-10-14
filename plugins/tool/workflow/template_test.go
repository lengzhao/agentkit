package workflow

import (
	"encoding/json"
	"os"
	"path/filepath"
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

func TestRenderArgsPromotesJSONObjectWithoutFloat64(t *testing.T) {
	t.Parallel()
	const big = "9007199254740993"
	data := map[string]any{
		"steps": map[string]any{
			"gen": map[string]any{
				"json": JSON{V: map[string]any{
					"count": json.Number("42"),
					"id":    json.Number(big),
					"ratio": json.Number("1.5"),
				}},
			},
		},
	}
	args, err := renderArgs(map[string]any{
		"datasetKey":      "dailyData",
		"note":            "keep-string",
		"code":            "42",
		"expectedVersion": `{{ dig .steps.gen.json "count" }}`,
		"fromFloat":       float64(7),
		"payload":         "{{ .steps.gen.json }}",
	}, data)
	if err != nil {
		t.Fatal(err)
	}
	payload, ok := args["payload"].(map[string]any)
	if !ok {
		t.Fatalf("payload type = %T, want object", args["payload"])
	}
	if payload["count"] != json.Number("42") || payload["id"] != json.Number(big) || payload["ratio"] != json.Number("1.5") {
		t.Fatalf("payload numbers = %#v", payload)
	}
	for _, key := range []string{"count", "id", "ratio"} {
		if _, isFloat := payload[key].(float64); isFloat {
			t.Fatalf("payload.%s is float64", key)
		}
	}
	if args["note"] != "keep-string" || args["code"] != "42" {
		t.Fatalf("strings = note %v code %v", args["note"], args["code"])
	}
	if args["expectedVersion"] != json.Number("42") {
		t.Fatalf("expectedVersion = %#v", args["expectedVersion"])
	}
	if args["fromFloat"] != json.Number("7") {
		t.Fatalf("fromFloat = %#v", args["fromFloat"])
	}
	raw, err := json.Marshal(args)
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	if strings.Contains(text, `"payload":"`) {
		t.Fatalf("payload encoded as string: %s", text)
	}
	if !strings.Contains(text, big) {
		t.Fatalf("integer literal lost: %s", text)
	}
}

func TestRenderArgsReadJSONFile(t *testing.T) {
	t.Parallel()
	runDir := t.TempDir()
	const big = "9007199254740993"
	body := `{"count":42,"id":` + big + `}`
	if err := os.WriteFile(filepath.Join(runDir, "payload.json"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	data := map[string]any{"workflow": map[string]any{"runDir": runDir, "dir": runDir}}
	args, err := renderArgs(map[string]any{
		"payload": `{{ readJSON (printf "%s/payload.json" .workflow.runDir) }}`,
	}, data)
	if err != nil {
		t.Fatal(err)
	}
	payload, ok := args["payload"].(map[string]any)
	if !ok {
		t.Fatalf("payload type = %T", args["payload"])
	}
	if payload["id"] != json.Number(big) || payload["count"] != json.Number("42") {
		t.Fatalf("payload = %#v", payload)
	}
}

func TestRenderArgsReadJSONRejectsOutsidePath(t *testing.T) {
	t.Parallel()
	runDir := t.TempDir()
	outside := t.TempDir()
	secret := filepath.Join(outside, "secret.json")
	if err := os.WriteFile(secret, []byte(`{"a":1}`), 0o644); err != nil {
		t.Fatal(err)
	}
	data := map[string]any{"workflow": map[string]any{"runDir": runDir, "dir": runDir}}
	_, err := renderArgs(map[string]any{
		"payload": `{{ readJSON "` + secret + `" }}`,
	}, data)
	if err == nil || !strings.Contains(err.Error(), "outside") {
		t.Fatalf("expected outside-directory error, got %v", err)
	}
}
