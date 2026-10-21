package deferred

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/lengzhao/agentkit"
)

const defaultListMaxBytes = 8192

type listPage struct {
	Entries    []string                       `json:"entries"`
	Tools      map[string]map[string]string   `json:"tools"`
	Total      int                            `json:"total"`
	Offset     int                            `json:"offset"`
	Limit      int                            `json:"limit"`
	HasMore    bool                           `json:"has_more"`
	NextOffset int                            `json:"next_offset"`
	Truncated  bool                           `json:"truncated,omitempty"`
	Hint       string                         `json:"hint,omitempty"`
}

func parseListInput(input json.RawMessage, defaultLimit, maxLimit int) (offset int, limit int, source string, err error) {
	var payload struct {
		Offset *int   `json:"offset"`
		Limit  *int   `json:"limit"`
		Source string `json:"source"`
	}
	if len(input) == 0 {
		input = json.RawMessage(`{}`)
	}
	if err := json.Unmarshal(input, &payload); err != nil {
		return 0, 0, "", err
	}
	offset = 0
	if payload.Offset != nil {
		offset = *payload.Offset
	}
	if offset < 0 {
		return 0, 0, "", fmt.Errorf("tool_list offset must be >= 0")
	}
	limit = defaultLimit
	if payload.Limit != nil {
		limit = *payload.Limit
	}
	if limit < 1 {
		limit = 1
	}
	if limit > maxLimit {
		limit = maxLimit
	}
	return offset, limit, strings.TrimSpace(payload.Source), nil
}

func filterCatalogBySource(catalog []catalogEntry, source string) []catalogEntry {
	if source == "" {
		return catalog
	}
	srcLower := strings.ToLower(source)
	out := make([]catalogEntry, 0, len(catalog))
	for _, e := range catalog {
		if catalogEntryMatchesSource(e, srcLower) {
			out = append(out, e)
		}
	}
	return out
}

func catalogEntryMatchesSource(e catalogEntry, sourceLower string) bool {
	if sourceLower == "" {
		return true
	}
	prefix := strings.ToLower(toolNameSourcePrefix(e.Spec.Name))
	if prefix == sourceLower {
		return true
	}
	if strings.ToLower(e.SourceLabel) == sourceLower {
		return true
	}
	return strings.HasPrefix(strings.ToLower(e.Spec.Name), sourceLower+"__") ||
		strings.HasPrefix(strings.ToLower(e.Spec.Name), sourceLower+"_")
}

func toolNameSourcePrefix(name string) string {
	if i := strings.Index(name, "__"); i > 0 {
		return name[:i]
	}
	if i := strings.Index(name, "_"); i > 0 {
		return name[:i]
	}
	return name
}

func sortedCatalog(catalog []catalogEntry) []catalogEntry {
	out := append([]catalogEntry(nil), catalog...)
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].Spec.Name < out[j].Spec.Name
	})
	return out
}

// listCatalogPage returns a stable name-ordered page of deferrable tools.
// It does not reveal tools or include full input schemas.
func listCatalogPage(catalog []catalogEntry, offset, limit, maxBytes int, source string) listPage {
	filtered := sortedCatalog(filterCatalogBySource(catalog, source))
	total := len(filtered)
	page := listPage{
		Total:  total,
		Offset: offset,
		Limit:  limit,
		Tools:  make(map[string]map[string]string),
	}
	if offset > total {
		page.Hint = "offset beyond catalog; use offset 0 or a lower value"
		page.NextOffset = total
		return page
	}
	if maxBytes < 256 {
		maxBytes = 256
	}
	window := filtered[offset:]
	entries := make([]string, 0, min(limit, len(window)))
	used := estimateListPageOverhead(page)
	for _, e := range window {
		if len(entries) >= limit {
			break
		}
		summary := toolSummaryMap([]agentkit.ToolSpec{e.Spec})[e.Spec.Name]
		entryBytes := estimateListEntryBytes(e.Spec.Name, summary)
		if len(entries) > 0 && used+entryBytes > maxBytes {
			page.Truncated = true
			break
		}
		entries = append(entries, e.Spec.Name)
		page.Tools[e.Spec.Name] = summary
		used += entryBytes
		if len(entries) == 1 && used > maxBytes {
			page.Truncated = true
		}
	}
	page.Entries = entries
	next := offset + len(entries)
	page.NextOffset = next
	page.HasMore = next < total
	if page.HasMore {
		page.Hint = fmt.Sprintf("More tools available: call %s with offset=%d (same limit/source). Use %s for full schemas, %s to find tools by keyword.",
			ToolList, next, ToolDescribe, ToolSearch)
	} else if total == 0 && source != "" {
		page.Hint = "No tools match source filter; omit source or try tool_search."
	}
	return page
}

func estimateListPageOverhead(page listPage) int {
	// Fixed JSON envelope without entries/tools payload.
	stub, _ := json.Marshal(listPage{
		Total: page.Total, Offset: page.Offset, Limit: page.Limit,
		HasMore: false, NextOffset: page.Offset,
	})
	return len(stub) + 32
}

func estimateListEntryBytes(name string, summary map[string]string) int {
	body, err := json.Marshal(map[string]any{
		"name": name,
		"tool": summary,
	})
	if err != nil {
		return len(name) + 64
	}
	return len(body)
}
