package deferred

import (
	"encoding/json"
	"testing"

	"github.com/lengzhao/agentkit"
)

func TestListCatalogPageStableOrder(t *testing.T) {
	t.Parallel()
	catalog := buildCatalog([]agentkit.ToolSpec{
		toolSpec("zeta__a", "z"),
		toolSpec("alpha__b", "a"),
		toolSpec("mcp__ping", "p"),
	})
	page := listCatalogPage(catalog, 0, 10, 8192, "")
	if len(page.Entries) != 3 {
		t.Fatalf("entries = %v", page.Entries)
	}
	if page.Entries[0] != "alpha__b" || page.Entries[1] != "mcp__ping" || page.Entries[2] != "zeta__a" {
		t.Fatalf("order = %v", page.Entries)
	}
	if page.Total != 3 || page.HasMore {
		t.Fatalf("total/has_more = %d %v", page.Total, page.HasMore)
	}
}

func TestListCatalogPageSourceFilter(t *testing.T) {
	t.Parallel()
	catalog := buildCatalog([]agentkit.ToolSpec{
		toolSpec("petstore__get", "g"),
		toolSpec("github__search", "s"),
	})
	page := listCatalogPage(catalog, 0, 50, 8192, "petstore")
	if len(page.Entries) != 1 || page.Entries[0] != "petstore__get" {
		t.Fatalf("page = %+v", page)
	}
}

func TestListCatalogPagePagination(t *testing.T) {
	t.Parallel()
	specs := make([]agentkit.ToolSpec, 0, 5)
	for i := 0; i < 5; i++ {
		specs = append(specs, toolSpec("api__tool_"+string(rune('a'+i)), "d"))
	}
	catalog := buildCatalog(specs)
	page := listCatalogPage(catalog, 2, 2, 8192, "")
	if len(page.Entries) != 2 {
		t.Fatalf("entries = %v", page.Entries)
	}
	if !page.HasMore || page.NextOffset != 4 {
		t.Fatalf("has_more/next = %v %d", page.HasMore, page.NextOffset)
	}
}

func TestParseListInput(t *testing.T) {
	t.Parallel()
	raw := json.RawMessage(`{"offset":3,"limit":10,"source":"mcp"}`)
	off, lim, src, err := parseListInput(raw, 50, 200)
	if err != nil || off != 3 || lim != 10 || src != "mcp" {
		t.Fatalf("got %d %d %q err=%v", off, lim, src, err)
	}
}
