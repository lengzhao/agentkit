package deferred

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/lengzhao/agentkit"
	"golang.org/x/sync/errgroup"
)

func (d *Runtime) executeSearch(ctx context.Context, call agentkit.ToolCall) (agentkit.ToolResult, error) {
	queries, limit, err := parseSearchInput(call.Input, d.cfg.SearchDefaultLimit, d.cfg.MaxSearchLimit)
	if err != nil {
		return bridgeError(call, err.Error()), nil
	}
	catalog, err := d.deferrableCatalog(ctx)
	if err != nil {
		return bridgeError(call, err.Error()), nil
	}
	type queryGroup struct {
		Query   string   `json:"query"`
		Matches []string `json:"matches"`
	}
	groups := make([]queryGroup, 0, len(queries))
	shared := make(map[string]map[string]string)
	var hits []string
	var hitSpecs []agentkit.ToolSpec
	seen := make(map[string]bool)
	for _, q := range queries {
		found := searchCatalog(catalog, q, limit)
		matches := make([]string, 0, len(found))
		for _, spec := range found {
			matches = append(matches, spec.Name)
			if _, ok := shared[spec.Name]; !ok {
				shared[spec.Name] = toolSummaryMap([]agentkit.ToolSpec{spec})[spec.Name]
			}
			if !seen[spec.Name] {
				seen[spec.Name] = true
				hitSpecs = append(hitSpecs, spec)
			}
		}
		groups = append(groups, queryGroup{Query: q, Matches: matches})
		hits = append(hits, matches...)
	}
	reveal(ctx, hits...)
	out := map[string]any{
		"queries":         groups,
		"tools":           shared,
		"total_available": len(catalog),
	}
	// Inline full schemas for the top hits so the model can invoke immediately,
	// skipping a tool_describe round-trip (hits are revealed either way).
	if schemas := inlineSearchSchemas(hitSpecs, d.cfg.SearchInlineSchemaMax); len(schemas) > 0 {
		out["schemas"] = schemas
		out["hint"] = "Tools in schemas carry full parameter schemas: call them directly by name (or via " + ToolCall + ") — " + ToolDescribe + " is only needed for matches without an entry here."
	}
	body, err := json.Marshal(out)
	if err != nil {
		return bridgeError(call, err.Error()), nil
	}
	return agentkit.ResultFromCall(call, string(body)), nil
}

// inlineSearchSchemas returns full specs for the top-ranked hits, bounded by
// count (max) and a total JSON byte budget. The first hit is always inlined
// when max > 0, even if it alone exceeds the budget.
func inlineSearchSchemas(specs []agentkit.ToolSpec, max int) map[string]agentkit.ToolSpec {
	if max <= 0 {
		return nil
	}
	out := make(map[string]agentkit.ToolSpec, min(max, len(specs)))
	used := 0
	for _, spec := range specs {
		if len(out) >= max {
			break
		}
		body, err := json.Marshal(spec)
		if err != nil {
			continue
		}
		if len(out) > 0 && used+len(body) > searchInlineSchemaBudget {
			break
		}
		out[spec.Name] = spec
		used += len(body)
	}
	return out
}

func (d *Runtime) executeDescribe(ctx context.Context, call agentkit.ToolCall) (agentkit.ToolResult, error) {
	names, err := parseDescribeNames(call.Input)
	if err != nil {
		return bridgeError(call, err.Error()), nil
	}
	catalog, err := d.deferrableCatalog(ctx)
	if err != nil {
		return bridgeError(call, err.Error()), nil
	}
	found, notFound := describeCatalog(catalog, names)
	tools := make(map[string]agentkit.ToolSpec, len(found))
	loaded := make([]string, 0, len(found))
	for name, spec := range found {
		warnHollowDescribeSchema(spec)
		tools[name] = spec
		loaded = append(loaded, name)
	}
	reveal(ctx, loaded...)
	out := map[string]any{"tools": tools}
	if len(notFound) > 0 {
		out["not_found"] = notFound
		out["hint"] = "Names in not_found are not in the deferred catalog. Run tool_search to refresh."
	}
	body, err := json.Marshal(out)
	if err != nil {
		return bridgeError(call, err.Error()), nil
	}
	return agentkit.ResultFromCall(call, string(body)), nil
}

func (d *Runtime) executeBridgeCall(ctx context.Context, call agentkit.ToolCall) (agentkit.ToolResult, error) {
	entries, err := normalizeCalls(call.Input)
	if err != nil {
		return bridgeError(call, err.Error()), nil
	}
	allowed, err := d.catalogNameSet(ctx)
	if err != nil {
		return bridgeError(call, err.Error()), nil
	}
	for _, entry := range entries {
		if !allowed[entry.Name] {
			return bridgeError(call, fmt.Sprintf("'%s' is not in the deferred catalog for this session", entry.Name)), nil
		}
	}
	if len(entries) == 1 {
		return d.executeDeferredEntry(ctx, call, entries[0])
	}
	return d.executeDeferredEntriesParallel(ctx, call, entries)
}

func (d *Runtime) executeDeferredEntry(ctx context.Context, bridgeCall agentkit.ToolCall, entry callEntry) (agentkit.ToolResult, error) {
	innerCall := agentkit.ToolCall{
		ID:    bridgeCall.ID,
		Name:  entry.Name,
		Input: entry.Arguments,
	}
	result, err := d.inner.Execute(ctx, innerCall)
	if err != nil {
		if agentkit.IsTurnAbort(err) {
			return result, err
		}
		return bridgeError(bridgeCall, err.Error()), nil
	}
	result.Name = entry.Name
	return result, nil
}

type deferredCallResult struct {
	Name    string `json:"name"`
	Content string `json:"content"`
	Error   string `json:"error,omitempty"`
}

func (d *Runtime) executeDeferredEntriesParallel(ctx context.Context, bridgeCall agentkit.ToolCall, entries []callEntry) (agentkit.ToolResult, error) {
	results := make([]deferredCallResult, len(entries))
	var abortErr error
	var abortMu sync.Mutex
	g, gctx := errgroup.WithContext(ctx)

	for i, entry := range entries {
		idx := i
		g.Go(func() error {
			if gctx.Err() != nil {
				return nil
			}
			innerCall := agentkit.ToolCall{
				ID:    agentkit.ToolCallID(fmt.Sprintf("%s~%d", bridgeCall.ID, idx)),
				Name:  entry.Name,
				Input: entry.Arguments,
			}
			result, err := d.inner.Execute(gctx, innerCall)
			if err != nil {
				if agentkit.IsTurnAbort(err) {
					abortMu.Lock()
					if abortErr == nil {
						abortErr = err
					}
					abortMu.Unlock()
					return err
				}
				results[idx] = deferredCallResult{Name: entry.Name, Error: err.Error()}
				return nil
			}
			results[idx] = deferredCallResult{Name: entry.Name, Content: result.Content}
			return nil
		})
	}

	if err := g.Wait(); err != nil {
		if agentkit.IsTurnAbort(err) || agentkit.IsTurnAbort(abortErr) {
			if abortErr != nil {
				err = abortErr
			}
			return agentkit.ToolResult{}, err
		}
	}
	body, err := json.Marshal(map[string]any{"calls": results})
	if err != nil {
		return bridgeError(bridgeCall, err.Error()), nil
	}
	return agentkit.ResultFromCall(bridgeCall, string(body)), nil
}
