package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestResolveReport(t *testing.T) {
	base := []byte(`runner.default:
  use: runner
  deps: {good: good, bad: b, typo: unknown}
good: {use: good}
b:
  use: b
  deps: {a: a}
a:
  use: a
  config: {key: "${var:REPORT_TEST_MISSING}"}
unused: {use: unused}
off: {use: off}
`)
	var previous string
	for i := 0; i < 20; i++ {
		_, r, err := ResolveYAMLWithReport(".", base, [][]byte{[]byte("off: null\n")})
		if err != nil {
			t.Fatal(err)
		}
		states := map[string]string{}
		for _, s := range r.Instances {
			states[s.ID] = s.State
		}
		for id, want := range map[string]string{"a": "missing_value", "b": "dependency_cascade", "unused": "unreachable", "off": "explicit_null", "runner.default": "active"} {
			if states[id] != want {
				t.Fatalf("%s: %v", id, r)
			}
		}
		encoded, _ := json.Marshal(r)
		if !strings.Contains(string(encoded), "unknown") || !strings.Contains(string(encoded), "REPORT_TEST_MISSING") {
			t.Fatal(string(encoded))
		}
		if i > 0 && previous != string(encoded) {
			t.Fatal("nondeterministic report")
		}
		previous = string(encoded)
	}
}

func TestReportRootFailure(t *testing.T) {
	_, r, err := ResolveYAMLWithReport(".", []byte("runner.default: {use: runner, config: {key: '${var:REPORT_TEST_MISSING}'}}"), nil)
	if err == nil || len(r.Instances) != 1 || r.Instances[0].State != "missing_value" {
		t.Fatalf("%+v %v", r, err)
	}
}

func TestDeploymentIntent(t *testing.T) {
	base := []byte("runner.default: {use: runner, deps: {child: child}}\nchild: {use: child}\n")
	for _, tc := range []struct {
		name   string
		intent DeploymentIntent
		fail   bool
	}{
		{"required", DeploymentIntent{Required: []string{"child"}}, false},
		{"exclude required dependency", DeploymentIntent{Required: []string{"runner.default"}, Excluded: []string{"child"}}, true},
		{"optional excluded conflict", DeploymentIntent{Optional: []string{"child"}, Excluded: []string{"child"}}, true},
		{"unknown", DeploymentIntent{Required: []string{"typo"}}, true},
		{"duplicate", DeploymentIntent{Required: []string{"child", "child"}}, true},
		{"exclude", DeploymentIntent{Excluded: []string{"child"}}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := ResolveYAMLWithReport(".", base, nil, WithDeploymentIntent(tc.intent))
			if (err != nil) != tc.fail {
				t.Fatal(err)
			}
		})
	}
}

func TestReportCauseChainAndCompatibility(t *testing.T) {
	base := []byte(`runner.default: {use: runner, deps: {bad: b, good: good}}
good: {use: good}
b: {use: b, deps: {a: a}}
a: {use: a, config: {key: '${var:REPORT_TEST_MISSING}'}}
`)
	plain, err := ResolveYAML(".", base)
	if err != nil {
		t.Fatal(err)
	}
	out, r, err := ResolveYAMLWithReport(".", base, nil)
	if err != nil || string(out) != string(plain) {
		t.Fatalf("compatibility: %v", err)
	}
	for _, s := range r.Instances {
		if s.ID == "b" && (len(s.Causes) != 1 || s.Causes[0] != "a") {
			t.Fatalf("missing cause: %+v", s)
		}
	}
	found := false
	for _, d := range r.Dependencies {
		if d.InstanceID == "runner.default" && d.Target == "b" && d.State == "pruned" {
			found = true
		}
	}
	if !found {
		t.Fatal("surviving instance lost dependency diagnostic")
	}
}

func TestIntentOptionalAndExcluded(t *testing.T) {
	base := []byte(`runner.default: {use: runner, deps: {good: good, child: child}}
good: {use: good}
child: {use: child, config: {key: '${var:REPORT_TEST_MISSING}'}}
`)
	_, r, err := ResolveYAMLWithReport(".", base, nil, WithDeploymentIntent(DeploymentIntent{Optional: []string{"child"}}))
	if err != nil {
		t.Fatal(err)
	}
	_, r, err = ResolveYAMLWithReport(".", base, nil, WithEnvLookup(MapEnvLookup(map[string]string{"REPORT_TEST_MISSING": "secret-value"})), WithDeploymentIntent(DeploymentIntent{Excluded: []string{"child"}}))
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, s := range r.Instances {
		if s.ID == "child" && s.State == "excluded" {
			found = true
		}
	}
	if !found {
		t.Fatalf("%+v", r)
	}
	encoded, _ := json.Marshal(r)
	if strings.Contains(string(encoded), "secret-value") {
		t.Fatal("secret in report")
	}
	_, r, err = ResolveYAMLWithReport(".", []byte("runner.default: {use: runner}\nchild: {use: child}"), nil, WithDeploymentIntent(DeploymentIntent{Required: []string{"child"}}))
	if err == nil || len(r.ResolvedIDs) != 1 {
		t.Fatalf("required unreachable: %+v %v", r, err)
	}
}

