package deferred

import (
	"strings"
	"unicode"
)

// isCJKRune reports Han script used for catalog/search (CJK Unified + ext A).
func isCJKRune(r rune) bool {
	return unicode.Is(unicode.Han, r)
}

// isPureCJKQuery is true when the query has at least one Han character and no ASCII letters.
func isPureCJKQuery(q string) bool {
	q = strings.TrimSpace(q)
	if q == "" {
		return false
	}
	hasHan := false
	for _, r := range q {
		if isCJKRune(r) {
			hasHan = true
			continue
		}
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' {
			return false
		}
	}
	return hasHan
}

func extractCJKRunes(s string) []rune {
	out := make([]rune, 0, len(s))
	for _, r := range s {
		if isCJKRune(r) {
			out = append(out, r)
		}
	}
	return out
}

func cjkBigramCounts(run []rune) map[string]int {
	if len(run) < 2 {
		return nil
	}
	m := make(map[string]int, len(run)-1)
	for i := 0; i+1 < len(run); i++ {
		bg := string(run[i : i+2])
		m[bg]++
	}
	return m
}

func buildCJKBigrames(name, searchText string) map[string]int {
	run := extractCJKRunes(name + searchText)
	return cjkBigramCounts(run)
}

// scoreCJK2Gram scores overlap between query and catalog entry CJK bigrams.
func scoreCJK2Gram(query string, entry catalogEntry) float64 {
	qRunes := extractCJKRunes(query)
	if len(qRunes) == 0 {
		return 0
	}
	if len(qRunes) == 1 {
		ch := string(qRunes[0])
		if strings.Contains(entry.Spec.Name, ch) || strings.Contains(entry.SearchText, ch) {
			return 2
		}
		return 0
	}
	qBg := cjkBigramCounts(qRunes)
	if len(qBg) == 0 || len(entry.CJKBigrames) == 0 {
		return 0
	}
	var overlap float64
	for bg, qf := range qBg {
		if ef, ok := entry.CJKBigrames[bg]; ok {
			if qf < ef {
				overlap += float64(qf)
			} else {
				overlap += float64(ef)
			}
		}
	}
	return overlap
}

// scoreCJKSubstring supplements scoreSubstring for Han text (rune-wise contains).
func scoreCJKSubstring(query string, entry catalogEntry) float64 {
	qRunes := extractCJKRunes(strings.TrimSpace(query))
	if len(qRunes) == 0 {
		return 0
	}
	q := string(qRunes)
	name := entry.Spec.Name
	text := entry.SearchText
	switch {
	case strings.Contains(name, q):
		return 4
	case strings.Contains(text, q):
		return 3
	default:
		return 0
	}
}
