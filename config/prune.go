package config

import (
	"fmt"
	"log/slog"
	"sort"
	"strings"
)

type disableReason struct {
	instanceID string
	use        string
	reason     string // "missing_value" | "empty_deps"
	field      string
	missingRef string
	depKeys    []string
}

// pruneUnavailableInstances removes instances that cannot run because required
// interpolation values are missing or because all deps were pruned. The process
// repeats until the graph stabilizes.
func pruneUnavailableInstances(raw map[string]any, interpDir string, explicitDisabled map[string]disableReason) (map[string]any, error) {
	disabled := cloneDisableReasons(explicitDisabled)
	prunedDepCount := 0

	for id, reason := range explicitDisabled {
		slog.Warn("config plugin disabled",
			"instance_id", id,
			"use", reason.use,
			"reason", reason.reason,
		)
	}

	for id, node := range raw {
		nodeMap, ok := asStringMap(node)
		if !ok {
			continue
		}
		if missing, ok := probeInterpolationMissing(id, nodeMap, interpDir); ok {
			disabled[id] = disableReason{
				instanceID: id,
				use:        instanceUse(nodeMap),
				reason:     "missing_value",
				field:      missing.field,
				missingRef: missing.ref,
			}
			slog.Warn("config plugin disabled",
				"instance_id", id,
				"use", instanceUse(nodeMap),
				"reason", "missing_value",
				"field", missing.field,
				"missing_ref", missing.ref,
			)
		}
	}

	for {
		changed := false
		for id, node := range raw {
			if _, isDisabled := disabled[id]; isDisabled {
				continue
			}
			nodeMap, ok := asStringMap(node)
			if !ok {
				continue
			}
			deps, ok := asStringMap(nodeMap["deps"])
			if !ok || len(deps) == 0 {
				continue
			}
			originalKeys := depKeys(deps)
			newDeps, removed := pruneDepsMap(id, deps, disabled)
			prunedDepCount += removed
			if removed == 0 {
				continue
			}
			if len(newDeps) == 0 {
				nodeMap["deps"] = map[string]any{}
				if _, already := disabled[id]; !already {
					disabled[id] = disableReason{
						instanceID: id,
						use:        instanceUse(nodeMap),
						reason:     "empty_deps",
						depKeys:    originalKeys,
					}
					slog.Warn("config plugin disabled by empty deps",
						"instance_id", id,
						"use", instanceUse(nodeMap),
						"dep_keys", originalKeys,
					)
					changed = true
				}
				continue
			}
			nodeMap["deps"] = newDeps
		}
		if !changed {
			break
		}
	}

	out := make(map[string]any, len(raw)-len(disabled))
	for id, node := range raw {
		if _, isDisabled := disabled[id]; isDisabled {
			continue
		}
		out[id] = node
	}

	rootID := DefaultRootID
	if _, ok := out[rootID]; !ok {
		rootID, _ = resolveRootID(out)
	}
	if rootID == "" || out[rootID] == nil {
		return nil, formatPruneFailure(disabled)
	}

	if len(disabled) > 0 || prunedDepCount > 0 {
		slog.Warn("config prune summary",
			"disabled_count", len(disabled),
			"pruned_dep_count", prunedDepCount,
			"remaining_count", len(out),
			"root_id", rootID,
		)
	}
	return out, nil
}

func instanceUse(node map[string]any) string {
	if use, ok := node["use"].(string); ok {
		return use
	}
	return ""
}

func instanceUseFromAny(node any) string {
	nodeMap, ok := asStringMap(node)
	if !ok {
		return ""
	}
	return instanceUse(nodeMap)
}

func cloneDisableReasons(in map[string]disableReason) map[string]disableReason {
	if len(in) == 0 {
		return map[string]disableReason{}
	}
	out := make(map[string]disableReason, len(in))
	for id, reason := range in {
		out[id] = reason
	}
	return out
}

func depKeys(deps map[string]any) []string {
	keys := make([]string, 0, len(deps))
	for key := range deps {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func pruneDepsMap(instanceID string, deps map[string]any, disabled map[string]disableReason) (map[string]any, int) {
	out := make(map[string]any, len(deps))
	pruned := 0
	for key, ref := range deps {
		newRef, removed := pruneDepRef(instanceID, key, ref, disabled)
		pruned += removed
		if newRef == nil {
			continue
		}
		out[key] = newRef
	}
	return out, pruned
}

func pruneDepRef(instanceID, depKey string, ref any, disabled map[string]disableReason) (any, int) {
	switch x := ref.(type) {
	case string:
		if x == "" {
			return ref, 0
		}
		if _, ok := disabled[x]; ok {
			slog.Warn("config dep pruned",
				"instance_id", instanceID,
				"dep_key", depKey,
				"dep_value", x,
				"disabled_instance_id", x,
			)
			return nil, 1
		}
		return ref, 0
	case []any:
		filtered := make([]any, 0, len(x))
		removed := 0
		for _, item := range x {
			id, ok := item.(string)
			if ok && id != "" {
				if _, dis := disabled[id]; dis {
					slog.Warn("config dep pruned",
						"instance_id", instanceID,
						"dep_key", depKey,
						"dep_value", id,
						"disabled_instance_id", id,
					)
					removed++
					continue
				}
			}
			filtered = append(filtered, item)
		}
		if len(filtered) == 0 {
			return nil, removed
		}
		return filtered, removed
	case []string:
		filtered := make([]any, 0, len(x))
		removed := 0
		for _, id := range x {
			if id == "" {
				continue
			}
			if _, dis := disabled[id]; dis {
				slog.Warn("config dep pruned",
					"instance_id", instanceID,
					"dep_key", depKey,
					"dep_value", id,
					"disabled_instance_id", id,
				)
				removed++
				continue
			}
			filtered = append(filtered, id)
		}
		if len(filtered) == 0 {
			return nil, removed
		}
		return filtered, removed
	case map[string]any:
		if _, hasUse := x["use"]; hasUse {
			if nestedDeps, ok := asStringMap(x["deps"]); ok && len(nestedDeps) > 0 {
				newNested, removed := pruneDepsMap(instanceID, nestedDeps, disabled)
				pruned := removed
				if len(newNested) == 0 {
					return nil, pruned
				}
				clone := cloneMap(x)
				clone["deps"] = newNested
				return clone, pruned
			}
		}
	}
	return ref, 0
}

func formatPruneFailure(disabled map[string]disableReason) error {
	ids := make([]string, 0, len(disabled))
	for id := range disabled {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	var b strings.Builder
	b.WriteString("config root unavailable after pruning disabled plugins")
	for _, id := range ids {
		reason := disabled[id]
		switch reason.reason {
		case "missing_value":
			b.WriteString(fmt.Sprintf("; %s (%s): missing %s at %s", id, reason.use, reason.missingRef, reason.field))
		case "empty_deps":
			b.WriteString(fmt.Sprintf("; %s (%s): all deps pruned (%s)", id, reason.use, strings.Join(reason.depKeys, ", ")))
		default:
			b.WriteString(fmt.Sprintf("; %s (%s): %s", id, reason.use, reason.reason))
		}
	}
	return fmt.Errorf("%s", b.String())
}