func TestReportMalformedInterpolationDoesNotLeak(t *testing.T) {
	_, r, err := ResolveYAMLWithReport(".", []byte("runner.default: {use: runner, deps: {child: 'secret-prefix ${var:NAME} secret-suffix'}}"), nil)
	if err == nil {
		t.Fatal("expected unavailable root")
	}
	encoded, _ := json.Marshal(r)
	if strings.Contains(string(encoded), "secret-") || strings.Contains(err.Error(), "secret-") {
		t.Fatalf("leaked raw interpolation: %s %v", encoded, err)
	}
}

func TestResolveFilesReport(t *testing.T) {
	dir := t.TempDir()
	base := filepath.Join(dir, "base.yaml")
	if err := os.WriteFile(base, []byte("runner.default: {use: runner}\noff: {use: off}"), 0600); err != nil {
		t.Fatal(err)
	}
	overlay := filepath.Join(dir, "overlay.yaml")
	if err := os.WriteFile(overlay, []byte("off: null"), 0600); err != nil {
		t.Fatal(err)
	}
	_, r, err := ResolveFilesWithReport(base, []string{filepath.Join(dir, "absent.yaml"), overlay})
	if err != nil || len(r.MergedIDs) != 2 || len(r.ResolvedIDs) != 1 {
		t.Fatalf("%+v %v", r, err)
	}
}

func TestIntentExcludesManyInstances(t *testing.T) {
	base := []byte("runner.default: {use: runner}\na: {use: a}\nb: {use: b}\nc: {use: c}\n")
	_, r, err := ResolveYAMLWithReport(".", base, nil, WithDeploymentIntent(DeploymentIntent{Excluded: []string{"a", "b", "c"}}))
	if err != nil || len(r.ResolvedIDs) != 1 {
		t.Fatalf("%+v %v", r, err)
	}
}

func TestReportInterpolatedDependency(t *testing.T) {
	_, r, err := ResolveYAMLWithReport(".", []byte("runner.default: {use: runner, deps: {child: '${var:DEP}'}}\nchild: {use: child}"), nil, WithEnvLookup(MapEnvLookup(map[string]string{"DEP": "child"})))
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Dependencies) != 1 || r.Dependencies[0].Target != "child" || r.Dependencies[0].State != "present" {
		t.Fatalf("%+v", r)
	}
}

func TestReportShiftedDependencyDoesNotLeak(t *testing.T) {
	_, r, err := ResolveYAMLWithReport(".", []byte("runner.default: {use: runner, deps: {children: [off, '${var:SECRET}']}}\noff: {use: off}"), [][]byte{[]byte("off: null")}, WithEnvLookup(MapEnvLookup(map[string]string{"SECRET": "supersecret"})))
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(r)
	if strings.Contains(string(encoded), "supersecret") {
		t.Fatal(string(encoded))
	}
}

func TestIntentPrunesInterpolatedDisabledDependencies(t *testing.T) {
	t.Setenv("REPORT_DEP", "")
	t.Setenv("REPORT_REQUIRED_MISSING", "")
	for _, disabledBy := range []string{"excluded", "explicit_null", "missing_value"} {
		t.Run(disabledBy, func(t *testing.T) {
			base := []byte("runner.default: {use: runner, deps: {good: good, wrapper: wrapper}}\ngood: {use: good}\nwrapper: {use: wrapper, deps: {child: '${var:REPORT_DEP}'}}\nchild: {use: child}\n")
			opts := []ResolveOption{WithEnvLookup(MapEnvLookup(map[string]string{"REPORT_DEP": "child"}))}
			var overlays [][]byte
			switch disabledBy {
			case "excluded":
				opts = append(opts, WithDeploymentIntent(DeploymentIntent{Excluded: []string{"child"}}))
			case "explicit_null":
				overlays = [][]byte{[]byte("child: null")}
			case "missing_value":
				overlays = [][]byte{[]byte("child: {config: {key: '${var:REPORT_REQUIRED_MISSING}'}}")}
			}
			out, report, err := ResolveYAMLWithReport(".", base, overlays, opts...)
			if err != nil {
				t.Fatal(err)
			}
			plain, err := ResolveYAMLWithOptions(".", base, overlays, opts...)
			if err != nil || string(plain) != string(out) {
				t.Fatalf("legacy resolver differs: %v\n%s\n%s", err, plain, out)
			}
			var graph map[string]any
			if err := yaml.Unmarshal(out, &graph); err != nil {
				t.Fatal(err)
			}
			if _, ok := graph["wrapper"]; ok {
				t.Fatalf("wrapper survived with a disabled dependency: %s", out)
			}
			assertReportPrunedEdge(t, report, "wrapper", "child")
			for _, instance := range report.Instances {
				if instance.ID == "wrapper" && (instance.State != "dependency_cascade" || len(instance.Causes) != 1 || instance.Causes[0] != "child") {
					t.Fatalf("missing cascade cause: %+v", instance)
				}
			}
		})
	}
}

