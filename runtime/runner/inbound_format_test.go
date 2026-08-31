package runner_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/runtime/runner"
)

type stubTZPlatform struct {
	tz string
}

func (p stubTZPlatform) Receive(context.Context) (agentkit.MessageEvent, error) {
	panic("unused")
}
func (p stubTZPlatform) Send(context.Context, agentkit.OutboundEvent) error { return nil }
func (p stubTZPlatform) UserTimezone(string) string                         { return p.tz }

func TestBuildInboundPromptPrefixInjectList(t *testing.T) {
	t.Parallel()
	root, err := runner.New(runner.Config{
		Inject: []string{"sender_id", "sender_name", "platform", "chat_id"},
	}, runner.Deps{
		Platform: &scriptedPlatform{},
		Loop:     &recordingLoop{},
	})
	if err != nil {
		t.Fatal(err)
	}
	r := root.(*runner.Root)

	event := agentkit.MessageEvent{
		PlatformID: "feishu",
		UserID:     "user123",
		Metadata:   map[string]any{"displayName": "Alice"},
	}
	delivery := agentkit.SessionID("feishu:channel42:user123")
	got := r.BuildInboundPromptPrefixForTest(event, delivery)
	want := `[meta sender_id=user123 sender_name="Alice" platform=feishu chat_id=channel42]`
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestBuildInboundPromptPrefixDisabled(t *testing.T) {
	t.Parallel()
	root, err := runner.New(runner.Config{Inject: []string{}}, runner.Deps{
		Platform: &scriptedPlatform{},
		Loop:     &recordingLoop{},
	})
	if err != nil {
		t.Fatal(err)
	}
	r := root.(*runner.Root)
	got := r.BuildInboundPromptPrefixForTest(agentkit.MessageEvent{
		UserID:     "user1",
		PlatformID: "feishu",
	}, agentkit.SessionID("feishu:ch:user1"))
	if got != "" {
		t.Fatalf("expected empty prefix, got %q", got)
	}
}

func TestBuildInboundPromptPrefixDefaultInject(t *testing.T) {
	t.Parallel()
	root, err := runner.New(runner.Config{
		Inject:          []string{"sender_id", "sender_name", "timestamp"},
		DefaultTimezone: "UTC",
	}, runner.Deps{
		Platform: &scriptedPlatform{},
		Loop:     &recordingLoop{},
	})
	if err != nil {
		t.Fatal(err)
	}
	r := root.(*runner.Root)
	got := r.BuildInboundPromptPrefixForTest(agentkit.MessageEvent{
		UserID:   "U1",
		Metadata: map[string]any{"displayName": "Alice"},
	}, agentkit.SessionID("slack:C1:u:U1"))
	for _, want := range []string{
		`sender_id=U1`,
		`sender_name="Alice"`,
		`timestamp="`,
		`timezone="UTC"`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in %q", want, got)
		}
	}
}

func TestBuildInboundPromptPrefixDefaultSkippedWithoutUserID(t *testing.T) {
	t.Parallel()
	root, err := runner.New(runner.Config{}, runner.Deps{
		Platform: &scriptedPlatform{},
		Loop:     &recordingLoop{},
	})
	if err != nil {
		t.Fatal(err)
	}
	r := root.(*runner.Root)
	got := r.BuildInboundPromptPrefixForTest(agentkit.MessageEvent{
		PlatformID: "cli",
	}, agentkit.SessionID("cli:default"))
	if got != "" {
		t.Fatalf("expected empty prefix without UserID, got %q", got)
	}
}

func TestBuildInboundPromptPrefixInjectTimestamp(t *testing.T) {
	t.Parallel()
	root, err := runner.New(runner.Config{
		Inject:          []string{"timestamp"},
		DefaultTimezone: "UTC",
	}, runner.Deps{
		Platform: &scriptedPlatform{},
		Loop:     &recordingLoop{},
	})
	if err != nil {
		t.Fatal(err)
	}
	r := root.(*runner.Root)
	got := r.BuildInboundPromptPrefixForTest(agentkit.MessageEvent{UserID: "u1"}, agentkit.SessionID("cli:default"))
	if !strings.HasPrefix(got, `[meta timestamp="`) || !strings.Contains(got, `timezone="UTC"`) {
		t.Fatalf("unexpected prefix: %q", got)
	}
}

