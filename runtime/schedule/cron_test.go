package schedule_test

import (
	"testing"
	"time"

	rtschedule "github.com/lengzhao/agentkit/runtime/schedule"
)

func mustParse(t *testing.T, expr string) rtschedule.Schedule {
	t.Helper()
	s, err := rtschedule.ParseCron(expr)
	if err != nil {
		t.Fatalf("ParseCron(%q): %v", expr, err)
	}
	return s
}

func at(t *testing.T, value string) time.Time {
	t.Helper()
	parsed, err := time.ParseInLocation("2006-01-02 15:04", value, time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	return parsed
}

func TestNextComputesFollowingBoundary(t *testing.T) {
	t.Parallel()

	cases := []struct {
		expr string
		from string
		want string
	}{
		{"* * * * *", "2026-08-24 12:00", "2026-08-24 12:01"},
		{"0 * * * *", "2026-08-24 12:00", "2026-08-24 13:00"},
		{"30 9 * * *", "2026-08-24 12:00", "2026-08-25 09:30"},
		{"30 9 * * *", "2026-08-24 08:00", "2026-08-24 09:30"},
		{"*/15 * * * *", "2026-08-24 12:07", "2026-08-24 12:15"},
		{"*/15 * * * *", "2026-08-24 12:45", "2026-08-24 13:00"},
		{"0 0 1 * *", "2026-08-24 12:00", "2026-09-01 00:00"},
		{"0 0 1 1 *", "2026-08-24 12:00", "2027-01-01 00:00"},
		{"5,35 * * * *", "2026-08-24 12:10", "2026-08-24 12:35"},
		{"5,35 * * * *", "2026-08-24 12:40", "2026-08-24 13:05"},
		{"0 9-17 * * *", "2026-08-24 20:00", "2026-08-25 09:00"},
		{"0 9-17/4 * * *", "2026-08-24 10:00", "2026-08-24 13:00"},
		// A match exactly at the input must not be returned again.
		{"30 9 * * *", "2026-08-24 09:30", "2026-08-25 09:30"},
		// Seconds in the input are ignored, not rounded up past the boundary.
		{"* * * * *", "2026-08-24 12:00", "2026-08-24 12:01"},
	}
	for _, tc := range cases {
		got, ok := mustParse(t, tc.expr).Next(at(t, tc.from))
		if !ok {
			t.Errorf("%q from %s: no next time", tc.expr, tc.from)
			continue
		}
		if want := at(t, tc.want); !got.Equal(want) {
			t.Errorf("%q from %s = %s, want %s", tc.expr, tc.from, got.Format(time.RFC3339), want.Format(time.RFC3339))
		}
	}
}

func TestNextHandlesWeekdays(t *testing.T) {
	t.Parallel()

	// 2026-08-24 is a Monday.
	if got := at(t, "2026-08-24 12:00").Weekday(); got != time.Monday {
		t.Fatalf("fixture date is %s, expected Monday", got)
	}

	cases := []struct {
		expr string
		from string
		want string
	}{
		{"0 9 * * 1", "2026-08-24 12:00", "2026-08-31 09:00"},   // next Monday
		{"0 9 * * 2", "2026-08-24 12:00", "2026-08-25 09:00"},   // Tuesday
		{"0 9 * * 0", "2026-08-24 12:00", "2026-08-30 09:00"},   // Sunday as 0
		{"0 9 * * 7", "2026-08-24 12:00", "2026-08-30 09:00"},   // Sunday as 7
		{"0 9 * * sun", "2026-08-24 12:00", "2026-08-30 09:00"}, // Sunday by name
		{"0 9 * * 1-5", "2026-08-28 12:00", "2026-08-31 09:00"}, // Friday -> Monday
	}
	for _, tc := range cases {
		got, ok := mustParse(t, tc.expr).Next(at(t, tc.from))
		if !ok {
			t.Errorf("%q: no next time", tc.expr)
			continue
		}
		if want := at(t, tc.want); !got.Equal(want) {
			t.Errorf("%q from %s = %s, want %s", tc.expr, tc.from, got.Format(time.RFC3339), want.Format(time.RFC3339))
		}
	}
}

// TestDayOfMonthAndWeekAreOred pins the standard cron rule that trips most
// hand-rolled parsers: when both day fields are restricted, either one matching
// is enough.
func TestDayOfMonthAndWeekAreOred(t *testing.T) {
	t.Parallel()

	// "1st of the month, or any Friday".
	s := mustParse(t, "0 0 1 * 5")
	for _, day := range []string{"2026-08-28", "2026-09-01", "2026-09-04"} {
		when := at(t, day+" 00:00")
		if !s.Matches(when) {
			t.Errorf("%s should match (1st or Friday)", day)
		}
	}
	// A Tuesday that is not the 1st matches neither.
	if s.Matches(at(t, "2026-09-08 00:00")) {
		t.Error("2026-09-08 is a Tuesday and not the 1st, should not match")
	}

	// With only day-of-month restricted, weekday is irrelevant.
	domOnly := mustParse(t, "0 0 15 * *")
	if !domOnly.Matches(at(t, "2026-09-15 00:00")) {
		t.Error("day-of-month only should match the 15th")
	}
	if domOnly.Matches(at(t, "2026-09-16 00:00")) {
		t.Error("day-of-month only should not match the 16th")
	}
}

func TestShorthandsExpand(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"@hourly":  "2026-08-24 13:00",
		"@daily":   "2026-08-25 00:00",
		"@weekly":  "2026-08-30 00:00", // next Sunday
		"@monthly": "2026-09-01 00:00",
		"@yearly":  "2027-01-01 00:00",
	}
	from := at(t, "2026-08-24 12:30")
	for expr, want := range cases {
		got, ok := mustParse(t, expr).Next(from)
		if !ok {
			t.Errorf("%s: no next time", expr)
			continue
		}
		if !got.Equal(at(t, want)) {
			t.Errorf("%s = %s, want %s", expr, got.Format(time.RFC3339), want)
		}
	}
}

