package deferred

import (
	"encoding/json"
	"fmt"
	"strings"
)

type callEntry struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

func normalizeCalls(input json.RawMessage) ([]callEntry, error) {
	if len(input) == 0 {
		return nil, fmt.Errorf("tool_call requires calls")
	}
	var payload struct {
		Calls json.RawMessage `json:"calls"`
	}
	if err := json.Unmarshal(input, &payload); err != nil {
		return nil, fmt.Errorf("tool_call: invalid JSON: %w", err)
	}
	var calls []callEntry
	if err := json.Unmarshal(payload.Calls, &calls); err != nil {
		// single object fallback
		var one callEntry
		if err2 := json.Unmarshal(payload.Calls, &one); err2 != nil {
			return nil, fmt.Errorf("tool_call calls must be a non-empty array of {name, arguments}")
		}
		calls = []callEntry{one}
	}
	if len(calls) == 0 {
		return nil, fmt.Errorf("tool_call calls must be a non-empty array")
	}
	for i := range calls {
		calls[i].Name = strings.TrimSpace(calls[i].Name)
		if calls[i].Name == "" {
			return nil, fmt.Errorf("tool_call calls[%d] requires name", i)
		}
		if IsBridge(calls[i].Name) {
			return nil, fmt.Errorf("tool_call cannot invoke bridge tool %q", calls[i].Name)
		}
		if len(calls[i].Arguments) == 0 {
			calls[i].Arguments = json.RawMessage(`{}`)
		}
	}
	if len(calls) > 1 {
		return nil, fmt.Errorf("tool_call supports one local tool per call in this version")
	}
	return calls, nil
}

func parseSearchInput(input json.RawMessage, defaultLimit, maxLimit int) ([]string, int, error) {
	var payload struct {
		Queries json.RawMessage `json:"queries"`
		Limit   *int            `json:"limit"`
	}
	if err := json.Unmarshal(input, &payload); err != nil {
		return nil, 0, err
	}
	var queries []string
	if err := json.Unmarshal(payload.Queries, &queries); err != nil {
		var one string
		if err2 := json.Unmarshal(payload.Queries, &one); err2 != nil {
			return nil, 0, fmt.Errorf("tool_search requires queries")
		}
		queries = []string{one}
	}
	if len(queries) == 0 {
		return nil, 0, fmt.Errorf("tool_search requires queries")
	}
	if len(queries) > 7 {
		return nil, 0, fmt.Errorf("tool_search supports at most 7 queries per call")
	}
	limit := defaultLimit
	if payload.Limit != nil {
		limit = *payload.Limit
	}
	if limit < 1 {
		limit = 1
	}
	if limit > maxLimit {
		limit = maxLimit
	}
	return queries, limit, nil
}

func parseDescribeNames(input json.RawMessage) ([]string, error) {
	var payload struct {
		Names json.RawMessage `json:"names"`
	}
	if err := json.Unmarshal(input, &payload); err != nil {
		return nil, err
	}
	var names []string
	if err := json.Unmarshal(payload.Names, &names); err != nil {
		var one string
		if err2 := json.Unmarshal(payload.Names, &one); err2 != nil {
			return nil, fmt.Errorf("tool_describe requires names")
		}
		names = []string{one}
	}
	if len(names) == 0 {
		return nil, fmt.Errorf("tool_describe requires names")
	}
	if len(names) > 10 {
		return nil, fmt.Errorf("tool_describe supports at most 10 names per call")
	}
	return names, nil
}
