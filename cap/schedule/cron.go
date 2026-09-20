package schedule

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Field bounds, matching standard 5-field cron.
const (
	minuteMin, minuteMax = 0, 59
	hourMin, hourMax     = 0, 23
	domMin, domMax       = 1, 31
	monthMin, monthMax   = 1, 12
	dowMin, dowMax       = 0, 6 // 0 = Sunday; 7 is accepted as Sunday too
)

// searchYears bounds Next so an unsatisfiable date (say "30 2 *", Feb 30) reports
// failure instead of looping forever.
const searchYears = 5

// Schedule is a parsed cron expression. Times are interpreted in the location of
// whatever time.Time it is asked about, so a schedule built from local time
// behaves like an OS crontab entry.
type Schedule struct {
	expr   string
	minute uint64
	hour   uint64
	dom    uint64
	month  uint64
	dow    uint64
	// Standard cron ORs day-of-month with day-of-week when both are restricted,
	// and ANDs them with the rest of the fields. Tracking "restricted" is what
	// makes that rule expressible.
	domRestricted bool
	dowRestricted bool
}

func (s Schedule) String() string { return s.expr }

// shorthands expand the common @-forms before field parsing.
var shorthands = map[string]string{
	"@yearly":   "0 0 1 1 *",
	"@annually": "0 0 1 1 *",
	"@monthly":  "0 0 1 * *",
	"@weekly":   "0 0 * * 0",
	"@daily":    "0 0 * * *",
	"@midnight": "0 0 * * *",
	"@hourly":   "0 * * * *",
}

// ParseCron parses a 5-field cron expression: minute hour day-of-month month
// day-of-week. Each field accepts `*`, a number, `a-b`, comma-separated lists,
// and `*/n` or `a-b/n` steps. The @hourly/@daily/@weekly/@monthly/@yearly
// shorthands are also accepted.
func ParseCron(expr string) (Schedule, error) {
	raw := strings.TrimSpace(expr)
	if raw == "" {
		return Schedule{}, fmt.Errorf("empty cron expression")
	}
	if expanded, ok := shorthands[strings.ToLower(raw)]; ok {
		raw = expanded
	}
	fields := strings.Fields(raw)
	if len(fields) != 5 {
		return Schedule{}, fmt.Errorf("cron expression %q has %d fields, want 5 (minute hour day-of-month month day-of-week)", expr, len(fields))
	}

	out := Schedule{expr: strings.TrimSpace(expr)}
	var err error
	if out.minute, _, err = parseField(fields[0], minuteMin, minuteMax, nil); err != nil {
		return Schedule{}, fmt.Errorf("minute field: %w", err)
	}
	if out.hour, _, err = parseField(fields[1], hourMin, hourMax, nil); err != nil {
		return Schedule{}, fmt.Errorf("hour field: %w", err)
	}
	if out.dom, out.domRestricted, err = parseField(fields[2], domMin, domMax, nil); err != nil {
		return Schedule{}, fmt.Errorf("day-of-month field: %w", err)
	}
	if out.month, _, err = parseField(fields[3], monthMin, monthMax, monthNames); err != nil {
		return Schedule{}, fmt.Errorf("month field: %w", err)
	}
	if out.dow, out.dowRestricted, err = parseField(fields[4], dowMin, dowMax, dayNames); err != nil {
		return Schedule{}, fmt.Errorf("day-of-week field: %w", err)
	}
	return out, nil
}

var monthNames = map[string]int{
	"jan": 1, "feb": 2, "mar": 3, "apr": 4, "may": 5, "jun": 6,
	"jul": 7, "aug": 8, "sep": 9, "oct": 10, "nov": 11, "dec": 12,
}

var dayNames = map[string]int{
	"sun": 0, "mon": 1, "tue": 2, "wed": 3, "thu": 4, "fri": 5, "sat": 6,
}

// parseField returns the allowed values as a bitmask, plus whether the field was
// restricted (anything other than a bare `*`).
func parseField(field string, min, max int, names map[string]int) (uint64, bool, error) {
	field = strings.TrimSpace(field)
	if field == "" {
		return 0, false, fmt.Errorf("empty field")
	}
	if field == "*" {
		return fullMask(min, max), false, nil
	}

	var mask uint64
	for _, part := range strings.Split(field, ",") {
		bits, err := parseRange(strings.TrimSpace(part), min, max, names)
		if err != nil {
			return 0, false, err
		}
		mask |= bits
	}
	if mask == 0 {
		return 0, false, fmt.Errorf("field %q matches nothing", field)
	}
	return mask, true, nil
}