func TestMonthNamesAndRanges(t *testing.T) {
	t.Parallel()

	got, ok := mustParse(t, "0 0 1 mar *").Next(at(t, "2026-08-24 12:00"))
	if !ok {
		t.Fatal("no next time")
	}
	if want := at(t, "2027-03-01 00:00"); !got.Equal(want) {
		t.Fatalf("= %s, want %s", got.Format(time.RFC3339), want.Format(time.RFC3339))
	}

	// Quarterly.
	q := mustParse(t, "0 0 1 1,4,7,10 *")
	got, ok = q.Next(at(t, "2026-08-24 12:00"))
	if !ok {
		t.Fatal("no next time")
	}
	if want := at(t, "2026-10-01 00:00"); !got.Equal(want) {
		t.Fatalf("quarterly = %s, want %s", got.Format(time.RFC3339), want.Format(time.RFC3339))
	}
}

// TestImpossibleDateReportsFailure covers the reason Next is bounded: without a
// horizon an unsatisfiable expression would spin forever.
func TestImpossibleDateReportsFailure(t *testing.T) {
	t.Parallel()

	if _, ok := mustParse(t, "0 0 30 2 *").Next(at(t, "2026-08-24 12:00")); ok {
		t.Fatal("February 30th should never match")
	}
}

func TestNextSkipsShortMonths(t *testing.T) {
	t.Parallel()

	// The 31st exists only in some months.
	got, ok := mustParse(t, "0 0 31 * *").Next(at(t, "2026-04-15 12:00"))
	if !ok {
		t.Fatal("no next time")
	}
	if want := at(t, "2026-05-31 00:00"); !got.Equal(want) {
		t.Fatalf("= %s, want %s (April has 30 days)", got.Format(time.RFC3339), want.Format(time.RFC3339))
	}
}

func TestParseRejectsBadExpressions(t *testing.T) {
	t.Parallel()

	bad := []string{
		"",
		"* * * *",           // too few fields
		"* * * * * *",       // too many
		"60 * * * *",        // minute out of range
		"* 24 * * *",        // hour out of range
		"* * 0 * *",         // day-of-month is 1-based
		"* * * 13 *",        // month out of range
		"* * * * 8",         // day-of-week out of range
		"*/0 * * * *",       // zero step
		"5-1 * * * *",       // inverted range
		"abc * * * *",       // not a number
		"* * * notamonth *", // unknown name
		"/5 * * * *",        // step with no range
	}
	for _, expr := range bad {
		if _, err := rtschedule.ParseCron(expr); err == nil {
			t.Errorf("ParseCron(%q) should have failed", expr)
		}
	}
}

func TestStepFromValueRunsToEndOfField(t *testing.T) {
	t.Parallel()

	// "10/20" means 10, 30, 50 within the minute field.
	s := mustParse(t, "10/20 * * * *")
	from := at(t, "2026-08-24 12:00")
	want := []string{"2026-08-24 12:10", "2026-08-24 12:30", "2026-08-24 12:50", "2026-08-24 13:10"}
	cursor := from
	for _, expect := range want {
		next, ok := s.Next(cursor)
		if !ok {
			t.Fatalf("no next time after %s", cursor.Format(time.RFC3339))
		}
		if !next.Equal(at(t, expect)) {
			t.Fatalf("= %s, want %s", next.Format(time.RFC3339), expect)
		}
		cursor = next
	}
}

func TestStringRoundTripsTheInput(t *testing.T) {
	t.Parallel()

	if got := mustParse(t, " 0 9 * * 1-5 ").String(); got != "0 9 * * 1-5" {
		t.Fatalf("String() = %q", got)
	}
}

func TestNextIsStableAcrossRepeatedCalls(t *testing.T) {
	t.Parallel()

	// Walking a full day of a 15-minute schedule must produce exactly 96 fires,
	// which catches off-by-one errors in the minute stepping.
	s := mustParse(t, "*/15 * * * *")
	cursor := at(t, "2026-08-24 00:00")
	end := at(t, "2026-08-25 00:00")
	count := 0
	for {
		next, ok := s.Next(cursor)
		if !ok {
			t.Fatal("no next time")
		}
		if !next.Before(end) {
			break
		}
		count++
		cursor = next
	}
	if count != 95 {
		t.Fatalf("fires between 00:00 exclusive and next midnight = %d, want 95", count)
	}
}
