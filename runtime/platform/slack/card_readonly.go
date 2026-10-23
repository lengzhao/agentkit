package slack

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/lengzhao/agentkit/cap/interaction"
	slackapi "github.com/slack-go/slack"
)

// Slack has no disabled state for interactive elements. Preserve all display
// blocks and replace only controls at their original positions with plain text.
func submittedSlackBlocks(e interaction.Event, summary string) ([]slackapi.Block, error) {
	var card map[string]any
	decoder := json.NewDecoder(bytes.NewReader(e.RawCard))
	decoder.UseNumber()
	if err := decoder.Decode(&card); err != nil {
		return nil, fmt.Errorf("original card JSON unavailable: %w", err)
	}
	blocks, ok := card["blocks"].([]any)
	if !ok {
		return nil, fmt.Errorf("original card blocks unavailable; preserving card")
	}
	textOf := func(v any) string {
		m, _ := v.(map[string]any)
		s, _ := m["text"].(string)
		return s
	}
	controlText := func(control map[string]any) string {
		var texts []string
		if label, ok := control["accessibility_label"].(string); ok && label != "" {
			texts = append(texts, label)
		}
		if text := textOf(control["text"]); text != "" {
			texts = append(texts, text)
		}
		if text := textOf(control["placeholder"]); text != "" {
			texts = append(texts, text)
		}
		var options func(any)
		options = func(v any) {
			switch x := v.(type) {
			case []any:
				for _, item := range x {
					options(item)
				}
			case map[string]any:
				if text := textOf(x["text"]); text != "" {
					texts = append(texts, text)
				}
				options(x["options"])
				options(x["buttons"])
			}
		}
		options(control["options"])
		options(control["option_groups"])
		options(control["buttons"])
		id, _ := control["action_id"].(string)
		for _, answer := range e.Answers {
			if id != "" && answer.Control == id {
				label := answer.Label
				if label == "" {
					label = fmt.Sprint(answer.Value)
				}
				texts = append(texts, "已选择: "+label)
			}
		}
		if len(texts) == 0 {
			return "已提交"
		}
		return strings.Join(texts, " · ")
	}
	plain := func(text string) map[string]any { return map[string]any{"type": "plain_text", "text": text} }
	var out []any
	for _, raw := range blocks {
		block, ok := raw.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("invalid original block")
		}
		switch block["type"] {
		case "actions", "context_actions":
			controls, _ := block["elements"].([]any)
			var labels []string
			for _, c := range controls {
				if m, ok := c.(map[string]any); ok {
					labels = append(labels, controlText(m))
				}
			}
			if len(labels) == 0 {
				continue
			}
			block = map[string]any{"type": "section", "text": plain(strings.Join(labels, "\n")), "block_id": block["block_id"]}
		case "input":
			element, _ := block["element"].(map[string]any)
			text := textOf(block["label"]) + "\n" + controlText(element)
			if hint := textOf(block["hint"]); hint != "" {
				text += "\n" + hint
			}
			block = map[string]any{"type": "section", "text": plain(text), "block_id": block["block_id"]}
		case "section":
			if accessory, ok := block["accessory"].(map[string]any); ok && accessory["type"] != "image" {
				delete(block, "accessory")
				out = append(out, block, map[string]any{"type": "section", "text": plain(controlText(accessory))})
				continue
			}
		}
		out = append(out, block)
	}
	out = append(out, map[string]any{"type": "section", "text": plain(summary)})
	// Do not claim a closed card if a new/unknown block still contains controls.
	var interactive func(any) bool
	interactive = func(v any) bool {
		switch x := v.(type) {
		case []any:
			for _, child := range x {
				if interactive(child) {
					return true
				}
			}
		case map[string]any:
			if _, ok := x["action_id"]; ok {
				return true
			}
			for _, child := range x {
				if interactive(child) {
					return true
				}
			}
		}
		return false
	}
	if interactive(out) {
		return nil, fmt.Errorf("unsupported interactive block; preserving original card")
	}
	// Derived option lists and submitted input can exceed a section's limit.
	var bounded []any
	for _, item := range out {
		block := item.(map[string]any)
		text, _ := block["text"].(map[string]any)
		content, _ := text["text"].(string)
		runes := []rune(content)
		if block["type"] != "section" || len(runes) <= 3000 {
			bounded = append(bounded, block)
			continue
		}
		first := true
		for len(runes) > 0 {
			n := min(len(runes), 3000)
			if first {
				text["text"] = string(runes[:n])
				bounded = append(bounded, block)
				first = false
			} else {
				bounded = append(bounded, map[string]any{"type": "section", "text": plain(string(runes[:n]))})
			}
			runes = runes[n:]
		}
	}
	limit := 50
	if e.InteractionID != "" {
		limit = 100
	}
	if len(bounded) > limit {
		return nil, fmt.Errorf("submitted card exceeds Slack %d-block limit; preserving original card", limit)
	}
	raw, err := json.Marshal(bounded)
	if err != nil {
		return nil, err
	}
	var result slackapi.Blocks
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, err
	}
	return result.BlockSet, nil
}
