package deferred

import (
	"regexp"
	"strings"

	"github.com/lengzhao/agentkit"
)

var tokenRe = regexp.MustCompile(`[A-Za-z0-9]+`)

type catalogEntry struct {
	Spec        agentkit.ToolSpec
	Tokens      []string
	SearchText  string
	SourceLabel string
}

func buildCatalog(specs []agentkit.ToolSpec) []catalogEntry {
	out := make([]catalogEntry, 0, len(specs))
	for _, spec := range specs {
		label := sourceLabel(spec.Name)
		text := searchText(spec, label)
		out = append(out, catalogEntry{
			Spec:        spec,
			Tokens:      tokenize(text),
			SearchText:  text,
			SourceLabel: label,
		})
	}
	return out
}

func sourceLabel(name string) string {
	if i := strings.Index(name, "__"); i > 0 {
		return strings.ReplaceAll(name[:i], "_", " ")
	}
	if i := strings.Index(name, "_"); i > 0 {
		return name[:i]
	}
	return ""
}

func searchText(spec agentkit.ToolSpec, sourceLabel string) string {
	name := spec.Name
	if strings.Contains(name, "__") {
		name = strings.ReplaceAll(strings.ReplaceAll(name, "__", " "), "_", " ")
	} else {
		name = strings.ReplaceAll(name, "_", " ")
	}
	desc := spec.Description
	var params string
	for k := range spec.InputSchema.Properties {
		params += " " + k
	}
	extra := ""
	if sourceLabel != "" && !strings.Contains(strings.ToLower(name), strings.ToLower(sourceLabel)) {
		extra = sourceLabel
	}
	return strings.TrimSpace(name + " " + extra + " " + desc + params)
}

func tokenize(text string) []string {
	raw := tokenRe.FindAllString(strings.ToLower(text), -1)
	if len(raw) == 0 {
		return nil
	}
	return raw
}

func scoreQuery(query string, entry catalogEntry) int {
	qTokens := tokenize(query)
	if len(qTokens) == 0 {
		return 0
	}
	score := 0
	for _, qt := range qTokens {
		for _, dt := range entry.Tokens {
			if dt == qt {
				score += 3
				continue
			}
			if strings.Contains(dt, qt) || strings.Contains(qt, dt) {
				score++
			}
		}
		if strings.Contains(strings.ToLower(entry.Spec.Name), qt) {
			score += 2
		}
	}
	return score
}

func describeCatalog(catalog []catalogEntry, names []string) (map[string]agentkit.ToolSpec, []string) {
	byName := make(map[string]agentkit.ToolSpec, len(catalog))
	for _, e := range catalog {
		byName[e.Spec.Name] = e.Spec
	}
	found := make(map[string]agentkit.ToolSpec)
	var notFound []string
	for _, name := range names {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		if spec, ok := byName[name]; ok {
			found[name] = spec
		} else {
			notFound = append(notFound, name)
		}
	}
	return found, notFound
}

func listingText(catalog []catalogEntry, maxChars int) string {
	if maxChars <= 0 || len(catalog) == 0 {
		return ""
	}
	var b strings.Builder
	bySource := map[string][]catalogEntry{}
	var order []string
	for _, e := range catalog {
		key := e.SourceLabel
		if key == "" {
			key = "other"
		}
		if _, ok := bySource[key]; !ok {
			order = append(order, key)
		}
		bySource[key] = append(bySource[key], e)
	}
	for _, key := range order {
		group := bySource[key]
		if b.Len() > 0 {
			b.WriteByte('\n')
		}
		b.WriteString("## ")
		b.WriteString(key)
		b.WriteString(" (")
		b.WriteString(itoa(len(group)))
		b.WriteString(" tools)\n")
		for _, e := range group {
			line := "- " + e.Spec.Name
			short := firstSentence(e.Spec.Description, 60)
			if short != "" {
				line += ": " + short
			}
			line += "\n"
			if b.Len()+len(line) > maxChars {
				return strings.TrimSpace(b.String()) + "\n…"
			}
			b.WriteString(line)
		}
	}
	return strings.TrimSpace(b.String())
}

func firstSentence(text string, max int) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	end := len(text)
	for i, r := range text {
		if r == '.' || r == '。' || r == '\n' {
			end = i
			break
		}
	}
	text = strings.TrimSpace(text[:end])
	if len(text) > max {
		return strings.TrimSpace(text[:max]) + "…"
	}
	return text
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}

func catalogNamesSet(catalog []catalogEntry) map[string]bool {
	out := make(map[string]bool, len(catalog))
	for _, e := range catalog {
		out[e.Spec.Name] = true
	}
	return out
}
