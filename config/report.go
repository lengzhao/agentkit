package config

import (
	"fmt"
	"sort"
	"strings"
)

// ResolveReport contains metadata only, never resolved configuration values.
// Causes refer to instance IDs; following them through Instances yields root causes.
type ResolveReport struct {
	MergedIDs    []string           `json:"merged_ids"`
	ResolvedIDs  []string           `json:"resolved_ids"`
	RootID       string             `json:"root_id,omitempty"`
	Instances    []InstanceReport   `json:"instances"`
	Dependencies []DependencyReport `json:"dependencies"`
}

type InstanceReport struct {
	ID        string   `json:"id"`
	State     string   `json:"state"` // active, pending, explicit_null, missing_value, dependency_cascade, excluded, unreachable
	Field     string   `json:"field,omitempty"`
	Reference string   `json:"reference,omitempty"`
	Causes    []string `json:"causes,omitempty"`
}

// DependencyReport preserves the original dependency edge even after pruning.
type DependencyReport struct {
	InstanceID string `json:"instance_id"`
	Field      string `json:"field"`
	Target     string `json:"target"`
	State      string `json:"state"` // present, pruned, dangling
}

// DeploymentIntent lists mutually exclusive deployment expectations. Unlisted
// instances retain normal resolution behavior. Required does not force reachability.
type DeploymentIntent struct {
	Required []string
	Optional []string
	Excluded []string
}

// WithDeploymentIntent copies intent so subsequent caller mutation has no effect.
func WithDeploymentIntent(intent DeploymentIntent) ResolveOption {
	intent.Required = append([]string(nil), intent.Required...)
	intent.Optional = append([]string(nil), intent.Optional...)
	intent.Excluded = append([]string(nil), intent.Excluded...)
	return func(o *resolveOptions) { o.intents = append(o.intents, intent) }
}

// ResolveYAMLWithReport resolves using the same pipeline as ResolveYAML. A partial
// report is returned on error; pending means resolution did not reach a final state.
func ResolveYAMLWithReport(dir string, base []byte, overlays [][]byte, opts ...ResolveOption) ([]byte, ResolveReport, error) {
	r := ResolveReport{}
	out, err := resolveYAMLReported(dir, base, overlays, opts, &r)
	return out, r, err
}

