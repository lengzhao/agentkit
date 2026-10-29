package common

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/cap/workspace"
	"github.com/lengzhao/agentkit/runtime/rctx"
	rtworkspace "github.com/lengzhao/agentkit/runtime/workspace"
)

func TestDeferredUploadsUseTurnDirectory(t *testing.T) {
	root, cwd := t.TempDir(), t.TempDir()
	ws := rtworkspace.Static(root)
	event := InboundFromContent("assistant", agentkit.SessionRouteInput{Platform: "slack", ChannelID: "C1"}, "U1", "read", "", []ImageAttachment{{Data: []byte("image"), FileName: "pic.png", MimeType: "image/png"}}, []FileAttachment{{Data: []byte("hello"), FileName: "note.txt"}}, nil, nil, DeferredInboundOptsFor(ws))
	if _, err := os.Stat(filepath.Join(root, "work", "upload")); !os.IsNotExist(err) {
		t.Fatalf("saved before turn: %v", err)
	}
	if len(event.Uploads) != 2 {
		t.Fatalf("uploads = %d", len(event.Uploads))
	}
	ctx := workspace.WithWorkingDirectory(context.Background(), cwd)
	got, err := PrepareInboundAttachments(ctx, ws, event)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Uploads) != 0 || len(got.Attachments) != 2 {
		t.Fatalf("prepared = %#v", got)
	}
	for _, a := range got.Attachments {
		if !filepath.IsAbs(a.Path) || filepath.Dir(a.Path) != filepath.Join(cwd, "upload") {
			t.Fatalf("path = %s", a.Path)
		}
		if _, err := os.Stat(a.Path); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(got.Message.Content[0].Text, a.Path) {
			t.Fatalf("missing reference %s", a.Path)
		}
	}
	if got.Message.Content[1].Source != filepath.Join(cwd, "upload", "pic.png") {
		t.Fatalf("image = %#v", got.Message.Content[1])
	}
	// Re-preparation after a cwd change never reinterprets historical references.
	again, err := PrepareInboundAttachments(workspace.WithWorkingDirectory(ctx, t.TempDir()), ws, got)
	if err != nil || again.Attachments[0].Path != got.Attachments[0].Path {
		t.Fatalf("unstable reference: %v", err)
	}
}

func TestInboundSameNameDoesNotOverwrite(t *testing.T) {
	ws := rtworkspace.Static(t.TempDir())
	route := agentkit.RouteRef{}
	a := SaveInboundAttachments(route, []FileAttachment{{FileName: "note.txt", Data: []byte("first")}}, InboundOptsFor(ws))
	b := SaveInboundAttachments(route, []FileAttachment{{FileName: "note.txt", Data: []byte("second")}}, InboundOptsFor(ws))
	if len(a) != 1 || len(b) != 1 || a[0].Path == b[0].Path {
		t.Fatalf("colliding refs: %v %v", a, b)
	}
}

func TestDeferredUploadFailureAbortsPreparation(t *testing.T) {
	root := t.TempDir()
	ws := rtworkspace.Static(root)
	if err := os.WriteFile(filepath.Join(root, "upload"), []byte("block"), 0600); err != nil {
		t.Fatal(err)
	}
	event := InboundFromContent("assistant", agentkit.SessionRouteInput{}, "", "read", "", nil, []FileAttachment{{FileName: "a.txt", Data: []byte("a")}}, nil, nil, DeferredInboundOptsFor(ws))
	_, err := PrepareInboundAttachments(workspace.WithWorkingDirectory(context.Background(), root), ws, event)
	if err == nil {
		t.Fatal("save failure ignored")
	}
}

func TestBufferedAttachmentKeepsOriginalDirectory(t *testing.T) {
	ws := rtworkspace.Static(t.TempDir())
	saved := SaveInboundAttachments(agentkit.RouteRef{}, []FileAttachment{{Data: []byte("old"), FileName: "a.txt"}}, InboundOptsFor(ws))
	if len(saved) != 1 || !filepath.IsAbs(saved[0].Path) {
		t.Fatalf("unstable buffer: %v", saved)
	}
	opts := DeferredInboundOptsFor(ws)
	opts.PresavedAttachments = saved
	event := InboundFromContent("assistant", agentkit.SessionRouteInput{}, "", "read", "", nil, []FileAttachment{{Data: []byte("new"), FileName: "a.txt"}}, nil, nil, opts)
	got, err := PrepareInboundAttachments(workspace.WithWorkingDirectory(context.Background(), t.TempDir()), ws, event)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Attachments) != 2 || got.Attachments[0].Path != saved[0].Path {
		t.Fatalf("buffer changed ownership: %#v", got.Attachments)
	}
	for i, a := range got.Attachments {
		data, err := os.ReadFile(a.Path)
		want := []string{"old", "new"}[i]
		if err != nil || string(data) != want {
			t.Fatalf("%s content = %q, %v", a.Path, data, err)
		}
	}
}

