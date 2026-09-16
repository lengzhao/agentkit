package deferred

import "strings"

// lightStem trims common English suffixes for BM25/token recall (not full Porter).
func lightStem(token string) string {
	t := strings.ToLower(strings.TrimSpace(token))
	if len(t) < 4 {
		return t
	}
	suffixes := []string{"ingly", "edly", "ing", "tion", "ment", "ness", "able", "ible", "ed", "es", "er", "ly", "s"}
	for _, suf := range suffixes {
		if len(t) <= len(suf)+2 {
			continue
		}
		if strings.HasSuffix(t, suf) {
			return t[:len(t)-len(suf)]
		}
	}
	return t
}

func stemTokens(tokens []string) []string {
	if len(tokens) == 0 {
		return nil
	}
	out := make([]string, 0, len(tokens))
	for _, t := range tokens {
		s := lightStem(t)
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}
