package feishu

import (
	"context"
	"fmt"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/runtime/platform/common"
)

func (p *Platform) sendMediaPart(ctx context.Context, sessionID agentkit.SessionID, part agentkit.ContentPart) error {
	rc, ok := p.deliveryFor(sessionID)
	if !ok {
		return fmt.Errorf("%s: unknown session %s", p.tag(), sessionID)
	}

	data, name, err := common.ReadMediaPart(part)
	if err != nil {
		return fmt.Errorf("%s: %w", p.tag(), err)
	}

	switch part.Type {
	case "image":
		return p.SendImage(ctx, rc, common.ImageAttachment{
			Data: data, FileName: name, MimeType: part.MIME,
		})
	case "document":
		return p.SendFile(ctx, rc, common.FileAttachment{
			Data: data, FileName: name, MimeType: part.MIME,
		})
	default:
		return fmt.Errorf("%s: unsupported media type %q", p.tag(), part.Type)
	}
}
