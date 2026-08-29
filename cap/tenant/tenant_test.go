package tenant_test

import (
	"testing"

	"github.com/lengzhao/agentkit/cap/tenant"
)

func TestKey(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"slack:C123ABC":                 "slack:C123ABC",
		"slack:C123ABC:U456":            "slack:C123ABC",
		"slack:C123ABC:t:1712345678.99": "slack:C123ABC",
		"slack:C123ABC:u:U456":          "slack:C123ABC",
		"feishu:oc_xxx:t:om_yyy":        "feishu:oc_xxx",
		"cli:default":                   "cli:default",
		"  slack:C1  ":                  "slack:C1",
		"":                              "",
		"bare-id":                       "bare-id",
		"slack:":                        "slack",
		":leading":                      ":leading",
	}
	for in, want := range cases {
		if got := tenant.Key(in); got != want {
			t.Errorf("Key(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestDirName(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"slack:C123ABC": "slack_C123ABC",
		"cli:default":   "cli_default",
		"a.b":           "a.b",
		"":              "",
	}
	for in, want := range cases {
		if got := tenant.DirName(in); got != want {
			t.Errorf("DirName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestRoutingSegment(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"slack:C123ABC":          "C123ABC",
		"chat-api:slack_D0AK":    "slack_D0AK",
		"cli:default":            "default",
		"bare-id":                "bare-id",
		"slack:C123ABC:u:U456":   "C123ABC",
		"":                       "",
	}
	for in, want := range cases {
		if got := tenant.RoutingSegment(in); got != want {
			t.Errorf("RoutingSegment(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestLocalDirNameOmitPlatform(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"slack:C123ABC":       "C123ABC",
		"chat-api:slack_D0AK": "slack_D0AK",
		"cli:default":         "default",
	}
	for in, want := range cases {
		if got := tenant.LocalDirName(in, true); got != want {
			t.Errorf("LocalDirName(%q, true) = %q, want %q", in, got, want)
		}
		if got := tenant.LocalDirName(in, false); got != tenant.DirName(in) {
			t.Errorf("LocalDirName(%q, false) = %q, want DirName %q", in, got, tenant.DirName(in))
		}
	}
}

// A tenant key reaching a sibling tenant's directory would defeat the whole
// point, so no key may survive as a traversal.
func TestDirNameNeverTraverses(t *testing.T) {
	t.Parallel()

	for _, in := range []string{"..", "../..", "slack:../other", "a/../../b", ".", "./..", "...."} {
		got := tenant.DirName(in)
		if got == "." || got == ".." {
			t.Fatalf("DirName(%q) = %q", in, got)
		}
		for i := 0; i+1 < len(got); i++ {
			if got[i] == '.' && got[i+1] == '.' {
				t.Fatalf("DirName(%q) = %q contains ..", in, got)
			}
		}
		for _, r := range got {
			if r == '/' || r == '\\' {
				t.Fatalf("DirName(%q) = %q contains a separator", in, got)
			}
		}
	}
}
