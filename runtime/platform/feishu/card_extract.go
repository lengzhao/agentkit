package feishu

import (
	"encoding/json"
	"fmt"
	"strings"
)

// extractInteractiveCardText converts a Feishu interactive card JSON into
// readable markdown-ish text for the agent.
// With raw_card_content, the response wraps the card in {"json_card": "...", ...}.
// Supports schema 2.0 (header.property + body.property.elements with recursive
// nesting) and legacy format (top-level title + elements).
//
// Besides text, it keeps what the agent needs to act on a card: header
// title/subtitle/tags, link and button URLs, image alt text, column_set and
// collapsible_panel contents, and inline bold/italic/strikethrough.
func extractInteractiveCardText(content string) string {
	// Try raw_card_content format: {"json_card": "<escaped JSON>", ...}
	var wrapper struct {
		JsonCard string `json:"json_card"`
	}
	cardJSON := content
	if json.Unmarshal([]byte(content), &wrapper) == nil && wrapper.JsonCard != "" {
		cardJSON = wrapper.JsonCard
	}

	var card map[string]json.RawMessage
	if err := json.Unmarshal([]byte(cardJSON), &card); err != nil {
		return "[interactive card]"
	}

	var parts []string
	if raw, ok := card["header"]; ok {
		extractCardHeader(raw, &parts)
	}
	headerParts := len(parts)

	// Schema 2.0: body may use property.elements (standard) or direct elements (simplified).
	if raw, ok := card["body"]; ok {
		var body struct {
			Elements []json.RawMessage `json:"elements"`
			Property struct {
				Elements []json.RawMessage `json:"elements"`
			} `json:"property"`
		}
		if json.Unmarshal(raw, &body) == nil {
			if len(body.Property.Elements) > 0 {
				extractCardElements(body.Property.Elements, &parts)
			} else if len(body.Elements) > 0 {
				extractCardElements(body.Elements, &parts)
			}
		}
	}

	// Legacy: direct title string + flat/nested elements.
	if len(parts) == headerParts {
		if headerParts == 0 {
			if raw, ok := card["title"]; ok {
				var title string
				if json.Unmarshal(raw, &title) == nil && title != "" {
					parts = append(parts, title)
				}
			}
		}
		if raw, ok := card["elements"]; ok {
			var elements []json.RawMessage
			if json.Unmarshal(raw, &elements) == nil {
				extractCardElements(elements, &parts)
			}
		}
	}

	if len(parts) == 0 {
		return "[interactive card]"
	}
	return strings.Join(parts, "\n")
}

// cardElem is the common shape of a card element. Schema 2.0 raw cards keep
// attributes under property; legacy and simplified cards keep them at the top
// level, so both are decoded and get checks property first.
type cardElem struct {
	Tag      string
	top      map[string]json.RawMessage
	property map[string]json.RawMessage
}

func parseCardElem(raw json.RawMessage) (cardElem, bool) {
	var e cardElem
	if json.Unmarshal(raw, &e.top) != nil || e.top == nil {
		return e, false
	}
	_ = json.Unmarshal(e.top["tag"], &e.Tag)
	_ = json.Unmarshal(e.top["property"], &e.property)
	return e, true
}

func (e cardElem) get(key string) json.RawMessage {
	if v, ok := e.property[key]; ok {
		return v
	}
	return e.top[key]
}

func (e cardElem) str(key string) string {
	var s string
	_ = json.Unmarshal(e.get(key), &s)
	return s
}

func (e cardElem) list(key string) []json.RawMessage {
	var l []json.RawMessage
	_ = json.Unmarshal(e.get(key), &l)
	return l
}

