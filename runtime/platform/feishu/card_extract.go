package feishu

import (
	"encoding/json"
	"fmt"
	"strings"
)

// extractInteractiveCardText extracts readable text from a Feishu interactive card JSON.
// With raw_card_content, the response wraps the card in {"json_card": "...", ...}.
// Supports schema 2.0 (body.property.elements with recursive nesting) and
// legacy format (top-level title + elements).
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

	// Schema 2.0: body may use property.elements (standard) or direct elements (simplified).
	if raw, ok := card["body"]; ok {
		var body struct {
			Tag      string            `json:"tag"`
			Elements []json.RawMessage `json:"elements"`
			Property struct {
				Elements []json.RawMessage `json:"elements"`
			} `json:"property"`
		}
		if json.Unmarshal(raw, &body) == nil {
			if body.Tag == "body" && len(body.Property.Elements) > 0 {
				extractCardElements(body.Property.Elements, &parts)
			} else if len(body.Elements) > 0 {
				extractCardElements(body.Elements, &parts)
			}
		}
	}

	// Legacy: direct title string + flat/nested elements.
	if len(parts) == 0 {
		if raw, ok := card["header"]; ok {
			var header struct {
				Title struct {
					Content string `json:"content"`
				} `json:"title"`
			}
			if json.Unmarshal(raw, &header) == nil && header.Title.Content != "" {
				parts = append(parts, header.Title.Content)
			}
		}
		if len(parts) == 0 {
			if raw, ok := card["title"]; ok {
				var title string
				if json.Unmarshal(raw, &title) == nil && title != "" {
					parts = append(parts, title)
				}
			}
		}
		var elements []json.RawMessage
		if raw, ok := card["elements"]; ok {
			var nested [][]json.RawMessage
			if json.Unmarshal(raw, &nested) == nil && len(nested) > 0 {
				for _, row := range nested {
					elements = append(elements, row...)
				}
			} else {
				_ = json.Unmarshal(raw, &elements)
			}
		}
		for _, raw := range elements {
			var elem struct {
				Tag  string `json:"tag"`
				Text string `json:"text"`
			}
			if json.Unmarshal(raw, &elem) == nil && elem.Tag == "text" && strings.TrimSpace(elem.Text) != "" {
				parts = append(parts, elem.Text)
			}
		}
	}

	if len(parts) == 0 {
		return "[interactive card]"
	}
	return strings.Join(parts, "\n")
}

// extractCardElements recursively extracts text from schema 2.0 card elements.
// Handles: property.content, property.text (nested element), property.elements (recursive),
// code_span, code_block (with tokenized contents), text_tag, hr, etc.
func extractCardElements(elements []json.RawMessage, parts *[]string) {
	for _, raw := range elements {
		var elem struct {
			Tag      string `json:"tag"`
			Content  string `json:"content"`
			Property struct {
				Content  string            `json:"content"`
				Contents json.RawMessage   `json:"contents"`
				Language string            `json:"language"`
				Elements []json.RawMessage `json:"elements"`
				Text     json.RawMessage   `json:"text"`
				Items    json.RawMessage   `json:"items"`
				Columns  json.RawMessage   `json:"columns"`
				Rows     json.RawMessage   `json:"rows"`
			} `json:"property"`
		}
		if json.Unmarshal(raw, &elem) != nil {
			continue
		}
		switch elem.Tag {
		case "code_block":
			var lines []struct {
				Contents []struct {
					Content string `json:"content"`
				} `json:"contents"`
			}
			if json.Unmarshal(elem.Property.Contents, &lines) == nil {
				var codeLines []string
				for _, line := range lines {
					var lineText string
					for _, tok := range line.Contents {
						lineText += tok.Content
					}
					codeLines = append(codeLines, lineText)
				}
				code := strings.Join(codeLines, "")
				if strings.TrimSpace(code) != "" {
					lang := elem.Property.Language
					if lang != "" {
						*parts = append(*parts, fmt.Sprintf("```%s\n%s```", lang, code))
					} else {
						*parts = append(*parts, fmt.Sprintf("```\n%s```", code))
					}
				}
			}
		case "code_span":
			if elem.Property.Content != "" {
				*parts = append(*parts, "`"+elem.Property.Content+"`")
			}
		case "hr":
			*parts = append(*parts, "---")
		case "table":
			extractCardTable(elem.Property.Columns, elem.Property.Rows, parts)
		case "list":
			extractCardListItems(elem.Property.Items, parts)
		default:
			content := elem.Property.Content
			if content == "" {
				content = elem.Content
			}
			if content != "" {
				*parts = append(*parts, content)
			}
			if len(elem.Property.Text) > 0 {
				var textElem struct {
					Property struct {
						Content string `json:"content"`
					} `json:"property"`
				}
				if json.Unmarshal(elem.Property.Text, &textElem) == nil && textElem.Property.Content != "" {
					*parts = append(*parts, textElem.Property.Content)
				}
			}
		}
		if len(elem.Property.Elements) > 0 {
			extractCardElements(elem.Property.Elements, parts)
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
			cells[i] = strings.Join(cellParts, " ")
		}
		*parts = append(*parts, "| "+strings.Join(cells, " | ")+" |")
	}
}

// extractCardListItems extracts text from a Feishu card list element.
// List structure: property.items is an array of items, each with an "elements" array.
func extractCardListItems(itemsRaw json.RawMessage, parts *[]string) {
	var items []struct {
		Elements []json.RawMessage `json:"elements"`
	}
	if err := json.Unmarshal(itemsRaw, &items); err != nil {
		return
	}
	for _, item := range items {
		var itemParts []string
		extractCardElements(item.Elements, &itemParts)
		if len(itemParts) > 0 {
			*parts = append(*parts, "- "+strings.Join(itemParts, " "))
		}
	}
}
