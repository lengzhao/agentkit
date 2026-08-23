package llm

import (
	"context"
	"errors"
	"io"

	"github.com/lengzhao/agentkit"
	openai "github.com/sashabaranov/go-openai"
)

type chatStream struct {
	stream *openai.ChatCompletionStream
	acc    *streamAccumulator
}

func (b *chatBackend) stream(ctx context.Context, model string, req agentkit.LLMRequest) (agentkit.LLMStream, error) {
	return streamWithProviderRetry(ctx, b.providerRetry, func() (agentkit.LLMStream, error) {
		stream, err := b.client.CreateChatCompletionStream(ctx, openai.ChatCompletionRequest{
			Model:    model,
			Messages: toChatCompletionMessages(req.Messages),
			Tools:    toOpenAITools(req.Tools),
			Stream:   true,
		})
		if err != nil {
			return nil, err
		}
		return &chatStream{stream: stream, acc: newStreamAccumulator()}, nil
	})
}

func (s *chatStream) Recv() (agentkit.LLMEvent, error) {
	if ev, ok := s.acc.recvPending(); ok {
		return ev, nil
	}
	if s.acc.done {
		return agentkit.LLMEvent{}, io.EOF
	}
	for {
		chunk, err := s.stream.Recv()
		if errors.Is(err, io.EOF) {
			if !s.acc.done {
				s.acc.finalize()
				return s.Recv()
			}
			return agentkit.LLMEvent{}, io.EOF
		}
		if err != nil {
			return agentkit.LLMEvent{}, err
		}
		if len(chunk.Choices) == 0 {
			continue
		}
		choice := chunk.Choices[0]
		delta := choice.Delta
		if delta.ReasoningContent != "" {
			s.acc.appendThinkingDelta(delta.ReasoningContent)
		}
		if delta.Content != "" {
			s.acc.appendTextDelta(delta.Content)
		}
		for i, tc := range delta.ToolCalls {
			idx := i
			if tc.Index != nil {
				idx = *tc.Index
			}
			s.acc.appendToolCallDelta(idx, tc.ID, tc.Function.Name, tc.Function.Arguments)
		}
		if ev, ok := s.acc.recvPending(); ok {
			return ev, nil
		}
		if choice.FinishReason != "" {
			s.acc.finalize()
			return s.Recv()
		}
	}
}

func (s *chatStream) Close() error {
	if s.stream == nil {
		return nil
	}
	return s.stream.Close()
}