// extractCardHeader renders the card header: title, text tags and subtitle.
// Handles schema 2.0 raw ({property:{title:{property:{content}}}}) and legacy
// ({title:{content}}) headers.
func extractCardHeader(raw json.RawMessage, parts *[]string) {
	h, ok := parseCardElem(raw)
	if !ok {
		return
	}
	title := strings.TrimSpace(cardInlineText(h.get("title")))
	var tags []string
	for _, key := range []string{"textTagList", "text_tag_list"} {
		for _, t := range h.list(key) {
			if s := strings.TrimSpace(cardInlineText(t)); s != "" {
				tags = append(tags, s)
			}
		}
	}
	if title != "" || len(tags) > 0 {
		line := strings.TrimSpace("## " + strings.TrimSpace(title+" "+strings.Join(tags, " ")))
		*parts = append(*parts, line)
	}
	if sub := strings.TrimSpace(cardInlineText(h.get("subtitle"))); sub != "" {
		*parts = append(*parts, sub)
	}
}

// cardInlineText renders a text-like element (plain_text, markdown runs,
// link, at, text_tag ...) as a single inline string.
func cardInlineText(raw json.RawMessage) string {
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	e, ok := parseCardElem(raw)
	if !ok {
		return ""
	}
	switch e.Tag {
	case "br":
		return "\n"
	case "hr":
		return "\n---\n"
	case "code_span":
		if c := e.str("content"); c != "" {
			return "`" + c + "`"
		}
		return ""
	case "link", "a":
		text := cardInlineText(e.get("content"))
		if text == "" {
			text = cardInlineText(e.get("text"))
		}
		url := cardURL(e.get("url"))
		if url == "" {
			url = cardURL(e.get("href"))
		}
		if url == "" {
			return text
		}
		if text == "" {
			text = url
		}
		return "[" + text + "](" + url + ")"
	case "at":
		if name := e.str("user_name"); name != "" {
			return "@" + name
		}
		id := e.str("userID")
		if id == "" {
			id = e.str("user_id")
		}
		if id != "" {
			return "@" + id
		}
		return ""
	case "text_tag":
		if t := strings.TrimSpace(cardInlineText(e.get("text"))); t != "" {
			return "[" + t + "]"
		}
		return ""
	case "img", "image":
		return cardImageText(e)
	case "standard_icon", "ud_icon":
		return ""
	case "list":
		if lines := cardListLines(e.get("items")); len(lines) > 0 {
			return "\n" + strings.Join(lines, "\n") + "\n"
		}
		return ""
	case "code_block":
		if code := cardCodeBlock(e); code != "" {
			return "\n" + code + "\n"
		}
		return ""
	}

	var b strings.Builder
	if c := e.get("content"); len(c) > 0 {
		b.WriteString(applyCardTextStyle(cardInlineText(c), e.get("textStyle")))
	} else if t := e.get("text"); len(t) > 0 {
		b.WriteString(cardInlineText(t))
	}
	for _, child := range e.list("elements") {
		b.WriteString(cardInlineText(child))
	}
	return b.String()
}

// applyCardTextStyle wraps text with markdown markers for bold/italic/strikethrough.
func applyCardTextStyle(text string, styleRaw json.RawMessage) string {
	core := strings.TrimSpace(text)
	if core == "" || len(styleRaw) == 0 {
		return text
	}
	var style struct {
		Attributes []string `json:"attributes"`
	}
	if json.Unmarshal(styleRaw, &style) != nil {
		return text
	}
	styled := core
	for _, a := range style.Attributes {
		switch a {
		case "bold":
			styled = "**" + styled + "**"
		case "italic":
			styled = "*" + styled + "*"
		case "strikethrough", "lineThrough":
			styled = "~~" + styled + "~~"
		}
	}
	// Keep surrounding whitespace outside the markers so markdown stays valid.
	i := strings.Index(text, core)
	return text[:i] + styled + text[i+len(core):]
}

// cardURL accepts either a URL string or an object like {"url": "..."} /
// {"pc_url": "...", "android_url": ...}.
func cardURL(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var obj map[string]json.RawMessage
	if json.Unmarshal(raw, &obj) != nil {
		return ""
	}
	for _, key := range []string{"url", "pc_url", "pcUrl", "default_url"} {
		if v := cardURL(obj[key]); v != "" {
			return v
		}
	}
	return ""
}