func parseRange(part string, min, max int, names map[string]int) (uint64, error) {
	step := 1
	if slash := strings.Index(part, "/"); slash >= 0 {
		stepText := part[slash+1:]
		part = part[:slash]
		parsed, err := strconv.Atoi(stepText)
		if err != nil || parsed <= 0 {
			return 0, fmt.Errorf("invalid step %q", stepText)
		}
		step = parsed
		if part == "" {
			return 0, fmt.Errorf("step needs a range or * before /")
		}
	}

	lo, hi := min, max
	switch {
	case part == "*":
		// keep the full range
	case strings.Contains(part, "-"):
		bounds := strings.SplitN(part, "-", 2)
		var err error
		if lo, err = parseValue(bounds[0], min, max, names); err != nil {
			return 0, err
		}
		if hi, err = parseValue(bounds[1], min, max, names); err != nil {
			return 0, err
		}
		if lo > hi {
			return 0, fmt.Errorf("range %q is inverted", part)
		}
	default:
		value, err := parseValue(part, min, max, names)
		if err != nil {
			return 0, err
		}
		lo, hi = value, value
		// `N/step` means "from N to the end of the field, every step".
		if step > 1 {
			hi = max
		}
	}

	var mask uint64
	for v := lo; v <= hi; v += step {
		mask |= 1 << uint(v)
	}
	return mask, nil
}

func parseValue(text string, min, max int, names map[string]int) (int, error) {
	text = strings.TrimSpace(text)
	if names != nil {
		if v, ok := names[strings.ToLower(text)]; ok {
			return v, nil
		}
	}
	v, err := strconv.Atoi(text)
	if err != nil {
		return 0, fmt.Errorf("invalid value %q", text)
	}
	// Sunday is both 0 and 7 in every cron dialect.
	if min == dowMin && max == dowMax && v == 7 {
		v = 0
	}
	if v < min || v > max {
		return 0, fmt.Errorf("value %d out of range %d-%d", v, min, max)
	}
	return v, nil
}

func fullMask(min, max int) uint64 {
	var mask uint64
	for v := min; v <= max; v++ {
		mask |= 1 << uint(v)
	}
	return mask
}

func isSet(mask uint64, v int) bool {
	if v < 0 || v > 63 {
		return false
	}
	return mask&(1<<uint(v)) != 0
}

// Matches reports whether t (to the minute) satisfies the schedule.
func (s Schedule) Matches(t time.Time) bool {
	if !s.matchesMonth(t) || !s.matchesDate(t) {
		return false
	}
	return isSet(s.hour, t.Hour()) && isSet(s.minute, t.Minute())
}

func (s Schedule) matchesMonth(t time.Time) bool {
	return isSet(s.month, int(t.Month()))
}

// matchesDate applies the day-of-month / day-of-week rule: when both fields are
// restricted a match on either one is enough, which is what standard cron does.
func (s Schedule) matchesDate(t time.Time) bool {
	domOK := isSet(s.dom, t.Day())
	dowOK := isSet(s.dow, int(t.Weekday()))
	switch {
	case s.domRestricted && s.dowRestricted:
		return domOK || dowOK
	case s.domRestricted:
		return domOK
	case s.dowRestricted:
		return dowOK
	default:
		return true
	}
}

// Next returns the first matching minute strictly after t. The second result is
// false when nothing matches within the search horizon, which only happens for
// impossible dates such as "0 0 30 2 *".
func (s Schedule) Next(t time.Time) (time.Time, bool) {
	if s.minute == 0 {
		return time.Time{}, false
	}
	// Start at the next whole minute so a match at t itself is not returned.
	cursor := t.Truncate(time.Minute).Add(time.Minute)
	limit := cursor.AddDate(searchYears, 0, 0)

	for cursor.Before(limit) {
		// Skip coarse to fine: whole months, then whole days, then minutes.
		if !s.matchesMonth(cursor) {
			cursor = startOfNextMonth(cursor)
			continue
		}
		if !s.matchesDate(cursor) {
			cursor = startOfNextDay(cursor)
			continue
		}
		if !isSet(s.hour, cursor.Hour()) {
			cursor = startOfNextHour(cursor)
			continue
		}
		if isSet(s.minute, cursor.Minute()) {
			return cursor, true
		}
		cursor = cursor.Add(time.Minute)
	}
	return time.Time{}, false
}

func startOfNextMonth(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, t.Location()).AddDate(0, 1, 0)
}

func startOfNextDay(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location()).AddDate(0, 0, 1)
}

func startOfNextHour(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), 0, 0, 0, t.Location()).Add(time.Hour)
}
