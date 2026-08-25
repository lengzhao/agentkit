package plugindoc_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/lengzhao/agentkit/plugindoc"
	_ "github.com/lengzhao/agentkit/plugins"
	"github.com/lengzhao/pluginkit"
)

// TestEveryKindIsDocumented is the reason `/help plugin -l` can be trusted: a new
// pluginkit.Register with no matching plugindoc.Register fails here instead of
// quietly printing "(undocumented)".
func TestEveryKindIsDocumented(t *testing.T) {
	t.Parallel()
	for _, kind := range plugindoc.Kinds() {
		doc, ok := plugindoc.Lookup(kind)
		if !ok {
			t.Errorf("kind %q has no plugindoc.Register call; add one to the package that registers it", kind)
			continue
		}
		if strings.TrimSpace(doc.Summary) == "" {
			t.Errorf("kind %q has a doc with an empty Summary", kind)
		}
	}
}

// TestDocsMatchRegisteredKinds catches the opposite drift: a doc left behind
// after its kind was renamed or removed.
func TestDocsMatchRegisteredKinds(t *testing.T) {
	t.Parallel()
	kinds := plugindoc.Kinds()
	for _, kind := range plugindoc.Documented() {
		if !slices.Contains(kinds, kind) {
			t.Errorf("doc registered for %q but no such plugin kind exists", kind)
		}
	}
}

// TestConfigNotesMatchSchema keeps the prose anchored to the struct. Renaming a
// config field without touching its note fails here, which is what stopped
// shell/bash from documenting a `timeout` key its Config never had.
func TestConfigNotesMatchSchema(t *testing.T) {
	t.Parallel()
	for _, kind := range plugindoc.Documented() {
		doc, _ := plugindoc.Lookup(kind)
		if len(doc.ConfigNotes) == 0 {
			continue
		}
		spec, ok := pluginkit.Lookup(kind)
		if !ok {
			continue // reported by TestDocsMatchRegisteredKinds
		}
		fields := plugindoc.ConfigFieldNames(spec.ConfigType)
		for key := range doc.ConfigNotes {
			if !slices.Contains(fields, key) {
				t.Errorf("%s: ConfigNotes key %q is not a config field; have %v", kind, key, fields)
			}
		}
	}
}

func TestFormatKindRendersReflectedFields(t *testing.T) {
	t.Parallel()
	text, err := plugindoc.FormatKind("tool/grep")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"# tool/grep",
		"## Config",
		"## Deps",
		"- maxMatches (int)",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("missing %q in:\n%s", want, text)
		}
	}
}

func TestFormatKindNestedStruct(t *testing.T) {
	t.Parallel()
	text, err := plugindoc.FormatKind("agent/coding")
	if err != nil {
		t.Fatal(err)
	}
	// budget is a nested struct of ours, so its fields must be expanded one
	// indent deeper rather than printed as an opaque type name.
	if !strings.Contains(text, "\n- budget (") {
		t.Fatalf("budget not rendered:\n%s", text)
	}
	if !strings.Contains(text, "\n  - maxContinuations (int)") {
		t.Fatalf("nested budget fields not expanded:\n%s", text)
	}
}

func TestFormatKindUnknown(t *testing.T) {
	t.Parallel()
	if _, err := plugindoc.FormatKind("no/such-kind"); err == nil {
		t.Fatal("expected an error for an unknown kind")
	}
	if _, err := plugindoc.FormatKind("  "); err == nil {
		t.Fatal("expected an error for an empty kind")
	}
}

func TestFormatListCoversEveryKind(t *testing.T) {
	t.Parallel()
	out := plugindoc.FormatList()
	for _, kind := range plugindoc.Kinds() {
		if !strings.Contains(out, kind) {
			t.Errorf("kind %q missing from the list output", kind)
		}
	}
	if strings.Contains(out, "(undocumented)") {
		t.Error("some kind rendered as (undocumented); TestEveryKindIsDocumented says which")
	}
}