func cardImageText(e cardElem) string {
	alt := strings.TrimSpace(cardInlineText(e.get("alt")))
	if alt == "" {
		alt = strings.TrimSpace(cardInlineText(e.get("title")))
	}
	if alt == "" {
		return "[Image]"
	}
	return "[Image: " + alt + "]"
}

// cardButtonText renders a button as "[Button: label](url)".
func cardButtonText(e cardElem) string {
	label := strings.TrimSpace(cardInlineText(e.get("text")))
	url := cardURL(e.get("url"))
	if url == "" {
		url = cardURL(e.get("multi_url"))
	}
	if url == "" {
		for _, a := range e.list("actions") {
			var act struct {
				Action json.RawMessage `json:"action"`
			}
			if json.Unmarshal(a, &act) == nil {
				if u := cardURL(act.Action); u != "" {
					url = u
					break
				}
			}
		}
	}
	if label == "" {
		label = url
	}
	if label == "" {
		return ""
	}
	if url == "" {
		return "[Button: " + label + "]"
	}
	return "[Button: " + label + "](" + url + ")"
}

// extractCardElements recursively renders schema 2.0 / legacy card elements
// as block-level text, one entry per block. Legacy cards may nest elements
// as rows ([][]element); rows are flattened.
func extractCardElements(elements []json.RawMessage, parts *[]string) {
	appendText := func(s string) {
		if s = strings.TrimSpace(s); s != "" {
			*parts = append(*parts, s)
		}
	}
	for _, raw := range elements {
		var row []json.RawMessage
		if json.Unmarshal(raw, &row) == nil {
			// Legacy row: its elements form one line.
			var line strings.Builder
			for _, el := range row {
				line.WriteString(cardInlineText(el))
			}
			appendText(line.String())
			continue
		}
		e, ok := parseCardElem(raw)
		if !ok {
			continue
		}
		switch e.Tag {
		case "code_block":
			if code := cardCodeBlock(e); code != "" {
				*parts = append(*parts, code)
			}
		case "hr":
			*parts = append(*parts, "---")
		case "table":
			extractCardTable(e.get("columns"), e.get("rows"), parts)
		case "list":
			*parts = append(*parts, cardListLines(e.get("items"))...)
		case "button":
			appendText(cardButtonText(e))
		case "img", "image":
			appendText(cardImageText(e))
		case "img_combination":
			for _, img := range e.list("img_list") {
				if ie, ok := parseCardElem(img); ok {
					appendText(cardImageText(ie))
				}
			}
		case "column_set":
			for _, col := range e.list("columns") {
				if ce, ok := parseCardElem(col); ok {
					extractCardElements(ce.list("elements"), parts)
				}
			}
		case "collapsible_panel":
			var header struct {
				Title json.RawMessage `json:"title"`
			}
			_ = json.Unmarshal(e.get("header"), &header)
			appendText(cardInlineText(header.Title))
			extractCardElements(e.list("elements"), parts)
		case "action":
			extractCardElements(e.list("actions"), parts)
		case "note":
			var inline []string
			for _, child := range e.list("elements") {
				inline = append(inline, cardInlineText(child))
			}
			appendText(strings.Join(inline, " "))
		case "div":
			appendText(cardInlineText(e.get("text")))
			for _, f := range e.list("fields") {
				var field struct {
					Text json.RawMessage `json:"text"`
				}
				if json.Unmarshal(f, &field) == nil {
					appendText(cardInlineText(field.Text))
				}
			}
			if extra := e.get("extra"); len(extra) > 0 {
				extractCardElements([]json.RawMessage{extra}, parts)
			}
		case "markdown", "lark_md", "plain_text", "text", "link", "a", "at", "code_span", "text_tag":
			appendText(cardInlineText(raw))
		case "standard_icon", "ud_icon", "br":
			// Decorative only.
		default:
			// Containers (form, interactive_container, ...) and unknown text-bearing elements.
			if c := e.get("content"); len(c) > 0 {
				appendText(applyCardTextStyle(cardInlineText(c), e.get("textStyle")))
			}
			if t := e.get("text"); len(t) > 0 {
				appendText(cardInlineText(t))
			}
			extractCardElements(e.list("elements"), parts)
			extractCardElements(e.list("columns"), parts)
		}
	}
}

