package slack

import (
	"bytes"
	"context"
	"fmt"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/runtime/platform/common"
	"github.com/slack-go/slack"
)

func (p *Platform) sendMediaPart(ctx context.Context, sessionID agentkit.SessionID, part agentkit.ContentPart) error {
	raw, ok := p.deliveries.Load(sessionID)
	if !ok {
		return fmt.Errorf("slack: unknown session %s", sessionID)
	}
	d := raw.(delivery)

	data, name, err := common.ReadMediaPart(part)
	if err != nil {
		return fmt.Errorf("slack: %w", err)
	}

	params := slack.UploadFileParameters{
		Reader:   bytes.NewReader(data),
		FileSize: len(data),
		Filename: name,
		Channel:  d.channel,
	}
	if d.threadTS != "" {
		params.ThreadTimestamp = d.threadTS
	}
	if _, err := p.client.UploadFileContext(ctx, params); err != nil {
		return fmt.Errorf("slack: upload file: %w", err)
	}
	return nil
}