func TestDeferredUploadsFollowResolvedWorkspace(t *testing.T) {
	ws, err := rtworkspace.NewTenant(rtworkspace.TenantConfig{Global: t.TempDir(), LocalBase: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	event := InboundFromContent("assistant", agentkit.SessionRouteInput{Platform: "slack", ChannelID: "original"}, "", "read", "", nil, []FileAttachment{{FileName: "a.txt", Data: []byte("payload")}}, nil, nil, DeferredInboundOptsFor(ws))
	event.Envelope.Workspace = "actual"
	ctx := rctx.ApplyEnvelopeToContext(context.Background(), event.Envelope)
	got, err := PrepareInboundAttachments(ctx, ws, event)
	if err != nil {
		t.Fatal(err)
	}
	want, err := rtworkspace.ResolveFile(ctx, ws, "upload/a.txt")
	if err != nil {
		t.Fatal(err)
	}
	if got.Attachments[0].Path != want {
		t.Fatalf("saved = %s, want routed workspace %s", got.Attachments[0].Path, want)
	}
}

func TestBufferedAttachmentRejectsDifferentLogicalOwner(t *testing.T) {
	ws := rtworkspace.Static(t.TempDir())
	owner := agentkit.AttachmentOwner{Conversation: "old", Route: rctx.BuildSessionRoute(agentkit.SessionRouteInput{Platform: "lark", Instance: "bot1", ChannelID: "C1", ThreadID: "T1"})}
	event := agentkit.MessageEvent{Attachments: []agentkit.InboundAttachment{{Path: filepath.Join(t.TempDir(), "a.txt"), Owner: &owner}}}
	ctx := rctx.ApplyEnvelopeToContext(context.Background(), agentkit.TurnEnvelope{Conversation: "new", Route: owner.Route})
	if _, err := PrepareInboundAttachments(ctx, ws, event); err == nil {
		t.Fatal("buffer silently handed to a different session")
	}
	ctx = rctx.ApplyEnvelopeToContext(context.Background(), agentkit.TurnEnvelope{Conversation: "old", Route: owner.Route})
	if _, err := PrepareInboundAttachments(ctx, ws, event); err != nil {
		t.Fatal(err)
	}
	changed := rctx.BuildSessionRoute(agentkit.SessionRouteInput{Platform: "lark", Instance: "bot1", ChannelID: "C1", ThreadID: "T2"})
	ctx = rctx.ApplyEnvelopeToContext(context.Background(), agentkit.TurnEnvelope{Conversation: "old", Route: changed})
	if _, err := PrepareInboundAttachments(ctx, ws, event); err == nil {
		t.Fatal("buffer silently handed to a different thread")
	}
}

func TestMixedFreshAndExistingAttachmentsBothReachModel(t *testing.T) {
	ws := rtworkspace.Static(t.TempDir())
	event := InboundFromContent("assistant", agentkit.SessionRouteInput{}, "", "read both", "", nil, []FileAttachment{{Data: []byte("new"), FileName: "new.txt"}}, nil, []string{"upload/existing.txt"}, DeferredInboundOptsFor(ws))
	got, err := PrepareInboundAttachments(context.Background(), ws, event)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got.Message.Content[0].Text, "existing.txt") || len(got.Attachments) != 2 {
		t.Fatalf("missing existing input: %#v", got)
	}
}

func TestUnknownBufferedOwnerPreservesLegacyButRequiresOwnerForNewPolicy(t *testing.T) {
	route := rctx.BuildSessionRoute(agentkit.SessionRouteInput{Platform: "lark", ChannelID: "C1"})
	event := agentkit.MessageEvent{Attachments: []agentkit.InboundAttachment{{Path: filepath.Join(t.TempDir(), "a.txt"), Owner: &agentkit.AttachmentOwner{Unknown: true, Route: route}}}}
	ctx := rctx.ApplyEnvelopeToContext(context.Background(), agentkit.TurnEnvelope{Conversation: "session", Route: route})
	if _, err := PrepareInboundAttachments(ctx, rtworkspace.Static(t.TempDir()), event); err != nil {
		t.Fatalf("legacy regression: %v", err)
	}
	scoped, err := rtworkspace.NewTenant(rtworkspace.TenantConfig{Global: t.TempDir(), LocalBase: t.TempDir(), WorkspaceScope: "global"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = PrepareInboundAttachments(ctx, scoped, event); err == nil {
		t.Fatal("new policy guessed unknown owner")
	}
}
