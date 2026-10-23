package feishu

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// submittedFeishuCard edits a copy of the original native card. It deliberately
// does not round-trip through common.Card, which cannot represent native layouts.
func submittedFeishuCard(raw json.RawMessage, summary string) (string, error) {
	var card map[string]any
	decoder := json.NewDecoder(bytes.NewReader(raw))
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
	container["elements"] = append(elements, map[string]any{"tag": "markdown", "content": summary})
	out, err := json.Marshal(card)
	return string(out), err
}
