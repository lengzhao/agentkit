package openapi

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/lengzhao/agentkit/cap/workspace"
	rtws "github.com/lengzhao/agentkit/runtime/workspace"
)

func storeAbsPath(ctx context.Context, ws workspace.Service, path string) (string, error) {
	if ws == nil {
		return path, nil
	}
	return rtws.ResolveFile(ctx, ws, path)
}

func normalizeAPIEntryPaths(ctx context.Context, ws workspace.Service, entry *rawAPIEntry) error {
	doc, err := entryDocumentPath(*entry)
	if err != nil {
		return err
	}
	if doc == "" {
		return nil
	}
	abs, err := storeAbsPath(ctx, ws, doc)
	if err != nil {
		return fmt.Errorf("resolve api document path: %w", err)
	}
	entry.Path = abs
	entry.SpecFile = ""
	return nil
}

func normalizeAPIEntryRaw(ctx context.Context, ws workspace.Service, raw []byte) ([]byte, error) {
	var entry rawAPIEntry
	if err := json.Unmarshal(raw, &entry); err != nil {
		return nil, fmt.Errorf("parse api json: %w", err)
	}
	if err := normalizeAPIEntryPaths(ctx, ws, &entry); err != nil {
		return nil, err
	}
	return json.Marshal(entry)
}
