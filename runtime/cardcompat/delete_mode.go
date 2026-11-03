// Package cardcompat translates legacy application card conventions at the
// boundary. Platform renderers only consume the generic card elements it emits.
package cardcompat

import (
	"encoding/hex"
	"sort"
	"strings"

	"github.com/lengzhao/agentkit/runtime/platform/common"
)

// Normalize preserves legacy session-delete cards supplied by downstream hosts.
// It leaves unrelated cards untouched and never mutates the source card.
func Normalize(card *common.Card) *common.Card {
	if card == nil {
		return nil
	}
	var fields []common.CardElement
	var notes []common.CardNote
	var navigation []common.CardActions
	var submit, cancel string
	for _, element := range card.Elements {
		switch e := element.(type) {
		case common.CardListItem:
			id, selectable, ok := deleteListItem(e.BtnValue)
			if !ok {
				return card
			}
			text := deleteItemText(e.Text)
			if selectable {
				fields = append(fields, common.CardCheckbox{Name: "delete_sel_" + hex.EncodeToString([]byte(id)), Text: text, Checked: strings.Contains(e.Text, "☑")})
			} else {
				fields = append(fields, common.CardMarkdown{Content: "▶ " + text})
			}
		case common.CardNote:
			notes = append(notes, e)
		case common.CardActions:
			var remaining []common.CardButton
			for _, button := range e.Buttons {
				switch button.Value {
				case "act:/delete-mode confirm":
					submit = button.Text
				case "act:/delete-mode cancel":
					cancel = button.Text
				default:
					remaining = append(remaining, button)
				}
			}
			if len(remaining) > 0 {
				navigation = append(navigation, common.CardActions{Buttons: remaining, Layout: e.Layout})
			}
		default:
			return card
		}
	}
	if len(fields) == 0 || submit == "" {
		return card
	}
	out := *card
	out.Elements = nil
	for _, note := range notes {
		if note.Text != "" && note.Tag != "delete-mode-selected-count" {
			out.Elements = append(out.Elements, note)
		}
	}
	buttons := []common.CardButton{{Name: "delete_mode_submit", Text: submit, Type: "danger", FormAction: "submit", Value: "act:/delete-mode form-submit"}}
	if cancel != "" {
		buttons = append(buttons, common.CardButton{Name: "delete_mode_cancel", Text: cancel, Value: "act:/delete-mode cancel"})
	}
	out.Elements = append(out.Elements, common.CardForm{Name: "delete_mode_form", Elements: fields, Buttons: buttons})
	for _, row := range navigation {
		out.Elements = append(out.Elements, row)
	}
	return &out
}

func deleteListItem(action string) (string, bool, bool) {
	const toggle = "act:/delete-mode toggle "
	const noop = "act:/delete-mode noop "
	if strings.HasPrefix(action, toggle) {
		id := strings.TrimSpace(strings.TrimPrefix(action, toggle))
		return id, true, id != ""
	}
	if strings.HasPrefix(action, noop) {
		id := strings.TrimSpace(strings.TrimPrefix(action, noop))
		return id, false, id != ""
	}
	return "", false, false
}
func deleteItemText(text string) string {
	text = strings.TrimSpace(text)
	for _, prefix := range []string{"☑ ▶", "◻ ▶", "▶", "☑", "◻"} {
		if strings.HasPrefix(text, prefix) {
			return strings.TrimSpace(strings.TrimPrefix(text, prefix))
		}
	}
	return text
}

// NormalizeAction preserves callback tokens expected by existing delete commands.
func NormalizeAction(name, action string, form map[string]any) string {
	if action == "" {
		switch name {
		case "delete_mode_submit":
			action = "act:/delete-mode form-submit"
		case "delete_mode_cancel":
			action = "act:/delete-mode cancel"
		}
	}
	if action != "act:/delete-mode form-submit" {
		return action
	}
	var selected []string
	seen := map[string]bool{}
	for name, value := range form {
		if !strings.HasPrefix(name, "delete_sel_") || !checked(value) {
			continue
		}
		id, err := hex.DecodeString(strings.TrimPrefix(name, "delete_sel_"))
		if err == nil && len(id) > 0 && !seen[string(id)] {
			seen[string(id)] = true
			selected = append(selected, string(id))
		}
	}
	sort.Strings(selected)
	if len(selected) > 0 {
		action += " " + strings.Join(selected, ",")
	}
	return action
}
func checked(value any) bool {
	switch v := value.(type) {
	case bool:
		return v
	case string:
		v = strings.ToLower(strings.TrimSpace(v))
		return v == "true" || v == "1" || v == "yes" || v == "on"
	case float64:
		return v != 0
	case int:
		return v != 0
	case int64:
		return v != 0
	default:
		return false
	}
}

// IgnoreLegacyToggle keeps old single-toggle callbacks from becoming agent turns.
// Native form cards commit their complete selection on the submit action.
func IgnoreLegacyToggle(action string) bool {
	return strings.HasPrefix(action, "act:/delete-mode toggle ")
}