// extractCardTable extracts text from a Feishu card table element.
// Table structure: property.columns defines column names/headers,
// property.rows is an array of row objects where each key is the column name
// and the value has a "data" field containing a markdown/plain_text element.
func extractCardTable(columnsRaw, rowsRaw json.RawMessage, parts *[]string) {
	var columns []struct {
		DisplayName string `json:"displayName"`
		Name        string `json:"name"`
	}
	if err := json.Unmarshal(columnsRaw, &columns); err != nil || len(columns) == 0 {
		return
	}
	var rows []map[string]struct {
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(rowsRaw, &rows); err != nil {
		return
	}

	// Build markdown table.
	header := make([]string, len(columns))
	for i, col := range columns {
		header[i] = col.DisplayName
	}
	*parts = append(*parts, "| "+strings.Join(header, " | ")+" |")
	sep := make([]string, len(columns))
	for i := range sep {
		sep[i] = "---"
	}
	*parts = append(*parts, "| "+strings.Join(sep, " | ")+" |")

	for _, row := range rows {
		cells := make([]string, len(columns))
		for i, col := range columns {
			cell := row[col.Name]
			var cellParts []string
			extractCardElements([]json.RawMessage{cell.Data}, &cellParts)
			cells[i] = strings.ReplaceAll(strings.Join(cellParts, " "), "\n", " ")
		}
		*parts = append(*parts, "| "+strings.Join(cells, " | ")+" |")
	}
}

// cardCodeBlock renders a code_block element (tokenized lines) as a fenced block.
func cardCodeBlock(e cardElem) string {
	var lines []struct {
		Contents []struct {
			Content string `json:"content"`
		} `json:"contents"`
	}
	if json.Unmarshal(e.get("contents"), &lines) != nil {
		return ""
	}
	var code strings.Builder
	for _, line := range lines {
		for _, tok := range line.Contents {
			code.WriteString(tok.Content)
		}
	}
	if strings.TrimSpace(code.String()) == "" {
		return ""
	}
	return fmt.Sprintf("```%s\n%s```", e.str("language"), code.String())
}

// cardListLines renders a Feishu card list element as markdown list lines.
// List structure: property.items is an array of items, each with an "elements"
// array of inline runs, plus type (ul/ol) and nesting level.
func cardListLines(itemsRaw json.RawMessage) []string {
	var items []struct {
		Elements []json.RawMessage `json:"elements"`
		Type     string            `json:"type"`
		Level    int               `json:"level"`
	}
	if err := json.Unmarshal(itemsRaw, &items); err != nil {
		return nil
	}
	var lines []string
	n := 0
	for _, item := range items {
		var inline strings.Builder
		for _, el := range item.Elements {
			inline.WriteString(cardInlineText(el))
		}
		s := strings.TrimSpace(inline.String())
		if s == "" {
			continue
		}
		marker := "- "
		if item.Type == "ol" {
			n++
			marker = fmt.Sprintf("%d. ", n)
		}
		// Bound untrusted nesting levels to prevent panics and excessive allocation.
		lines = append(lines, strings.Repeat("  ", min(max(item.Level, 0), 16))+marker+s)
	}
	return lines
}

// interactiveCardText falls back to the event payload when a fetched card is unavailable.
func interactiveCardText(fetched, eventContent string) string {
	if text := strings.TrimSpace(fetched); text != "" && text != "[interactive card]" {
		return fetched
	}
	return extractInteractiveCardText(eventContent)
}
