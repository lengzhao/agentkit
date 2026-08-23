package llm

import (
	"encoding/json"
	"strings"

	"github.com/lengzhao/agentkit"
	openai "github.com/sashabaranov/go-openai"
)

func toChatCompletionMessages(messages []agentkit.ModelMessage) []openai.ChatCompletionMessage {
	out := make([]openai.ChatCompletionMessage, 0, len(messages))
	for _, msg := range messages {
		switch msg.Role {
		case "system":
			out = append(out, openai.ChatCompletionMessage{
				Role:    openai.ChatMessageRoleSystem,
				Content: textOf(msg.Content),
			})
		case "user":
			out = append(out, userChatMessage(msg.Content))
		case "assistant":
			om := openai.ChatCompletionMessage{
				Role:    openai.ChatMessageRoleAssistant,
				Content: textOf(msg.Content),
			}
			for _, call := range msg.ToolCalls {
				om.ToolCalls = append(om.ToolCalls, openai.ToolCall{
					ID:   string(call.ID),
					Type: openai.ToolTypeFunction,
					Function: openai.FunctionCall{
						Name:      call.Name,
						Arguments: string(call.Input),
					},
				})
			}
			out = append(out, om)
		case "tool":
			for _, result := range msg.ToolResults {
				out = append(out, openai.ChatCompletionMessage{
					Role:       openai.ChatMessageRoleTool,
					ToolCallID: string(result.ID),
					Name:       result.Name,
					Content:    textOf(result.Content),
				})
			}
		}
	}
	return out
}

func userChatMessage(parts []agentkit.ContentPart) openai.ChatCompletionMessage {
	content, multi := contentToChatParts(parts)
	if len(multi) > 0 {
		return openai.ChatCompletionMessage{
			Role:         openai.ChatMessageRoleUser,
			MultiContent: multi,
		}
	}
	return openai.ChatCompletionMessage{
		Role:    openai.ChatMessageRoleUser,
		Content: content,
	}
}

func contentToChatParts(parts []agentkit.ContentPart) (string, []openai.ChatMessagePart) {
	var textParts []string
	var multi []openai.ChatMessagePart
	for _, part := range parts {
		switch part.Type {
		case "text", "":
			if part.Text != "" {
				textParts = append(textParts, part.Text)
			}
		case "image", "image_url":
			url := imageURL(part)
			if url == "" {
				continue
			}
			multi = append(multi, openai.ChatMessagePart{
				Type: openai.ChatMessagePartTypeImageURL,
				ImageURL: &openai.ChatMessageImageURL{
					URL:    url,
					Detail: openai.ImageURLDetail(part.Detail),
				},
			})
		default:
			if part.Text != "" {
				textParts = append(textParts, part.Text)
			}
		}
	}
	if len(multi) == 0 {
		return strings.Join(textParts, ""), nil
	}
	if len(textParts) > 0 {
		multi = append([]openai.ChatMessagePart{{
			Type: openai.ChatMessagePartTypeText,
			Text: strings.Join(textParts, ""),
		}}, multi...)
	}
	return "", multi
}

func toOpenAITools(specs []agentkit.ToolSpec) []openai.Tool {
	out := make([]openai.Tool, 0, len(specs))
	for _, spec := range specs {
		out = append(out, openai.Tool{
			Type: openai.ToolTypeFunction,
			Function: &openai.FunctionDefinition{
				Name:        spec.Name,
				Description: spec.Description,
				Parameters:  schemaToMap(spec.InputSchema),
			},
		})
	}
	return out
}

func toResponseTools(specs []agentkit.ToolSpec) []openai.ResponseTool {
	out := make([]openai.ResponseTool, 0, len(specs))
	for _, spec := range specs {
		out = append(out, openai.NewResponseFunctionTool(openai.FunctionDefinition{
			Name:        spec.Name,
			Description: spec.Description,
			Parameters:  schemaToMap(spec.InputSchema),
		}))
	}
	return out
}

func toResponsesRequest(model string, messages []agentkit.ModelMessage, tools []agentkit.ToolSpec, reasoning *OpenAIReasoningConfig) openai.CreateResponseRequest {
	instructions, input := toResponsesInput(messages)
	req := openai.CreateResponseRequest{
		Model:        model,
		Input:        input,
		Instructions: instructions,
		Tools:        toResponseTools(tools),
	}
	if reasoning != nil {
		req.Reasoning = &openai.ResponseReasoning{
			Effort:          reasoning.Effort,
			GenerateSummary: reasoning.GenerateSummary,
		}
	}
	return req
}

func toResponsesInput(messages []agentkit.ModelMessage) (string, any) {
	var instructions strings.Builder
	var items []any
	for _, msg := range messages {
		switch msg.Role {
		case "system":
			if instructions.Len() > 0 {
				instructions.WriteByte('\n')
			}
			instructions.WriteString(textOf(msg.Content))
		case "user":
			items = append(items, openai.ResponseInputMessage{
				Type:    "message",
				Role:    openai.ChatMessageRoleUser,
				Content: contentToResponseParts(msg.Content),
			})
		case "assistant":
			if text := textOf(msg.Content); text != "" {
				items = append(items, openai.ResponseInputMessage{
					Type:    "message",
					Role:    openai.ChatMessageRoleAssistant,
					Content: contentToResponseParts(msg.Content),
				})
			}
			for _, call := range msg.ToolCalls {
				items = append(items, responseInputFunctionCall{
					Type:      "function_call",
					CallID:    string(call.ID),
					Name:      call.Name,
					Arguments: string(call.Input),
				})
			}
		case "tool":
			for _, result := range msg.ToolResults {
				items = append(items, openai.ResponseFunctionCallOutput{
					Type:   "function_call_output",
					CallID: string(result.ID),
					Output: textOf(result.Content),
				})
			}
		}
	}
	if len(items) == 0 {
		return instructions.String(), ""
	}
	if len(items) == 1 {
		if msg, ok := items[0].(openai.ResponseInputMessage); ok && msg.Role == openai.ChatMessageRoleUser {
			switch content := msg.Content.(type) {
			case openai.ResponseInputText:
				return instructions.String(), content.Text
			case []any:
				if len(content) == 1 {
					if text, ok := content[0].(openai.ResponseInputText); ok {
						return instructions.String(), text.Text
					}
				}
			}
		}
	}
	return instructions.String(), items
}

type responseInputFunctionCall struct {
	Type      string `json:"type"`
	CallID    string `json:"call_id"`
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

func contentToResponseParts(parts []agentkit.ContentPart) any {
	converted := make([]any, 0, len(parts))
	for _, part := range parts {
		switch part.Type {
		case "text", "":
			if part.Text == "" {
				continue
			}
			converted = append(converted, openai.ResponseInputText{
				Type: "input_text",
				Text: part.Text,
			})
		case "image", "image_url":
			url := imageURL(part)
			if url == "" {
				continue
			}
			converted = append(converted, openai.ResponseInputImage{
				Type:     "input_image",
				ImageURL: url,
				Detail:   part.Detail,
			})
		default:
			if part.Text != "" {
				converted = append(converted, openai.ResponseInputText{
					Type: "input_text",
					Text: part.Text,
				})
			}
		}
	}
	if len(converted) == 1 {
		return converted[0]
	}
	return converted
}

func imageURL(part agentkit.ContentPart) string {
	if part.URL != "" {
		return part.URL
	}
	return part.Text
}

func textOf(parts []agentkit.ContentPart) string {
	var b strings.Builder
	for _, part := range parts {
		if part.Type == "text" || part.Type == "" {
			b.WriteString(part.Text)
		}
	}
	return b.String()
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
