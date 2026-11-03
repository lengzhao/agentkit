package cardcompat

import (
	"github.com/lengzhao/agentkit/runtime/platform/common"
	"testing"
)

func legacyDeleteCard() *common.Card {
	return &common.Card{Header: &common.CardHeader{Title: "Delete sessions"}, Elements: []common.CardElement{
		common.CardNote{Text: "Choose sessions"}, common.CardNote{Text: "Selected: 1", Tag: "delete-mode-selected-count"},
		common.CardListItem{Text: "☑ one", BtnValue: "act:/delete-mode toggle session:one"},
		common.CardListItem{Text: "▶ active", BtnValue: "act:/delete-mode noop active"},
		common.CardActions{Buttons: []common.CardButton{{Text: "Delete", Value: "act:/delete-mode confirm"}, {Text: "Cancel", Value: "act:/delete-mode cancel"}, {Text: "Next", Value: "nav:next"}}},
	}}
}
func TestNormalizeLegacyDeleteCardWithoutMutatingInput(t *testing.T) {
	card := legacyDeleteCard()
	out := Normalize(card)
	if out == card || len(card.Elements) != 5 || len(out.Elements) != 3 {
		t.Fatalf("source=%+v output=%+v", card, out)
	}
	form, ok := out.Elements[1].(common.CardForm)
	if !ok || form.Name != "delete_mode_form" || len(form.Elements) != 2 || len(form.Buttons) != 2 {
		t.Fatalf("form=%+v", out.Elements[1])
	}
	checkbox, ok := form.Elements[0].(common.CardCheckbox)
	if !ok || checkbox.Name != "delete_sel_73657373696f6e3a6f6e65" || !checkbox.Checked || checkbox.Text != "one" {
		t.Fatalf("checkbox=%+v", form.Elements[0])
	}
	if form.Buttons[0].Name != "delete_mode_submit" || form.Buttons[0].FormAction != "submit" || form.Buttons[0].Value != "act:/delete-mode form-submit" {
		t.Fatalf("submit=%+v", form.Buttons[0])
	}
	if out.Elements[2].(common.CardActions).Buttons[0].Value != "nav:next" {
		t.Fatal("navigation was lost")
	}
}
func TestNormalizeLeavesOrdinaryCardUntouched(t *testing.T) {
	card := common.NewCard().Markdown("ordinary").Build()
	if Normalize(card) != card {
		t.Fatal("ordinary card rewritten")
	}
	card = &common.Card{Elements: []common.CardElement{common.CardListItem{Text: "one", BtnValue: "custom"}, common.CardActions{Buttons: []common.CardButton{{Text: "Delete", Value: "act:/delete-mode confirm"}}}}}
	if Normalize(card) != card {
		t.Fatal("unrelated list card rewritten")
	}
}
func TestNormalizeLegacyActionPreservesCallbackEncoding(t *testing.T) {
	form := map[string]any{"delete_sel_62": true, "delete_sel_61": "true", "delete_sel_63": false, "ordinary": true}
	if got := NormalizeAction("delete_mode_submit", "", form); got != "act:/delete-mode form-submit a,b" {
		t.Fatalf("action=%q", got)
	}
	if got := NormalizeAction("delete_mode_cancel", "", form); got != "act:/delete-mode cancel" {
		t.Fatalf("action=%q", got)
	}
	if got := NormalizeAction("delete_mode_submit", "custom", form); got != "custom" {
		t.Fatalf("custom action changed: %q", got)
	}
	if !IgnoreLegacyToggle("act:/delete-mode toggle a") || IgnoreLegacyToggle("custom:toggle") {
		t.Fatal("toggle compatibility mismatch")
	}
}

func TestNormalizeActionDeduplicatesEquivalentEncodedIDs(t *testing.T) {
	got := NormalizeAction("delete_mode_submit", "", map[string]any{"delete_sel_4a": true, "delete_sel_4A": true})
	if got != "act:/delete-mode form-submit J" {
		t.Fatalf("action=%q", got)
	}
}
