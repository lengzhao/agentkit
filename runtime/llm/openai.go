package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/cap/credentials"
)

type OpenAIConfig struct {
	Model     string `json:"model"`
	BaseURL   string `json:"baseUrl"`
	APIKey    string `json:"apiKey"`
	APIKeyRef string `json:"apiKeyRef"`
}

type OpenAIDeps struct {
	Credentials credentials.Store `json:"credentials,omitempty"`
}

type OpenAI struct {
	model   string
	baseURL string
	apiKey  string
	client  *http.Client
}

func NewOpenAI(cfg OpenAIConfig, deps OpenAIDeps) (*OpenAI, error) {
	model := cfg.Model
	if model == "" {
		model = "gpt-4o"
	}
	baseURL := strings.TrimRight(cfg.BaseURL, "/")
	if baseURL == "" {
		baseURL = "https://api.openai.com/v1"
	}
	apiKey, err := resolveAPIKey(context.Background(), cfg.APIKey, cfg.APIKeyRef, deps.Credentials)
	if err != nil {
		return nil, err
	}
	return &OpenAI{
		model:   model,
		baseURL: baseURL,
		apiKey:  apiKey,
		client:  &http.Client{Timeout: 10 * time.Minute},
	}, nil
}

func (p *OpenAI) Name() string { return "openai-compatible" }

func (p *OpenAI) Stream(ctx context.Context, req agentkit.LLMRequest) (agentkit.LLMStream, error) {
	if p.apiKey == "" {
		return nil, fmt.Errorf("missing API key: set config.apiKeyRef, config.apiKey, or OPENAI_API_KEY")
	}
	model := req.Model
	if model == "" {
		model = p.model
	}
	body, err := json.Marshal(openAIRequest{
		Model:    model,
		Messages: toOpenAIMessages(req.Messages),
		Tools:    toOpenAITools(req.Tools),
		Stream:   true,
	})
	if err != nil {
		return nil, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 300 {
		defer resp.Body.Close()
		raw, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("openai request failed: status=%d body=%s", resp.StatusCode, string(raw))
	}
	return &openAIStream{resp: resp, scanner: bufio.NewScanner(resp.Body)}, nil
}

type openAIRequest struct {
	Model    string           `json:"model"`
	Messages []openAIMessage  `json:"messages"`
	Tools    []openAITool     `json:"tools,omitempty"`
	Stream   bool             `json:"stream"`
}

type openAIMessage struct {
	Role       string           `json:"role"`
	Content    any              `json:"content,omitempty"`
	ToolCalls  []openAIToolCall `json:"tool_calls,omitempty"`
	ToolCallID string           `json:"tool_call_id,omitempty"`
	Name       string           `json:"name,omitempty"`
}

type openAITool struct {
	Type     string         `json:"type"`
	Function openAIFunction `json:"function"`
}

type openAIFunction struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Parameters  map[string]any `json:"parameters"`
}

