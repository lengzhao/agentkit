package learning

import "testing"

func TestFormatBackgroundReviewNotification(t *testing.T) {
	if FormatBackgroundReviewNotification("off", []string{"x"}) != "" {
		t.Fatal("off should be silent")
	}
	if got := FormatBackgroundReviewNotification("on", []string{"memory updated [10/2200]"}); got != "💾 Memory updated" {
		t.Fatalf("on = %q", got)
	}
	if got := FormatBackgroundReviewNotification("verbose", []string{"User prefers terse replies"}); got != "💾 Memory ➕ User prefers terse replies" {
		t.Fatalf("verbose = %q", got)
	}
	if got := FormatBackgroundReviewNotification("on", []string{"memory staged for approval (id x)"}); got != "💾 Memory pending approval" {
		t.Fatalf("staged on = %q", got)
	}
	if got := FormatBackgroundReviewNotification("on", []string{"memory staged for approval (id x)", "personal memory updated"}); got != "💾 Memory updated" {
		t.Fatalf("mixed on = %q", got)
	}
	if got := FormatBackgroundReviewNotification("on", []string{"personal memory updated"}); got != "💾 Memory updated" {
		t.Fatalf("committed on = %q", got)
	}
}