func TestBuildInboundPromptPrefixInjectContextFields(t *testing.T) {
	t.Parallel()
	root, err := runner.New(runner.Config{
		Inject: []string{"task_id", "trace_id", "language", "custom.*"},
	}, runner.Deps{
		Platform: &scriptedPlatform{},
		Loop:     &recordingLoop{},
	})
	if err != nil {
		t.Fatal(err)
	}
	r := root.(*runner.Root)
	got := r.BuildInboundPromptPrefixForTest(agentkit.MessageEvent{
		Metadata: map[string]any{
			"task_id":       "job-9",
			"trace_id":      "trace-1",
			"language":      "zh-CN",
			"custom.tenant": "acme",
			"ignored":       "secret",
		},
	}, agentkit.SessionID("chat-api:conv:u1"))
	for _, want := range []string{
		`task_id="job-9"`,
		`trace_id="trace-1"`,
		`language="zh-CN"`,
		`custom.tenant="acme"`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in %q", want, got)
		}
	}
	if strings.Contains(got, "ignored") {
		t.Fatalf("unexpected allowlist leak: %q", got)
	}
}

func TestBuildInboundPromptPrefixCombinedInject(t *testing.T) {
	t.Parallel()
	root, err := runner.New(runner.Config{
		Inject:          []string{"timestamp", "sender_id", "sender_name", "task_id"},
		DefaultTimezone: "UTC",
	}, runner.Deps{
		Platform: &scriptedPlatform{},
		Loop:     &recordingLoop{},
	})
	if err != nil {
		t.Fatal(err)
	}
	r := root.(*runner.Root)
	got := r.BuildInboundPromptPrefixForTest(agentkit.MessageEvent{
		UserID:   "U1",
		Metadata: map[string]any{"displayName": "Bob", "task_id": "t-42"},
	}, agentkit.SessionID("slack:C1:u:U1"))
	for _, want := range []string{
		`timestamp="`,
		`sender_id=U1`,
		`sender_name="Bob"`,
		`task_id="t-42"`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in %q", want, got)
		}
	}
}

func TestFormatInboundEventSkipPromptMeta(t *testing.T) {
	t.Parallel()
	root, err := runner.New(runner.Config{Inject: []string{"sender_id"}}, runner.Deps{
		Platform: &scriptedPlatform{},
		Loop:     &recordingLoop{},
	})
	if err != nil {
		t.Fatal(err)
	}
	r := root.(*runner.Root)
	event := r.FormatInboundEventForTest(agentkit.MessageEvent{
		UserID:     "U1",
		PlatformID: "slack",
		Message: agentkit.ModelMessage{
			Role:    "user",
			Content: []agentkit.ContentPart{{Type: "text", Text: "hello"}},
		},
		Metadata: map[string]any{agentkit.MetadataSkipPromptMeta: true},
	}, agentkit.SessionID("slack:C1:u:U1"))
	if text := event.Message.Content[0].Text; text != "hello" {
		t.Fatalf("got %q, want raw hello", text)
	}
}

func TestFormatInboundEventPrependsPrefix(t *testing.T) {
	t.Parallel()
	root, err := runner.New(runner.Config{Inject: []string{"sender_id", "platform", "chat_id"}}, runner.Deps{
		Platform: &scriptedPlatform{},
		Loop:     &recordingLoop{},
	})
	if err != nil {
		t.Fatal(err)
	}
	r := root.(*runner.Root)
	event := r.FormatInboundEventForTest(agentkit.MessageEvent{
		UserID:     "U999",
		PlatformID: "slack",
		Message: agentkit.ModelMessage{
			Role:    "user",
			Content: []agentkit.ContentPart{{Type: "text", Text: "hi"}},
		},
	}, agentkit.SessionID("slack:C012:u:U999"))
	want := `[meta sender_id=U999 platform=slack chat_id=C012]` + "\nhi"
	if event.Message.Content[0].Text != want {
		t.Fatalf("got %q, want %q", event.Message.Content[0].Text, want)
	}
}

func TestFormatInjectTimestampUsesPlatformTimezone(t *testing.T) {
	t.Parallel()
	p := stubTZPlatform{tz: "Asia/Shanghai"}
	root, err := runner.New(runner.Config{Inject: []string{"timestamp"}}, runner.Deps{
		Platform: p,
		Loop:     &recordingLoop{},
	})
	if err != nil {
		t.Fatal(err)
	}
	r := root.(*runner.Root)
	got := r.BuildInboundPromptPrefixForTest(agentkit.MessageEvent{UserID: "u1"}, agentkit.SessionID("feishu:ch:u1"))
	if !strings.Contains(got, `timezone="Asia/Shanghai"`) {
		t.Fatalf("got %q", got)
	}
	_, err = time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatal(err)
	}
}
