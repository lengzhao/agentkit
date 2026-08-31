package llm

import (
	"context"
	"errors"
	"io"

	"github.com/lengzhao/agentkit"
	openai "github.com/sashabaranov/go-openai"
)

type responsesStream struct {
	stream *openai.ResponseStream
	acc    *streamAccumulator
	toolID map[int]string
}

func (b *responsesBackend) stream(ctx context.Context, model string, req agentkit.LLMRequest) (agentkit.LLMStream, error) {
	return streamWithProviderRetry(ctx, b.providerRetry, func() (agentkit.LLMStream, error) {
		request, err := toResponsesRequest(model, req.Messages, req.Tools, b.hostedTools, b.reasoning)
		if err != nil {
			return nil, err
		}
		stream, err := b.client.CreateResponseStream(ctx, request)
		if err != nil {
			return nil, err
		}
		return &responsesStream{
			stream: stream,
			acc:    newStreamAccumulator(),
			toolID: make(map[int]string),
		}, nil
	})
}

func (s *responsesStream) Recv() (agentkit.LLMEvent, error) {
	if ev, ok := s.acc.recvPending(); ok {
		return ev, nil
	}
	if s.acc.done {
		return agentkit.LLMEvent{}, io.EOF
	}
	for {
		event, err := s.stream.Recv()
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
		if event.Response != nil && event.Response.Usage != nil {
			usage := event.Response.Usage
			s.acc.setUsage(usage.InputTokens, usage.OutputTokens, usage.TotalTokens)
		}
		switch event.Type {
		case openai.ResponseStreamEventOutputTextDelta:
			s.acc.appendTextDelta(event.Delta)
		case openai.ResponseStreamEventReasoningTextDelta,
			openai.ResponseStreamEventReasoningSummaryTextDelta:
			s.acc.appendThinkingDelta(event.Delta)
		case openai.ResponseStreamEventFunctionArgumentsDelta:
			idx := event.OutputIndex
			callID := event.ItemID
			if callID == "" {
				callID = s.toolID[idx]
			} else {
				s.toolID[idx] = callID
			}
			name := ""
			if event.Item != nil {
				name = event.Item.Name
			}
			s.acc.appendToolCallDelta(idx, callID, name, event.Delta)
		case openai.ResponseStreamEventOutputItemAdded:
			if event.Item != nil && event.Item.Type == "function_call" {
				idx := event.OutputIndex
				s.toolID[idx] = event.Item.CallID
				s.acc.appendToolCallDelta(idx, event.Item.CallID, event.Item.Name, "")
			}
		case openai.ResponseStreamEventWebSearchInProgress:
			s.acc.appendThinkingDelta("Searching the web...\n")
		case openai.ResponseStreamEventWebSearchSearching:
			// Provider-side search; no local tool execution needed.
		case openai.ResponseStreamEventWebSearchCompleted:
			s.acc.appendThinkingDelta("\n")
		case openai.ResponseStreamEventCompleted, openai.ResponseStreamEventIncomplete:
			s.acc.finalize()
			return s.Recv()
		case openai.ResponseStreamEventFailed, openai.ResponseStreamEventError:
			if event.Error != nil {
				return agentkit.LLMEvent{}, errors.New(event.Error.Message)
			}
			if event.Message != "" {
				return agentkit.LLMEvent{}, errors.New(event.Message)
			}
		}
		if ev, ok := s.acc.recvPending(); ok {
			return ev, nil
		}
	}
}

func (s *responsesStream) Close() error {
	if s.stream == nil {
		return nil
	}
	return s.stream.Close()
}
