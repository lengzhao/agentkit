package openapi

import (
	"context"
	"encoding/json"
	"testing"
)

func TestUpsertAPIJSONStoresAbsoluteDocumentPath(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	ws := &testWorkspace{root: dir}
	ctx := context.Background()
	raw := []byte(`{"path":"local:api/pet.json","baseUrl":"https://example.com"}`)
	out, err := upsertAPIJSON(ctx, ws, nil, "pet", raw)
	if err != nil {
		t.Fatal(err)
	}
	var doc rawIndexDocument
	if err := json.Unmarshal(out, &doc); err != nil {
		t.Fatal(err)
	}
	entry := doc.Apis["pet"]
	want, err := ws.Resolve(ctx, "local:api/pet.json")
	if err != nil {
		t.Fatal(err)
	}
	if entry.Path != want {
		t.Fatalf("path = %q, want %q", entry.Path, want)
	}
	if entry.SpecFile != "" {
		t.Fatalf("specFile = %q", entry.SpecFile)
	}
}
