package deferred

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/lengzhao/agentkit"
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
	for _, q := range queries {
		hits := searchCatalog(catalog, q, limit)
		matches := make([]string, 0, len(hits))
		for _, spec := range hits {
			matches = append(matches, spec.Name)
			if _, ok := shared[spec.Name]; !ok {
				shared[spec.Name] = toolSummaryMap([]agentkit.ToolSpec{spec})[spec.Name]
			}
		}
		groups = append(groups, queryGroup{Query: q, Matches: matches})
	}
	out := map[string]any{
		"queries": groups,
		"tools":   shared,
		"total_available": len(catalog),
	}
	body, err := json.Marshal(out)
	if err != nil {
		return bridgeError(call, err.Error()), nil
	}
	return agentkit.ResultFromCall(call, string(body)), nil
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
	for name, spec := range found {
		tools[name] = spec
	}
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
	entry := entries[0]
	if !allowed[entry.Name] {
		return bridgeError(call, fmt.Sprintf("'%s' is not in the deferred catalog for this session", entry.Name)), nil
	}
	innerCall := agentkit.ToolCall{
		ID:    call.ID,
		Name:  entry.Name,
		Input: entry.Arguments,
	}
	result, err := d.inner.Execute(ctx, innerCall)
	if err != nil {
		if agentkit.IsTurnAbort(err) {
			return result, err
		}
		return bridgeError(call, err.Error()), nil
	}
	// Session / UI show the real tool name after unwrap.
	result.Name = entry.Name
	return result, nil
}
