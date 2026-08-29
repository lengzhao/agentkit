package common_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/runtime/platform/common"
)

func TestOutboundSendsTextAndDocument(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "report.txt")
	if err := os.WriteFile(filePath, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}

	var texts []string
	var media []agentkit.ContentPart
	out := common.NewOutbound(
		func(_ context.Context, _ agentkit.SessionID, text string) error {
			texts = append(texts, text)
			return nil
		},
		func(_ context.Context, _ agentkit.SessionID, part agentkit.ContentPart) error {
			media = append(media, part)
			return nil
		},
	)

	msg := agentkit.ModelMessage{
		Role: "assistant",
		Content: []agentkit.ContentPart{
			{Type: "text", Text: "see attached"},
			{Type: "document", URL: filePath},
		},
	}
	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatal(err)
	}

	if err := out.Handle(context.Background(), agentkit.OutboundEvent{
		SessionID: "slack:C1",
		Type:      agentkit.EventAssistantMessage,
		Data:      data,
	}); err != nil {
		t.Fatal(err)
	}
	if len(texts) != 1 || texts[0] != "see attached" {
		t.Fatalf("texts=%v", texts)
	}
	if len(media) != 1 || media[0].URL != filePath {
		t.Fatalf("media=%v", media)
	}
}

func TestOutboundDocumentOnly(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "only.pdf")
	if err := os.WriteFile(filePath, []byte("pdf"), 0o644); err != nil {
		t.Fatal(err)
	}

	var media []agentkit.ContentPart
	out := common.NewOutbound(nil, func(_ context.Context, _ agentkit.SessionID, part agentkit.ContentPart) error {
		media = append(media, part)
		return nil
	})

	msg := agentkit.ModelMessage{
		Role:    "assistant",
		Content: []agentkit.ContentPart{{Type: "document", URL: filePath}},
	}
	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatal(err)
	}

	if err := out.Handle(context.Background(), agentkit.OutboundEvent{
		SessionID: "slack:C1",
		Type:      agentkit.EventAssistantMessage,
		Data:      data,
	}); err != nil {
		t.Fatal(err)
	}
	if len(media) != 1 {
		t.Fatalf("media=%d want 1", len(media))
	}
}
