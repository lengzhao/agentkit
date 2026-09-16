package deferred

import (
	"testing"

	"github.com/lengzhao/agentkit"
)

func toolSpec(name, desc string) agentkit.ToolSpec {
	return agentkit.ToolSpec{
		Name:        name,
		Description: desc,
		InputSchema: agentkit.JSONSchema{Type: "object", Properties: map[string]agentkit.JSONSchema{
			"repo": {Type: "string"},
		}},
	}
}

func TestSearchCatalogTokenOverlap(t *testing.T) {
	t.Parallel()
	catalog := buildCatalog([]agentkit.ToolSpec{
		toolSpec("github__search_repositories", "Search GitHub repositories by keyword"),
		toolSpec("slack__post_message", "Post a chat message to Slack"),
	})
	hits := searchCatalog(catalog, "github repositories", 5)
	if len(hits) == 0 || hits[0].Name != "github__search_repositories" {
		t.Fatalf("hits=%v", hits)
	}
}

func TestSearchCatalogBM25Stemming(t *testing.T) {
	t.Parallel()
	catalog := buildCatalog([]agentkit.ToolSpec{
		toolSpec("petstore__list_pets", "Returns all pets from the store inventory"),
		toolSpec("other__noop", "Unrelated utility"),
	})
	hits := searchCatalog(catalog, "listing pets inventory", 5)
	found := false
	for _, h := range hits {
		if h.Name == "petstore__list_pets" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("bm25/stem path missed pet tool: %v", hits)
	}
}

func TestSearchCatalogSubstringFallback(t *testing.T) {
	t.Parallel()
	catalog := buildCatalog([]agentkit.ToolSpec{
		toolSpec("mcp__my-special_tool", "Does something"),
		toolSpec("other", "Else"),
	})
	hits := searchCatalog(catalog, "my-special", 5)
	if len(hits) != 1 || hits[0].Name != "mcp__my-special_tool" {
		t.Fatalf("hits=%v", hits)
	}
}

func TestSearchCatalogUnionWhenTokenMisses(t *testing.T) {
	t.Parallel()
	catalog := buildCatalog([]agentkit.ToolSpec{
		toolSpec("openapi__getUserProfile", "Retrieve user profile details from API"),
		toolSpec("noise", "nothing here"),
	})
	hits := searchCatalog(catalog, "userprofile", 5)
	if len(hits) == 0 || hits[0].Name != "openapi__getUserProfile" {
		t.Fatalf("hits=%v", hits)
	}
}
