package web

import (
	"strconv"
	"strings"
	"unicode/utf8"
)

// skipContainers hold markup whose text is never page content.
var skipContainers = map[string]bool{
	"script": true, "style": true, "noscript": true, "svg": true,
	"head": true, "template": true, "iframe": true,
}

// blockTags end a line when opened or closed, so extracted text keeps the
// document's paragraph structure instead of running together.
var blockTags = map[string]bool{
	"br": true, "hr": true,
	"p": true, "/p": true, "div": true, "/div": true,
	"li": true, "/li": true, "/ul": true, "/ol": true,
	"tr": true, "/tr": true, "/table": true,
	"h1": true, "h2": true, "h3": true, "h4": true, "h5": true, "h6": true,
	"/h1": true, "/h2": true, "/h3": true, "/h4": true, "/h5": true, "/h6": true,
	"/section": true, "/article": true, "/header": true, "/footer": true,
	"/blockquote": true, "/pre": true, "/main": true, "/nav": true,
}

func htmlToText(doc string) string {
	var b strings.Builder
	b.Grow(len(doc) / 2)
	lower := strings.ToLower(doc)
	var last byte
	write := func(s string) {
		if s == "" {
			return
		}
		b.WriteString(s)
		last = s[len(s)-1]
	}
	writeBreak := func(sep byte) {
		if b.Len() == 0 || last == '\n' || last == sep {
			return
		}
		b.WriteByte(sep)
		last = sep
	}

	for i := 0; i < len(doc); {
		if doc[i] != '<' {
			j := strings.IndexByte(doc[i:], '<')
			if j < 0 {
				write(doc[i:])
				break
			}
			write(doc[i : i+j])
			i += j
			continue
		}
		if strings.HasPrefix(lower[i:], "<!--") {
			end := strings.Index(lower[i+4:], "-->")
			if end < 0 {
				break
			}
			i += 4 + end + 3
			continue
		}
		name, end := tagAt(lower, i)
		if end < 0 {
			break
		}
		switch {
		case skipContainers[name]:
			i = skipContainer(lower, name, end)
			continue
		case blockTags[name]:
			writeBreak('\n')
		case name == "td" || name == "/td" || name == "th" || name == "/th":
			writeBreak(' ')
		}
		i = end
	}
	return collapse(decodeEntities(b.String()))
}

func skipContainer(lower, name string, from int) int {
	closeTag := "</" + name
	k := strings.Index(lower[from:], closeTag)
	if k < 0 {
		return len(lower)
	}
	gt := strings.IndexByte(lower[from+k:], '>')
	if gt < 0 {
		return len(lower)
	}
	return from + k + gt + 1
}

func tagAt(lower string, i int) (string, int) {
	j := i + 1
	closing := j < len(lower) && lower[j] == '/'
	if closing {
		j++
	}
	start := j
	for j < len(lower) && (lower[j] >= 'a' && lower[j] <= 'z' || lower[j] >= '0' && lower[j] <= '9') {
		j++
	}
	name := lower[start:j]
	if closing {
		name = "/" + name
	}

	var quote byte
	for k := j; k < len(lower); k++ {
		c := lower[k]
		if quote != 0 {
			if c == quote {
				quote = 0
			}
			continue
		}
		switch c {
		case '"', '\'':
			quote = c
		case '>':
			return name, k + 1
		}
	}
	return name, -1
}

func extractTitle(doc string) string {
	lower := strings.ToLower(doc)
	open := strings.Index(lower, "<title")
	if open < 0 {
		return ""
	}
	_, start := tagAt(lower, open)
	if start < 0 {
		return ""
	}
	end := strings.Index(lower[start:], "</title")
	if end < 0 {
		return ""
	}
	return strings.Join(strings.Fields(decodeEntities(doc[start:start+end])), " ")
}

var namedEntities = map[string]string{
	"amp": "&", "lt": "<", "gt": ">", "quot": `"`, "apos": "'",
	"nbsp": " ", "mdash": "—", "ndash": "–", "hellip": "…",
	"lsquo": "‘", "rsquo": "’", "ldquo": "“", "rdquo": "”",
	"copy": "©", "reg": "®", "trade": "™", "middot": "·", "bull": "•",
}

func decodeEntities(s string) string {
	if !strings.ContainsRune(s, '&') {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); {
		if s[i] != '&' {
			b.WriteByte(s[i])
			i++
			continue
		}
		semi := strings.IndexByte(s[i:], ';')
		if semi < 0 || semi > 12 {
			b.WriteByte(s[i])
			i++
			continue
		}
		body := s[i+1 : i+semi]
		if decoded, ok := decodeEntity(body); ok {
			b.WriteString(decoded)
			i += semi + 1
			continue
		}
		b.WriteByte(s[i])
		i++
	}
	return b.String()
}

func decodeEntity(body string) (string, bool) {
	if body == "" {
		return "", false
	}
	if body[0] == '#' {
		digits, base := body[1:], 10
		if len(digits) > 0 && (digits[0] == 'x' || digits[0] == 'X') {
			digits, base = digits[1:], 16
		}
		code, err := strconv.ParseInt(digits, base, 32)
		if err != nil || code <= 0 || code > utf8.MaxRune {
			return "", false
		}
		return string(rune(code)), true
	}
	if v, ok := namedEntities[strings.ToLower(body)]; ok {
		return v, true
	}
	return "", false
}

func collapse(s string) string {
	lines := strings.Split(s, "\n")
	out := make([]string, 0, len(lines))
	blank := false
	for _, line := range lines {
		line = strings.Join(strings.Fields(line), " ")
		if line == "" {
			if len(out) > 0 {
				blank = true
			}
			continue
		}
		if blank {
			out = append(out, "")
			blank = false
		}
		out = append(out, line)
	}
	return strings.Join(out, "\n")
}