type openAIToolCall struct {
	Index    *int   `json:"index,omitempty"`
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

type openAIStream struct {
	resp          *http.Response
	scanner       *bufio.Scanner
	done          bool
	streamStarted bool
	acc           agentkit.ModelMessage
	toolBuf       map[int]*agentkit.ToolCall
	pending       []agentkit.LLMEvent
}

func (s *openAIStream) pushPending(events ...agentkit.LLMEvent) {
	s.pending = append(s.pending, events...)
}

func (s *openAIStream) Recv() (agentkit.LLMEvent, error) {
	if len(s.pending) > 0 {
		ev := s.pending[0]
		s.pending = s.pending[1:]
		return ev, nil
	}
	if s.done {
		return agentkit.LLMEvent{}, io.EOF
	}
	if s.toolBuf == nil {
		s.toolBuf = make(map[int]*agentkit.ToolCall)
		s.acc = agentkit.ModelMessage{Role: "assistant"}
	}
	for s.scanner.Scan() {
		line := strings.TrimSpace(s.scanner.Text())
		if line == "" || !strings.HasPrefix(line, "data:") {
			continue
		}
		payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if payload == "[DONE]" {
			s.done = true
			msg := s.acc
			s.pushPending(agentkit.LLMEvent{Type: "message", Message: &msg})
			return s.Recv()
		}
		var chunk openAIStreamChunk
		if err := json.Unmarshal([]byte(payload), &chunk); err != nil {
			slog.Warn("openai stream decode", "err", err)
			continue
		}
		if len(chunk.Choices) == 0 {
			continue
		}
		delta := chunk.Choices[0].Delta
		if delta.Content != "" {
			if !s.streamStarted {
				s.streamStarted = true
				start := cloneMessage(s.acc)
				s.pushPending(
					agentkit.LLMEvent{Type: "start", Message: start},
					agentkit.LLMEvent{Type: "text_start", ContentIndex: 0, Message: start},
				)
			}
			s.acc.Content = appendText(s.acc.Content, delta.Content)
			msg := cloneMessage(s.acc)
			s.pushPending(agentkit.LLMEvent{
				Type:         "text_delta",
				ContentIndex: 0,
				Delta:        delta.Content,
				Message:      msg,
			})
			return s.Recv()
		}
		for i, tc := range delta.ToolCalls {
			idx := i
			if tc.Index != nil {
				idx = *tc.Index
			}
			call, ok := s.toolBuf[idx]
			if !ok {
				if !s.streamStarted {
					s.streamStarted = true
					start := cloneMessage(s.acc)
					s.pushPending(agentkit.LLMEvent{Type: "start", Message: start})
				}
				call = &agentkit.ToolCall{ID: agentkit.ToolCallID(tc.ID), Name: tc.Function.Name}
				s.toolBuf[idx] = call
				if tc.ID != "" || tc.Function.Name != "" {
					cp := *call
					msg := cloneMessage(s.acc)
					s.pushPending(agentkit.LLMEvent{
						Type:         "toolcall_start",
						ContentIndex: idx,
						ToolCall:     &cp,
						Message:      msg,
					})
				}
			}
			if tc.ID != "" {
				call.ID = agentkit.ToolCallID(tc.ID)
			}
			if tc.Function.Name != "" {
				call.Name = tc.Function.Name
			}
			if tc.Function.Arguments != "" {
				call.Input = append(call.Input, tc.Function.Arguments...)
				cp := *call
				msg := cloneMessage(s.acc)
				s.pushPending(agentkit.LLMEvent{
					Type:         "toolcall_delta",
					ContentIndex: idx,
					Delta:        tc.Function.Arguments,
					ToolCall:     &cp,
					Message:      msg,
				})
			}
		}
		if len(s.pending) > 0 {
			return s.Recv()
		}
		if chunk.Choices[0].FinishReason != nil {
			for idx, call := range s.toolBuf {
				cp := *call
				s.acc.ToolCalls = append(s.acc.ToolCalls, cp)
				msg := cloneMessage(s.acc)
				s.pushPending(agentkit.LLMEvent{
					Type:         "toolcall_end",
					ContentIndex: idx,
					ToolCall:     &cp,
					Message:      msg,
				})
			}
			s.done = true
			msg := s.acc
			s.pushPending(agentkit.LLMEvent{Type: "message", Message: &msg})
			return s.Recv()
		}
	}
	if err := s.scanner.Err(); err != nil {
		return agentkit.LLMEvent{}, err
	}
	s.done = true
	msg := s.acc
	return agentkit.LLMEvent{Type: "message", Message: &msg}, io.EOF
}

func (s *openAIStream) Close() error {
	if s.resp == nil || s.resp.Body == nil {
		return nil
	}
	return s.resp.Body.Close()
}

type openAIStreamChunk struct {
	Choices []struct {
		Delta struct {
			Role      string           `json:"role"`
			Content   string           `json:"content"`
			ToolCalls []openAIToolCall `json:"tool_calls"`
		} `json:"delta"`
		FinishReason *string `json:"finish_reason"`
	} `json:"choices"`
}

func appendText(parts []agentkit.ContentPart, text string) []agentkit.ContentPart {
	if len(parts) == 0 || parts[len(parts)-1].Type != "text" {
		return append(parts, agentkit.ContentPart{Type: "text", Text: text})
	}
	parts[len(parts)-1].Text += text
	return parts
}

func cloneMessage(msg agentkit.ModelMessage) *agentkit.ModelMessage {
	cp := msg
	cp.Content = append([]agentkit.ContentPart(nil), msg.Content...)
	cp.ToolCalls = append([]agentkit.ToolCall(nil), msg.ToolCalls...)
	return &cp
}

func toOpenAIMessages(messages []agentkit.ModelMessage) []openAIMessage {
	out := make([]openAIMessage, 0, len(messages))
	for _, msg := range messages {
		switch msg.Role {
		case "system":
			out = append(out, openAIMessage{Role: "system", Content: textOf(msg.Content)})
		case "user":
			out = append(out, openAIMessage{Role: "user", Content: textOf(msg.Content)})
		case "assistant":
			om := openAIMessage{Role: "assistant", Content: textOf(msg.Content)}
			for _, call := range msg.ToolCalls {
				om.ToolCalls = append(om.ToolCalls, openAIToolCall{
					ID:   string(call.ID),
					Type: "function",
					Function: struct {
						Name      string `json:"name"`
						Arguments string `json:"arguments"`
					}{Name: call.Name, Arguments: string(call.Input)},
				})
			}
			out = append(out, om)
		case "tool":
			for _, result := range msg.ToolResults {
				out = append(out, openAIMessage{
					Role:       "tool",
					ToolCallID: string(result.ID),
					Name:       result.Name,
					Content:    textOf(result.Content),
				})
			}
		}
	}
	return out
}

func textOf(parts []agentkit.ContentPart) string {
	var b strings.Builder
	for _, part := range parts {
		if part.Type == "text" {
			b.WriteString(part.Text)
		}
	}
	return b.String()
}

func toOpenAITools(specs []agentkit.ToolSpec) []openAITool {
	out := make([]openAITool, 0, len(specs))
	for _, spec := range specs {
		params := schemaToMap(spec.InputSchema)
		out = append(out, openAITool{
			Type: "function",
			Function: openAIFunction{
				Name:        spec.Name,
				Description: spec.Description,
				Parameters:  params,
			},
		})
	}
	return out
}

func schemaToMap(schema agentkit.JSONSchema) map[string]any {
	if schema.Raw != nil {
		return schema.Raw
	}
	raw, _ := json.Marshal(schema)
	var out map[string]any
	_ = json.Unmarshal(raw, &out)
	if out == nil {
		out = map[string]any{"type": "object"}
	}
	return out
}