func sortedInstanceIDs(raw map[string]any) []string {
	ids := make([]string, 0, len(raw))
	for id := range raw {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

func (r *ResolveReport) prepare(raw map[string]any, disabled map[string]disableReason) {
	if r == nil {
		return
	}
	ids := map[string]any{}
	for id := range raw {
		ids[id] = nil
	}
	for id := range disabled {
		ids[id] = nil
	}
	r.MergedIDs = sortedInstanceIDs(ids)
	r.ResolvedIDs = []string{}
	r.Instances = []InstanceReport{}
	r.Dependencies = []DependencyReport{}
	for _, id := range r.MergedIDs {
		r.Instances = append(r.Instances, InstanceReport{ID: id, State: "pending"})
		node, _ := asStringMap(raw[id])
		deps, _ := asStringMap(node["deps"])
		for _, key := range sortedInstanceIDs(deps) {
			walkReportRefs(deps[key], id+".deps."+key, func(field, target string) {
				state := "present"
				if _, ok := ids[target]; !ok {
					state = "dangling"
				}
				if strings.Contains(target, "${") {
					target = "<interpolated reference>"
					state = "pending"
				}
				r.Dependencies = append(r.Dependencies, DependencyReport{InstanceID: id, Field: field, Target: target, State: state})
			})
		}
	}
}

func walkReportRefs(v any, path string, visit func(string, string)) {
	switch x := v.(type) {
	case string:
		if x != "" {
			visit(path, x)
		}
	case []any:
		for i, item := range x {
			walkReportRefs(item, fmt.Sprintf("%s[%d]", path, i), visit)
		}
	case []string:
		for i, item := range x {
			walkReportRefs(item, fmt.Sprintf("%s[%d]", path, i), visit)
		}
	case map[string]any:
		if _, ok := x["use"]; ok {
			deps, _ := asStringMap(x["deps"])
			for _, key := range sortedInstanceIDs(deps) {
				walkReportRefs(deps[key], path+".deps."+key, visit)
			}
		}
	}
}

func (r *ResolveReport) recordDisabled(disabled map[string]disableReason) {
	if r == nil {
		return
	}
	for i := range r.Dependencies {
		d := &r.Dependencies[i]
		if _, ok := disabled[d.Target]; ok {
			d.State = "pruned"
		}
	}
	for i := range r.Instances {
		s := &r.Instances[i]
		reason, ok := disabled[s.ID]
		if !ok {
			continue
		}
		s.State = reason.reason
		s.Field = reason.field
		s.Reference = reason.missingRef
		if s.State == "empty_deps" {
			s.State = "dependency_cascade"
			causes := map[string]any{}
			for _, d := range r.Dependencies {
				if d.InstanceID == s.ID && d.State == "pruned" {
					causes[d.Target] = nil
				}
			}
			s.Causes = sortedInstanceIDs(causes)
		}
	}
}

func (r *ResolveReport) finish(raw map[string]any, root string) {
	if r == nil {
		return
	}
	r.RootID = root
	r.ResolvedIDs = sortedInstanceIDs(raw)
	for i := range r.Instances {
		s := &r.Instances[i]
		if s.State != "pending" {
			continue
		}
		if _, ok := raw[s.ID]; ok {
			s.State = "active"
		} else {
			s.State = "unreachable"
		}
	}
}

func applyDeploymentIntent(raw map[string]any, disabled map[string]disableReason, opts []ResolveOption) ([]string, error) {
	seen := map[string]string{}
	var required []string
	for _, intent := range applyResolveOptions(opts).intents {
		for _, group := range []struct {
			kind string
			ids  []string
		}{{"required", intent.Required}, {"optional", intent.Optional}, {"excluded", intent.Excluded}} {
			for _, id := range group.ids {
				if prior, ok := seen[id]; ok {
					return nil, fmt.Errorf("deployment intent %q: duplicate or conflicting %s/%s", id, prior, group.kind)
				}
				if _, ok := raw[id]; !ok {
					if _, ok := disabled[id]; !ok {
						return nil, fmt.Errorf("deployment intent references unknown instance %q", id)
					}
				}
				seen[id] = group.kind
				if group.kind == "required" {
					required = append(required, id)
				}
			}
		}
	}
	// Validate the complete intent before applying any exclusions.
	for id, kind := range seen {
		if kind == "excluded" {
			disabled[id] = disableReason{reason: "excluded", use: instanceUseFromAny(raw[id])}
			delete(raw, id)
		}
	}
	sort.Strings(required)
	return required, nil
}

// refreshDependencies uses resolved references only for surviving instances.
// Pruned edges remain available for causal inspection, including shifted lists.
func (r *ResolveReport) refreshDependencies(raw map[string]any, interpolated map[string]bool) {
	if r == nil {
		return
	}
	old := r.Dependencies
	known := make(map[string]bool, len(r.MergedIDs))
	for _, id := range r.MergedIDs {
		known[id] = true
	}
	r.Dependencies = nil
	for _, d := range old {
		if _, ok := raw[d.InstanceID]; !ok || d.State == "pruned" {
			r.Dependencies = append(r.Dependencies, d)
		}
	}
	for _, id := range sortedInstanceIDs(raw) {
		node, _ := asStringMap(raw[id])
		deps, _ := asStringMap(node["deps"])
		for _, key := range sortedInstanceIDs(deps) {
			walkReportRefs(deps[key], id+".deps."+key, func(field, target string) {
				state := "dangling"
				if _, ok := raw[target]; ok {
					state = "present"
				}
				// An interpolated non-ID may be a secret value rather than an instance ID.
				if interpolated[field] && state == "dangling" && !known[target] {
					target = "<unresolved reference>"
				}

				r.Dependencies = append(r.Dependencies, DependencyReport{InstanceID: id, Field: field, Target: target, State: state})
			})
		}
	}
	sort.Slice(r.Dependencies, func(i, j int) bool {
		a, b := r.Dependencies[i], r.Dependencies[j]
		if a.InstanceID != b.InstanceID {
			return a.InstanceID < b.InstanceID
		}
		if a.Field != b.Field {
			return a.Field < b.Field
		}
		if a.Target != b.Target {
			return a.Target < b.Target
		}
		return a.State < b.State
	})
}

// Capture interpolation provenance after pruning, when list positions are final.
func (r *ResolveReport) interpolatedDependencyFields(raw map[string]any) map[string]bool {
	if r == nil {
		return nil
	}
	fields := map[string]bool{}
	for id, value := range raw {
		node, _ := asStringMap(value)
		deps, _ := asStringMap(node["deps"])
		for key, ref := range deps {
			walkReportRefs(ref, id+".deps."+key, func(field, target string) {
				if strings.Contains(target, "${") {
					fields[field] = true
				}
			})
		}
	}
	return fields
}
