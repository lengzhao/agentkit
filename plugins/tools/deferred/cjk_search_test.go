package deferred

import (
	"testing"

	"github.com/lengzhao/agentkit"
)

func TestSearchCatalogChineseDescription(t *testing.T) {
	t.Parallel()
	catalog := buildCatalog([]agentkit.ToolSpec{
		{
			Name:        "petstore__list_pets",
			Description: "查询宠物商店中的全部宠物列表",
			InputSchema: agentkit.JSONSchema{Type: "object"},
		},
		toolSpec("other__noop", "Unrelated"),
	})
	hits := searchCatalog(catalog, "宠物", 5)
	if len(hits) == 0 || hits[0].Name != "petstore__list_pets" {
		t.Fatalf("hits=%v", hits)
	}
}

func TestSearchCatalogChineseBigram(t *testing.T) {
	t.Parallel()
	catalog := buildCatalog([]agentkit.ToolSpec{
		{
			Name:        "lark__send_message",
			Description: "向飞书会话发送文本消息",
			InputSchema: agentkit.JSONSchema{Type: "object"},
		},
		toolSpec("other", "nothing"),
	})
	hits := searchCatalog(catalog, "飞书消息", 5)
	if len(hits) == 0 || hits[0].Name != "lark__send_message" {
		t.Fatalf("hits=%v", hits)
	}
}

func TestIsPureCJKQuery(t *testing.T) {
	t.Parallel()
	if !isPureCJKQuery("查询宠物") {
		t.Fatal("expected pure CJK")
	}
	if isPureCJKQuery("pet 宠物") {
		t.Fatal("ascii letters should not be pure CJK")
	}
	if isPureCJKQuery("github") {
		t.Fatal("english only")
	}
}
