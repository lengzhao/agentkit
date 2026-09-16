package llm

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/lengzhao/agentkit"
)

// CollectAssistantText reads a provider stream until EOF and returns assistant text.
func CollectAssistantText(stream agentkit.LLMStream) (string, error) {
	if stream == nil {
		return "", fmt.Errorf("nil llm stream")
	}
	defer stream.Close()

	var final strings.Builder
	var delta strings.Builder
	for {
		ev, err := stream.Recv()
		if ev.Type == agentkit.LLMEventMessage && ev.Message != nil {
			final.Reset()
			for _, part := range ev.Message.Content {
				if part.Type == "text" || part.Type == "" {
					final.WriteString(part.Text)
				}
			}
		} else if ev.Delta != "" {
			delta.WriteString(ev.Delta)
		}
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return "", err
		}
	}
	if final.Len() > 0 {
		return strings.TrimSpace(final.String()), nil
	}
	return strings.TrimSpace(delta.String()), nil
}

// CompleteText runs a single-turn LLM request and returns the assistant text.
func CompleteText(ctx context.Context, provider agentkit.LLMProvider, req agentkit.LLMRequest) (string, error) {
	if provider == nil {
		return "", fmt.Errorf("nil llm provider")
	}
	stream, err := provider.Stream(ctx, req)
	if err != nil {
		return "", err
	}
	text, err := CollectAssistantText(stream)
	if err != nil {
		return "", err
	}
	if text == "" {
		return "", fmt.Errorf("empty model response")
	}
	return text, nil
}
