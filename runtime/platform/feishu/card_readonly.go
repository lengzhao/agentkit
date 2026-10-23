package feishu

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/lengzhao/agentkit/cap/interaction"
	"github.com/lengzhao/agentkit/runtime/platform/common"
)

// submittedFeishuCard edits a copy of the original native card. It deliberately
// does not round-trip through common.Card, which cannot represent native layouts.
func submittedFeishuCard(event interaction.Event, summary string) (string, error) {
	var card map[string]any
	decoder := json.NewDecoder(bytes.NewReader(event.RawCard))
	decoder.UseNumber()
	if err := decoder.Decode(&card); err != nil || card == nil {
		return "", fmt.Errorf("original card JSON unavailable")
	}
	container := card
	if body, ok := card["body"].(map[string]any); ok {
		container = body
	}
	elements, ok := container["elements"].([]any)
	if !ok {
		return "", fmt.Errorf("original card elements unavailable; preserving card")
	}
	for _, element := range elements {
		item, ok := element.(map[string]any)
		if !ok || item["tag"] == nil {
			return "", fmt.Errorf("original card uses a simplified message structure; preserving card")
		}
	}
	var buttons []map[string]any
	var disable func(any) error
	disable = func(v any) error {
		switch x := v.(type) {
		case []any:
			for _, child := range x {
				if err := disable(child); err != nil {
					return err
				}
			}
		case map[string]any:
			tag, _ := x["tag"].(string)
			if tag == "button" {
				buttons = append(buttons, x)
			}
			switch tag {
			case "button", "select_static", "select_person", "select_img", "multi_select_static", "multi_select_person", "input", "date_picker", "picker_time", "picker_datetime", "checker", "checkbox":
				x["disabled"] = true
			case "overflow":
				return fmt.Errorf("unsupported read-only control %q; preserving card", tag)
			default:
				if tag != "" && (x["value"] != nil || x["behaviors"] != nil || x["form_action_type"] != nil) {
					return fmt.Errorf("unsupported read-only control %q; preserving card", tag)
				}
			}
			// Component containers include div.extra and newer native layouts.
			// Callback payloads are data, and must not be edited as components.
			for key, child := range x {
				if key == "value" || key == "behaviors" {
					continue
				}
				if err := disable(child); err != nil {
					return err
				}
			}
		}
		return nil
	}
	if err := disable(elements); err != nil {
		return "", err
	}
	markSubmittedFeishuButton(buttons, event)
	container["elements"] = append(elements, map[string]any{"tag": "markdown", "content": summary})
	out, err := json.Marshal(card)
	return string(out), err
}

// Button styles are presentation, not selection state. Only a unique match
// against the original callback payload may change the submitted selection.
func markSubmittedFeishuButton(buttons []map[string]any, event interaction.Event) {
	var action struct {
		Tag   string `json:"tag"`
		Name  string `json:"name"`
		Value any    `json:"value"`
	}
	decoder := json.NewDecoder(bytes.NewReader(event.RawAction))
	decoder.UseNumber()
	if decoder.Decode(&action) != nil || action.Tag != "button" || action.Value == nil {
		return
	}
	expected, err := json.Marshal(action.Value)
	if err != nil {
		return
	}
	selected := -1
	for i, button := range buttons {
		if action.Name != "" && button["name"] != action.Name {
			continue
		}
		match := false
		for _, value := range common.CardControlValues(button) {
			actual, err := json.Marshal(value)
			if err == nil && bytes.Equal(actual, expected) {
				match = true
				break
			}
		}
		if match {
			if selected >= 0 {
				return
			} // Same payload on multiple buttons is ambiguous.
			selected = i
		}
	}
	if selected < 0 {
		return
	}
	for i, button := range buttons {
		if i != selected {
			if button["type"] == "primary" {
				button["type"] = "default"
			}
			continue
		}
		button["type"] = "primary"
		if text, ok := button["text"].(map[string]any); ok {
			if content, ok := text["content"].(string); ok {
				text["content"] = "✓ " + content
			}
		}
	}
}