func TestIntentPrunesInlineListDependencies(t *testing.T) {
	base := []byte("runner.default: {use: runner, deps: {nodes: [{use: wrapper, deps: {child: child}}, good]}}\ngood: {use: good}\nchild: {use: child}\n")
	out, report, err := ResolveYAMLWithReport(".", base, nil, WithDeploymentIntent(DeploymentIntent{Excluded: []string{"child"}}))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(out), "wrapper") || strings.Contains(string(out), "child") {
		t.Errorf("disabled inline dependency survived: %s", out)
	}
	assertReportPrunedEdge(t, report, "runner.default", "child")
}

func assertReportPrunedEdge(t *testing.T, report ResolveReport, instance, target string) {
	t.Helper()
	count := 0
	for _, dependency := range report.Dependencies {
		if dependency.InstanceID == instance && dependency.Target == target {
			count++
			if dependency.State != "pruned" {
				t.Errorf("disabled dependency has conflicting state: %+v", dependency)
			}
		}
	}
	if count != 1 {
		t.Errorf("expected one pruned edge %s -> %s, got %d: %+v", instance, target, count, report.Dependencies)
	}
}

func TestReportAllowsEmptyOptionalParameters(t *testing.T) {
	t.Setenv("REPORT_OPTIONAL_MISSING", "")
	base := []byte("runner.default: {use: runner, config: {empty: '', absent: null, fallback: '${var:REPORT_OPTIONAL_MISSING:-}'}, deps: {empty: '', absent: null, fallback: '${var:REPORT_OPTIONAL_MISSING:-}'}}\n")
	out, report, err := ResolveYAMLWithReport(".", base, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(report.ResolvedIDs) != 1 || report.Instances[0].State != "active" {
		t.Fatalf("optional empty parameters disabled the instance: %+v", report)
	}
	var graph map[string]any
	if err := yaml.Unmarshal(out, &graph); err != nil {
		t.Fatal(err)
	}
	node := graph["runner.default"].(map[string]any)
	cfg := node["config"].(map[string]any)
	if cfg["empty"] != "" || cfg["absent"] != nil || cfg["fallback"] != "" {
		t.Fatalf("optional empty parameters changed: %+v", cfg)
	}
}

func TestIntentRequiredFailsAfterInterpolatedCascade(t *testing.T) {
	t.Setenv("REPORT_DEP", "")
	base := []byte("runner.default: {use: runner, deps: {good: good, wrapper: wrapper}}\ngood: {use: good}\nwrapper: {use: wrapper, deps: {child: '${var:REPORT_DEP}'}}\nchild: {use: child}\n")
	_, report, err := ResolveYAMLWithReport(".", base, nil,
		WithEnvLookup(MapEnvLookup(map[string]string{"REPORT_DEP": "child"})),
		WithDeploymentIntent(DeploymentIntent{Required: []string{"wrapper"}, Excluded: []string{"child"}}))
	if err == nil || !strings.Contains(err.Error(), `required instance "wrapper"`) {
		t.Fatalf("required instance silently pruned: %v", err)
	}
	assertReportPrunedEdge(t, report, "wrapper", "child")
}

func TestIntentPreservesInlineOptionalEmptyDependency(t *testing.T) {
	base := []byte("runner.default: {use: runner, deps: {nodes: [{use: wrapper, deps: {optional: null}}, child]}}\nchild: {use: child}\n")
	out, _, err := ResolveYAMLWithReport(".", base, nil, WithDeploymentIntent(DeploymentIntent{Excluded: []string{"child"}}))
	if err != nil {
		t.Fatal(err)
	}
	var graph map[string]any
	if err := yaml.Unmarshal(out, &graph); err != nil {
		t.Fatal(err)
	}
	node := graph["runner.default"].(map[string]any)
	deps := node["deps"].(map[string]any)
	nodes := deps["nodes"].([]any)
	if len(nodes) != 1 {
		t.Fatalf("expected only optional inline plugin: %s", out)
	}
	inline, ok := nodes[0].(map[string]any)
	if !ok || inline["use"] != "wrapper" {
		t.Fatalf("inline plugin with optional empty dependency was removed: %s", out)
	}
}
