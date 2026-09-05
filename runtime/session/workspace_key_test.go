package session_test

import (
	"testing"

	"github.com/lengzhao/agentkit/runtime/session"
)

func TestWorkspaceKey(t *testing.T) {
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
		if got := session.WorkspaceKey(in); got != want {
			t.Errorf("WorkspaceKey(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestWorkspaceDirName(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"slack:C123ABC": "slack_C123ABC",
		"cli:default":   "cli_default",
		"a.b":           "a.b",
		"":              "",
	}
	for in, want := range cases {
		if got := session.WorkspaceDirName(in); got != want {
			t.Errorf("WorkspaceDirName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestWorkspaceLocalDirNameOmitPlatform(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"slack:C123ABC":       "C123ABC",
		"chat-api:slack_D0AK": "slack_D0AK",
		"cli:default":         "default",
	}
	for in, want := range cases {
		if got := session.WorkspaceLocalDirName(in, true); got != want {
			t.Errorf("WorkspaceLocalDirName(%q, true) = %q, want %q", in, got, want)
		}
		if got := session.WorkspaceLocalDirName(in, false); got != session.WorkspaceDirName(in) {
			t.Errorf("WorkspaceLocalDirName(%q, false) = %q, want WorkspaceDirName %q", in, got, session.WorkspaceDirName(in))
		}
	}
}

func TestWorkspaceDirNameNeverTraverses(t *testing.T) {
	t.Parallel()

	for _, in := range []string{"..", "../..", "slack:../other", "a/../../b", ".", "./..", "...."} {
		got := session.WorkspaceDirName(in)
		if got == "." || got == ".." {
			t.Fatalf("WorkspaceDirName(%q) = %q", in, got)
		}
		for i := 0; i+1 < len(got); i++ {
			if got[i] == '.' && got[i+1] == '.' {
				t.Fatalf("WorkspaceDirName(%q) = %q contains ..", in, got)
			}
		}
		for _, r := range got {
			if r == '/' || r == '\\' {
				t.Fatalf("WorkspaceDirName(%q) = %q contains a separator", in, got)
			}
		}
	}
}
